package icu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// ListActivities calls GET /athlete/{id}/activities?oldest=&newest=.
//
// Both dates are inclusive and expressed in athlete-local time as
// YYYY-MM-DD strings.
func (c *Client) ListActivities(ctx context.Context, athleteID, oldest, newest string) ([]ActivitySummary, error) {
	q := map[string]string{"oldest": oldest, "newest": newest}
	var out []ActivitySummary
	if err := c.getJSON(ctx, "/athlete/"+athleteID+"/activities", q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListActivitiesRaw is like [Client.ListActivities] but returns the raw
// JSON for caching.
func (c *Client) ListActivitiesRaw(ctx context.Context, athleteID, oldest, newest string) (json.RawMessage, error) {
	q := map[string]string{"oldest": oldest, "newest": newest}
	var raw json.RawMessage
	if err := c.getJSON(ctx, "/athlete/"+athleteID+"/activities", q, &raw); err != nil {
		return nil, fmt.Errorf("list activities: %w", err)
	}
	return raw, nil
}

// GetActivity fetches a single /activity/{id}.
func (c *Client) GetActivity(ctx context.Context, id string) (*ActivitySummary, error) {
	var a ActivitySummary
	if err := c.getJSON(ctx, "/activity/"+id, nil, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// GetActivityRaw returns the raw JSON for a single /activity/{id}.
func (c *Client) GetActivityRaw(ctx context.Context, id string) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.getJSON(ctx, "/activity/"+id, nil, &raw); err != nil {
		return nil, fmt.Errorf("get activity %s: %w", id, err)
	}
	return raw, nil
}

// GetActivityFIT streams /activity/{id}/fit-file to w.
//
// Caller is responsible for the lifecycle of w (e.g. closing a file
// handle). The body is fully consumed even on success.
func (c *Client) GetActivityFIT(ctx context.Context, id string, w io.Writer) error {
	return c.streamGet(ctx, "/activity/"+id+"/fit-file", nil, w)
}
