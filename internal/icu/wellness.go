package icu

import (
	"context"
	"encoding/json"
	"fmt"
)

// ListWellness calls GET /athlete/{id}/wellness?oldest=&newest=. Both
// dates are inclusive and expressed in athlete-local time as YYYY-MM-DD.
func (c *Client) ListWellness(ctx context.Context, athleteID, oldest, newest string) ([]WellnessDay, error) {
	q := map[string]string{"oldest": oldest, "newest": newest}
	var out []WellnessDay
	if err := c.getJSON(ctx, "/athlete/"+athleteID+"/wellness", q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListWellnessRaw is like [Client.ListWellness] but returns the raw JSON.
func (c *Client) ListWellnessRaw(ctx context.Context, athleteID, oldest, newest string) (json.RawMessage, error) {
	q := map[string]string{"oldest": oldest, "newest": newest}
	var raw json.RawMessage
	if err := c.getJSON(ctx, "/athlete/"+athleteID+"/wellness", q, &raw); err != nil {
		return nil, fmt.Errorf("list wellness: %w", err)
	}
	return raw, nil
}
