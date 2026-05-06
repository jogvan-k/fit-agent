// Package icu is a typed HTTP client for the intervals.icu public API.
//
// Authentication uses HTTP Basic Auth with username "API_KEY" and the
// caller's API key as the password. The client honors Retry-After on
// 429 / 5xx responses, applies an exponential backoff with jitter, and
// is rate-limited via a token bucket so backfills stay polite.
//
// The client never logs or returns the API key in error messages.
package icu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// DefaultBaseURL is the production intervals.icu API root.
const DefaultBaseURL = "https://intervals.icu/api/v1"

// SelfAthleteID is the magic id used by intervals.icu to mean "the
// athlete identified by the API key in the current request".
const SelfAthleteID = "0"

// userAgent identifies the client to intervals.icu.
const userAgent = "fit-agent/dev (+https://github.com/jogvan-k/fit-agent)"

// Sentinel errors. Wrap with [HTTPError] for status-code context.
var (
	// ErrUnauthorized is returned for 401 responses.
	ErrUnauthorized = errors.New("intervals.icu: unauthorized")
	// ErrForbidden is returned for 403 responses.
	ErrForbidden = errors.New("intervals.icu: forbidden")
	// ErrNotFound is returned for 404 responses.
	ErrNotFound = errors.New("intervals.icu: not found")
	// ErrRateLimited is returned when retries on 429 are exhausted.
	ErrRateLimited = errors.New("intervals.icu: rate limited")
	// ErrServer is returned when retries on 5xx are exhausted.
	ErrServer = errors.New("intervals.icu: server error")
)

// HTTPError carries the response status code and a short snippet of the
// response body for diagnostics.
type HTTPError struct {
	StatusCode int
	Method     string
	URL        string
	Body       string // truncated to 512 bytes
	Err        error  // sentinel for errors.Is
}

// Error implements [error].
func (e *HTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("intervals.icu %s %s: %d", e.Method, e.URL, e.StatusCode)
	}
	return fmt.Sprintf("intervals.icu %s %s: %d: %s", e.Method, e.URL, e.StatusCode, e.Body)
}

// Unwrap exposes the sentinel error for errors.Is.
func (e *HTTPError) Unwrap() error { return e.Err }

// Options configures a [Client].
type Options struct {
	// BaseURL overrides [DefaultBaseURL]. Useful for tests.
	BaseURL string
	// HTTPClient is the underlying transport. Defaults to a client with
	// a 30s timeout (overridden per-request via context for streaming).
	HTTPClient *http.Client
	// MaxRetries is the maximum number of retry attempts on 429 / 5xx.
	// Defaults to 4.
	MaxRetries int
	// MinBackoff is the initial backoff. Defaults to 500ms.
	MinBackoff time.Duration
	// MaxBackoff caps any single backoff sleep. Defaults to 30s.
	MaxBackoff time.Duration
	// RateLimit is the steady-state requests-per-second budget.
	// Defaults to 4.0 (intervals.icu is generous; we stay polite).
	RateLimit float64
	// Burst is the token-bucket burst. Defaults to 8.
	Burst int
	// Now is injected for deterministic backoff in tests.
	Now func() time.Time
	// Sleep is injected so tests can skip real waits.
	Sleep func(context.Context, time.Duration) error
	// Rand seeds the backoff jitter. Defaults to a fresh rand source.
	Rand *rand.Rand
}

// Client is a typed intervals.icu HTTP client.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	limiter    *rate.Limiter
	maxRetries int
	minBackoff time.Duration
	maxBackoff time.Duration
	now        func() time.Time
	sleep      func(context.Context, time.Duration) error
	rand       *rand.Rand
}

// NewClient returns a new [Client]. apiKey must not be empty.
func NewClient(apiKey string, opts Options) (*Client, error) {
	if apiKey == "" {
		return nil, errors.New("icu: empty API key")
	}
	base := strings.TrimRight(opts.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	maxRetries := opts.MaxRetries
	if maxRetries == 0 {
		maxRetries = 4
	}
	minBackoff := opts.MinBackoff
	if minBackoff == 0 {
		minBackoff = 500 * time.Millisecond
	}
	maxBackoff := opts.MaxBackoff
	if maxBackoff == 0 {
		maxBackoff = 30 * time.Second
	}
	rps := opts.RateLimit
	if rps == 0 {
		rps = 4.0
	}
	burst := opts.Burst
	if burst == 0 {
		burst = 8
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	sleep := opts.Sleep
	if sleep == nil {
		sleep = ctxSleep
	}
	r := opts.Rand
	if r == nil {
		r = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	return &Client{
		baseURL:    base,
		apiKey:     apiKey,
		httpClient: hc,
		limiter:    rate.NewLimiter(rate.Limit(rps), burst),
		maxRetries: maxRetries,
		minBackoff: minBackoff,
		maxBackoff: maxBackoff,
		now:        now,
		sleep:      sleep,
		rand:       r,
	}, nil
}

func ctxSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// do executes req with retries, rate limiting, and Retry-After honoring.
// On success it returns the response; the caller MUST close the body.
//
// req.Body, if non-nil, must be a re-readable buffer (e.g. *bytes.Reader)
// because the request may be retried. The current GET-only call sites do
// not exercise that path, but mutating endpoints will.
func (c *Client) do(ctx context.Context, req *http.Request) (*http.Response, error) {
	req.SetBasicAuth("API_KEY", c.apiKey)
	req.Header.Set("User-Agent", userAgent)
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limit wait: %w", err)
		}
		resp, err := c.httpClient.Do(req.WithContext(ctx))
		if err != nil {
			lastErr = err
			if !shouldRetryNetwork(err) || attempt == c.maxRetries {
				return nil, fmt.Errorf("icu request: %w", err)
			}
			if err := c.sleep(ctx, c.backoff(attempt, 0)); err != nil {
				return nil, err
			}
			continue
		}

		switch {
		case resp.StatusCode == http.StatusUnauthorized:
			return nil, c.httpError(req, resp, ErrUnauthorized)
		case resp.StatusCode == http.StatusForbidden:
			return nil, c.httpError(req, resp, ErrForbidden)
		case resp.StatusCode == http.StatusNotFound:
			return nil, c.httpError(req, resp, ErrNotFound)
		case resp.StatusCode == http.StatusTooManyRequests:
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), c.now())
			drainAndClose(resp)
			if attempt == c.maxRetries {
				return nil, c.statusError(req, resp.StatusCode, ErrRateLimited, "")
			}
			if err := c.sleep(ctx, c.backoff(attempt, retryAfter)); err != nil {
				return nil, err
			}
		case resp.StatusCode >= 500 && resp.StatusCode <= 599:
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"), c.now())
			drainAndClose(resp)
			if attempt == c.maxRetries {
				return nil, c.statusError(req, resp.StatusCode, ErrServer, "")
			}
			if err := c.sleep(ctx, c.backoff(attempt, retryAfter)); err != nil {
				return nil, err
			}
		case resp.StatusCode >= 200 && resp.StatusCode <= 299:
			return resp, nil
		default:
			return nil, c.httpError(req, resp, fmt.Errorf("unexpected status %d", resp.StatusCode))
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("icu request: %w", lastErr)
	}
	return nil, errors.New("icu: retries exhausted")
}

func shouldRetryNetwork(err error) bool {
	// Conservative: retry on temporary I/O failures by default.
	// We treat any non-context error as retryable; context cancellations
	// surface through ctx.Err() in the sleep helper.
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// backoff returns the next sleep duration. retryAfter, when positive,
// takes precedence (capped at MaxBackoff); otherwise we use exponential
// backoff with full jitter.
func (c *Client) backoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		if retryAfter > c.maxBackoff {
			return c.maxBackoff
		}
		return retryAfter
	}
	d := c.minBackoff << attempt
	if d <= 0 || d > c.maxBackoff {
		d = c.maxBackoff
	}
	jitter := time.Duration(c.rand.Int63n(int64(d)))
	return jitter
}

// parseRetryAfter accepts either delta-seconds or HTTP-date.
func parseRetryAfter(s string, now time.Time) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if secs, err := strconv.Atoi(s); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(s); err == nil {
		if d := t.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}

func (c *Client) httpError(req *http.Request, resp *http.Response, sentinel error) *HTTPError {
	body := readSnippet(resp.Body, 512)
	drainAndClose(resp)
	return &HTTPError{
		StatusCode: resp.StatusCode,
		Method:     req.Method,
		URL:        req.URL.String(),
		Body:       body,
		Err:        sentinel,
	}
}

func (c *Client) statusError(req *http.Request, status int, sentinel error, body string) *HTTPError {
	return &HTTPError{
		StatusCode: status,
		Method:     req.Method,
		URL:        req.URL.String(),
		Body:       body,
		Err:        sentinel,
	}
}

func readSnippet(r io.Reader, limit int64) string {
	if r == nil {
		return ""
	}
	b, _ := io.ReadAll(io.LimitReader(r, limit))
	return strings.TrimSpace(string(b))
}

func drainAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// getJSON issues GET path?query and decodes the JSON body into out.
func (c *Client) getJSON(ctx context.Context, path string, query map[string]string, out any) error {
	req, err := c.newRequest(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, req)
	if err != nil {
		return err
	}
	defer drainAndClose(resp)
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

// streamGet issues GET path?query and copies the body to w.
func (c *Client) streamGet(ctx context.Context, path string, query map[string]string, w io.Writer) error {
	req, err := c.newRequest(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "*/*")
	resp, err := c.do(ctx, req)
	if err != nil {
		return err
	}
	defer drainAndClose(resp)
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("stream %s: %w", path, err)
	}
	return nil
}

func (c *Client) newRequest(ctx context.Context, method, path string, query map[string]string, body io.Reader) (*http.Request, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		var b strings.Builder
		first := true
		for k, v := range query {
			if v == "" {
				continue
			}
			if first {
				b.WriteByte('?')
				first = false
			} else {
				b.WriteByte('&')
			}
			b.WriteString(escape(k))
			b.WriteByte('=')
			b.WriteString(escape(v))
		}
		u += b.String()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	return req, nil
}

// escape is a tiny URL-query encoder. We avoid net/url import churn for
// the trivial keys we ship (dates, ids).
func escape(s string) string {
	const hex = "0123456789ABCDEF"
	needs := false
	for i := 0; i < len(s); i++ {
		if !unreserved(s[i]) {
			needs = true
			break
		}
	}
	if !needs {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) * 2)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if unreserved(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&0xF])
	}
	return b.String()
}

func unreserved(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	case c == '-' || c == '_' || c == '.' || c == '~':
		return true
	default:
		return false
	}
}

// expBackoffMs is exposed for tests that want to verify backoff growth.
func expBackoffMs(min time.Duration, attempt int, cap time.Duration) time.Duration {
	d := time.Duration(float64(min) * math.Pow(2, float64(attempt)))
	if d > cap {
		return cap
	}
	return d
}
