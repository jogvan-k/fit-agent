package icu

import (
	"context"
	"encoding/json"
	"fmt"
)

// GetAthlete fetches /athlete/{id}. Pass [SelfAthleteID] ("0") to resolve
// the athlete identified by the API key.
func (c *Client) GetAthlete(ctx context.Context, id string) (*Athlete, error) {
	var a Athlete
	if err := c.getJSON(ctx, "/athlete/"+id, nil, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// GetAthleteRaw is like [Client.GetAthlete] but returns the unmodified
// JSON bytes. Used when caching the response so unknown fields survive.
func (c *Client) GetAthleteRaw(ctx context.Context, id string) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.getJSON(ctx, "/athlete/"+id, nil, &raw); err != nil {
		return nil, fmt.Errorf("get athlete %s: %w", id, err)
	}
	return raw, nil
}
