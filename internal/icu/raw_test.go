package icu

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRawListAccessors(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"id":"x","extra":"survives"}]`)
	})
	c, _ := newTestClient(t, h)
	ctx := context.Background()

	for name, fn := range map[string]func() ([]byte, error){
		"activities": func() ([]byte, error) {
			raw, err := c.ListActivitiesRaw(ctx, "i1", "2026-01-01", "2026-01-31")
			return raw, err
		},
		"wellness": func() ([]byte, error) {
			raw, err := c.ListWellnessRaw(ctx, "i1", "2026-01-01", "2026-01-31")
			return raw, err
		},
		"events": func() ([]byte, error) {
			raw, err := c.ListEventsRaw(ctx, "i1", "2026-01-01", "2026-01-31", EventCategoryWorkout)
			return raw, err
		},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := fn()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), "extra") {
				t.Errorf("raw missing extra field: %s", raw)
			}
		})
	}
}

func TestActivityRawAndForbidden(t *testing.T) {
	var status int
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		_, _ = io.WriteString(w, `{"id":"a1","extra":"survives"}`)
	})
	c, _ := newTestClient(t, h)
	raw, err := c.GetActivityRaw(context.Background(), "a1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "extra") {
		t.Errorf("raw = %s", raw)
	}

	status = 403
	_, err = c.GetActivity(context.Background(), "a1")
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("got %v, want ErrForbidden", err)
	}
	var he *HTTPError
	if !errors.As(err, &he) {
		t.Fatal("not an HTTPError")
	}
	// Exercise Error() formatting (with and without body).
	msg := he.Error()
	if !strings.Contains(msg, "403") {
		t.Errorf("error message = %q", msg)
	}
	emptyHE := &HTTPError{StatusCode: 500, Method: "GET", URL: "/x", Err: ErrServer}
	if !strings.Contains(emptyHE.Error(), "500") {
		t.Errorf("empty-body error = %q", emptyHE.Error())
	}
}

func TestUnexpectedStatus(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(307)
	})
	c, _ := newTestClient(t, h)
	_, err := c.GetAthlete(context.Background(), "i1")
	if err == nil {
		t.Fatal("expected error for unexpected status")
	}
}
