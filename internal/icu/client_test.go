package icu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient returns a Client with retries cranked down and the
// injectable sleep replaced with a no-op so tests are fast.
func newTestClient(t *testing.T, h http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := NewClient("test-key", Options{
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
		MaxRetries: 3,
		MinBackoff: time.Millisecond,
		MaxBackoff: time.Millisecond,
		RateLimit:  1000,
		Burst:      1000,
		Sleep:      func(ctx context.Context, d time.Duration) error { return nil },
		Rand:       rand.New(rand.NewSource(1)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return c, srv
}

func TestNewClientRequiresKey(t *testing.T) {
	if _, err := NewClient("", Options{}); err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestBasicAuthAndUserAgent(t *testing.T) {
	var gotUser, gotPass string
	var gotUA string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, _ = r.BasicAuth()
		gotUA = r.Header.Get("User-Agent")
		_, _ = io.WriteString(w, `{"id":"i123","name":"Test"}`)
	})
	c, _ := newTestClient(t, h)
	if _, err := c.GetAthlete(context.Background(), "i123"); err != nil {
		t.Fatal(err)
	}
	if gotUser != "API_KEY" {
		t.Errorf("user = %q, want API_KEY", gotUser)
	}
	if gotPass != "test-key" {
		t.Errorf("pass = %q, want test-key", gotPass)
	}
	if !strings.HasPrefix(gotUA, "fit-agent/") {
		t.Errorf("user-agent = %q, want fit-agent/...", gotUA)
	}
}

func TestGetAthleteHappyPath(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/athlete/0" {
			t.Errorf("path = %q, want /athlete/0", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"i42","name":"Self","timezone":"Europe/Copenhagen","icu_ftp":250}`)
	})
	c, _ := newTestClient(t, h)
	a, err := c.GetAthlete(context.Background(), SelfAthleteID)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != "i42" || a.FTP != 250 || a.Timezone != "Europe/Copenhagen" {
		t.Errorf("athlete = %+v", a)
	}
}

func TestUnauthorized(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = io.WriteString(w, `unauthorized`)
	})
	c, _ := newTestClient(t, h)
	_, err := c.GetAthlete(context.Background(), "i1")
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("got %v, want ErrUnauthorized", err)
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.StatusCode != 401 {
		t.Errorf("expected HTTPError 401, got %v", err)
	}
}

func TestNotFound(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	c, _ := newTestClient(t, h)
	_, err := c.GetActivity(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestRateLimitRetryThenSucceed(t *testing.T) {
	var calls int32
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		_, _ = io.WriteString(w, `{"id":"i1","name":"OK"}`)
	})
	c, _ := newTestClient(t, h)
	a, err := c.GetAthlete(context.Background(), "i1")
	if err != nil {
		t.Fatalf("got %v", err)
	}
	if a.ID != "i1" {
		t.Errorf("id = %q", a.ID)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("calls = %d, want 3", got)
	}
}

func TestRateLimitExhausted(t *testing.T) {
	var calls int32
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(429)
	})
	c, _ := newTestClient(t, h)
	_, err := c.GetAthlete(context.Background(), "i1")
	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("got %v, want ErrRateLimited", err)
	}
	if got := atomic.LoadInt32(&calls); got != 4 { // initial + 3 retries
		t.Errorf("calls = %d, want 4", got)
	}
}

func TestServerErrorRetryThenFail(t *testing.T) {
	var calls int32
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(503)
	})
	c, _ := newTestClient(t, h)
	_, err := c.GetAthlete(context.Background(), "i1")
	if !errors.Is(err, ErrServer) {
		t.Errorf("got %v, want ErrServer", err)
	}
	if got := atomic.LoadInt32(&calls); got != 4 {
		t.Errorf("calls = %d, want 4", got)
	}
}

func TestMalformedJSON(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{not json`)
	})
	c, _ := newTestClient(t, h)
	_, err := c.GetAthlete(context.Background(), "i1")
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestListActivitiesQueryParams(t *testing.T) {
	var gotPath, gotQuery string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `[{"id":"a1","name":"Run","type":"Run","start_date_local":"2026-05-03T07:00:00"}]`)
	})
	c, _ := newTestClient(t, h)
	acts, err := c.ListActivities(context.Background(), "i42", "2026-05-01", "2026-05-31")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/athlete/i42/activities" {
		t.Errorf("path = %q", gotPath)
	}
	if !strings.Contains(gotQuery, "oldest=2026-05-01") || !strings.Contains(gotQuery, "newest=2026-05-31") {
		t.Errorf("query = %q", gotQuery)
	}
	if len(acts) != 1 || acts[0].ID != "a1" {
		t.Errorf("acts = %+v", acts)
	}
}

func TestFITStreaming(t *testing.T) {
	payload := bytes.Repeat([]byte{0x42}, 4096)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/activity/i9/fit-file" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(payload)
	})
	c, _ := newTestClient(t, h)
	var buf bytes.Buffer
	if err := c.GetActivityFIT(context.Background(), "i9", &buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), payload) {
		t.Errorf("payload mismatch: got %d bytes, want %d", buf.Len(), len(payload))
	}
}

func TestListWellness(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"id":"2026-05-01","restingHR":48,"hrv":72.0}]`)
	})
	c, _ := newTestClient(t, h)
	rows, err := c.ListWellness(context.Background(), "i1", "2026-05-01", "2026-05-31")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].RestingHR != 48 {
		t.Errorf("rows = %+v", rows)
	}
}

func TestEventsCRUD(t *testing.T) {
	var lastMethod, lastPath string
	var lastBody []byte
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastMethod = r.Method
		lastPath = r.URL.Path
		lastBody, _ = io.ReadAll(r.Body)
		switch r.Method {
		case http.MethodGet:
			_, _ = io.WriteString(w, `[{"id":1,"start_date_local":"2026-05-04T00:00:00","category":"WORKOUT","name":"Z2"}]`)
		case http.MethodPost:
			_, _ = io.WriteString(w, `{"id":42,"start_date_local":"2026-05-04T00:00:00","category":"WORKOUT","name":"Z2"}`)
		case http.MethodPut:
			_, _ = io.WriteString(w, `{"id":42,"start_date_local":"2026-05-04T00:00:00","category":"WORKOUT","name":"Z2 updated"}`)
		case http.MethodDelete:
			w.WriteHeader(204)
		default:
			w.WriteHeader(400)
		}
	})
	c, _ := newTestClient(t, h)
	ctx := context.Background()

	events, err := c.ListEvents(ctx, "i1", "2026-05-01", "2026-05-31", EventCategoryWorkout)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != 1 {
		t.Errorf("list events: %+v", events)
	}

	created, err := c.CreateEvent(ctx, "i1", Event{Name: "Z2", Category: EventCategoryWorkout, StartDateLocal: "2026-05-04T00:00:00"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != 42 {
		t.Errorf("created.ID = %d, want 42", created.ID)
	}
	if lastMethod != "POST" || lastPath != "/athlete/i1/events" {
		t.Errorf("create call: %s %s", lastMethod, lastPath)
	}
	var body map[string]any
	if err := json.Unmarshal(lastBody, &body); err != nil {
		t.Fatal(err)
	}
	if body["name"] != "Z2" {
		t.Errorf("body.name = %v", body["name"])
	}

	upd, err := c.UpdateEvent(ctx, "i1", Event{ID: 42, Name: "Z2 updated", Category: EventCategoryWorkout, StartDateLocal: "2026-05-04T00:00:00"})
	if err != nil {
		t.Fatal(err)
	}
	if upd.Name != "Z2 updated" || lastPath != "/athlete/i1/events/42" || lastMethod != "PUT" {
		t.Errorf("update: %+v %s %s", upd, lastMethod, lastPath)
	}

	if err := c.DeleteEvent(ctx, "i1", 42); err != nil {
		t.Fatal(err)
	}
	if lastMethod != "DELETE" || lastPath != "/athlete/i1/events/42" {
		t.Errorf("delete call: %s %s", lastMethod, lastPath)
	}
}

func TestUpdateEventRequiresID(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	if _, err := c.UpdateEvent(context.Background(), "i1", Event{Name: "x"}); err == nil {
		t.Error("expected error for missing ID")
	}
}

// TestCreateEventDecodesObjectWorkoutDoc is a regression test for
// https://github.com/jogvan-k/fit-agent/issues/1: intervals.icu changed
// `workout_doc` from a string to an object, which used to break decoding
// even though the event was successfully created server-side.
func TestCreateEventDecodesObjectWorkoutDoc(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":99,"start_date_local":"2026-05-11T00:00:00","category":"WORKOUT","name":"Easy","workout_doc":{"steps":[{"duration":600,"power":"Z2"}]}}`)
	})
	c, _ := newTestClient(t, h)
	ev, err := c.CreateEvent(context.Background(), "i1", Event{Name: "Easy", Category: EventCategoryWorkout, StartDateLocal: "2026-05-11T00:00:00"})
	if err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	if ev.ID != 99 {
		t.Errorf("ID = %d, want 99", ev.ID)
	}
	if len(ev.WorkoutDoc) == 0 || ev.WorkoutDoc[0] != '{' {
		t.Errorf("WorkoutDoc = %q, want JSON object", string(ev.WorkoutDoc))
	}
}

// TestCreateEventDecodesStringWorkoutDoc keeps the legacy string shape
// working too, in case intervals.icu ever falls back to it for some
// endpoints.
func TestCreateEventDecodesStringWorkoutDoc(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":100,"start_date_local":"2026-05-11T00:00:00","category":"WORKOUT","name":"Easy","workout_doc":"- 10m Z2"}`)
	})
	c, _ := newTestClient(t, h)
	ev, err := c.CreateEvent(context.Background(), "i1", Event{Name: "Easy", Category: EventCategoryWorkout, StartDateLocal: "2026-05-11T00:00:00"})
	if err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	if ev.ID != 100 {
		t.Errorf("ID = %d, want 100", ev.ID)
	}
	if string(ev.WorkoutDoc) != `"- 10m Z2"` {
		t.Errorf("WorkoutDoc = %q, want quoted string", string(ev.WorkoutDoc))
	}
}

// TestCreateEventDecodesBoolIndoor is a regression test for the
// `cannot unmarshal bool into Go struct field Event.indoor` error
// observed in `render planned`: intervals.icu returns the `indoor`
// field as a JSON bool, not a string.
func TestCreateEventDecodesBoolIndoor(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":101,"start_date_local":"2026-05-11T00:00:00","category":"WORKOUT","name":"Trainer","indoor":true}`)
	})
	c, _ := newTestClient(t, h)
	ev, err := c.CreateEvent(context.Background(), "i1", Event{Name: "Trainer", Category: EventCategoryWorkout, StartDateLocal: "2026-05-11T00:00:00"})
	if err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	if ev.Indoor == nil || !*ev.Indoor {
		t.Errorf("Indoor = %v, want pointer to true", ev.Indoor)
	}
}

// TestCreateEventOmitsIndoorWhenAbsent confirms a missing icu `indoor`
// field decodes to a nil pointer (so re-encoding drops it via
// omitempty rather than emitting a stray "indoor": false).
func TestCreateEventOmitsIndoorWhenAbsent(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":102,"start_date_local":"2026-05-11T00:00:00","category":"WORKOUT","name":"Outside"}`)
	})
	c, _ := newTestClient(t, h)
	ev, err := c.CreateEvent(context.Background(), "i1", Event{Name: "Outside", Category: EventCategoryWorkout, StartDateLocal: "2026-05-11T00:00:00"})
	if err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	if ev.Indoor != nil {
		t.Errorf("Indoor = %v, want nil", ev.Indoor)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"5", 5 * time.Second},
		{"0", 0},
		{"abc", 0},
		{now.Add(10 * time.Second).Format(http.TimeFormat), 10 * time.Second},
		{now.Add(-1 * time.Hour).Format(http.TimeFormat), 0},
	}
	for _, tc := range cases {
		got := parseRetryAfter(tc.in, now)
		// HTTP-date precision is 1s; allow a small slack.
		diff := got - tc.want
		if diff < -time.Second || diff > time.Second {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestEscapeQuery(t *testing.T) {
	cases := map[string]string{
		"hello":      "hello",
		"a b":        "a%20b",
		"2026-05-01": "2026-05-01",
		"i&j":        "i%26j",
		"a=b":        "a%3Db",
	}
	for in, want := range cases {
		if got := escape(in); got != want {
			t.Errorf("escape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExpBackoff(t *testing.T) {
	got := expBackoffMs(time.Second, 3, 10*time.Second)
	want := 8 * time.Second
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := expBackoffMs(time.Second, 10, time.Second); got != time.Second {
		t.Errorf("cap not respected: %v", got)
	}
}

func TestContextCancellation(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	c, _ := newTestClient(t, h)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.GetAthlete(ctx, "i1")
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestRawAccessors(t *testing.T) {
	body := `{"id":"i1","unknown":"survives"}`
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, body)
	})
	c, _ := newTestClient(t, h)
	raw, err := c.GetAthleteRaw(context.Background(), "i1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "unknown") {
		t.Errorf("raw missing unknown field: %s", raw)
	}
}
