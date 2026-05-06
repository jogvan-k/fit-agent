package icu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

// EventCategoryWorkout is the category filter for planned workouts.
const EventCategoryWorkout = "WORKOUT"

// ListEvents calls GET /athlete/{id}/events?oldest=&newest=&category=.
//
// category may be empty to fetch every category; the v1 use case is
// always [EventCategoryWorkout].
func (c *Client) ListEvents(ctx context.Context, athleteID, oldest, newest, category string) ([]Event, error) {
	q := map[string]string{"oldest": oldest, "newest": newest, "category": category}
	var out []Event
	if err := c.getJSON(ctx, "/athlete/"+athleteID+"/events", q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListEventsRaw is like [Client.ListEvents] but returns the raw JSON.
func (c *Client) ListEventsRaw(ctx context.Context, athleteID, oldest, newest, category string) (json.RawMessage, error) {
	q := map[string]string{"oldest": oldest, "newest": newest, "category": category}
	var raw json.RawMessage
	if err := c.getJSON(ctx, "/athlete/"+athleteID+"/events", q, &raw); err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	return raw, nil
}

// CreateEvent POSTs a new planned workout. The returned event includes
// the server-assigned ID.
func (c *Client) CreateEvent(ctx context.Context, athleteID string, ev Event) (*Event, error) {
	body, err := json.Marshal(ev)
	if err != nil {
		return nil, fmt.Errorf("marshal event: %w", err)
	}
	req, err := c.newRequest(ctx, http.MethodPost, "/athlete/"+athleteID+"/events", nil, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer drainAndClose(resp)
	var out Event
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode created event: %w", err)
	}
	return &out, nil
}

// UpdateEvent PUTs changes to an existing planned workout.
func (c *Client) UpdateEvent(ctx context.Context, athleteID string, ev Event) (*Event, error) {
	if ev.ID == 0 {
		return nil, fmt.Errorf("update event: missing ID")
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return nil, fmt.Errorf("marshal event: %w", err)
	}
	path := "/athlete/" + athleteID + "/events/" + strconv.FormatInt(ev.ID, 10)
	req, err := c.newRequest(ctx, http.MethodPut, path, nil, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer drainAndClose(resp)
	var out Event
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		// Some endpoints return empty 200; tolerate.
		if err == io.EOF {
			return &ev, nil
		}
		return nil, fmt.Errorf("decode updated event: %w", err)
	}
	return &out, nil
}

// DeleteEvent removes a planned workout.
func (c *Client) DeleteEvent(ctx context.Context, athleteID string, id int64) error {
	path := "/athlete/" + athleteID + "/events/" + strconv.FormatInt(id, 10)
	req, err := c.newRequest(ctx, http.MethodDelete, path, nil, nil)
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, req)
	if err != nil {
		return err
	}
	drainAndClose(resp)
	return nil
}
