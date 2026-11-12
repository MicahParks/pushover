package pushover

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// Limits describe the monthly message limits for the account that owns an application. The limit is shared by all
// applications on a user or Team account.
type Limits struct {
	// Limit is the account's monthly message limit, including any purchased capacity.
	Limit int
	// Remaining is the number of messages that can still be sent this month.
	Remaining int
	// Reset is when the monthly count resets.
	Reset time.Time
}

// Limits returns the monthly message limits for the account that owns the application.
func (c *Client) Limits(ctx context.Context) (Limits, error) {
	var resp struct {
		Limit     int   `json:"limit"`
		Remaining int   `json:"remaining"`
		Reset     int64 `json:"reset"`
	}
	r := apiRequest{
		method: http.MethodGet,
		path:   "apps/limits.json",
	}
	_, err := c.api.do(ctx, r, &resp)
	if err != nil {
		return Limits{}, err
	}
	l := Limits{
		Limit:     resp.Limit,
		Remaining: resp.Remaining,
		Reset:     unixTime(resp.Reset),
	}
	return l, nil
}

// parseLimitHeaders reads the X-Limit-App-* headers that Pushover sends with responses from the Message API. It
// returns nil if the headers are absent or malformed.
func parseLimitHeaders(h http.Header) *Limits {
	rawLimit := h.Get("X-Limit-App-Limit")
	rawRemaining := h.Get("X-Limit-App-Remaining")
	rawReset := h.Get("X-Limit-App-Reset")
	if rawLimit == "" || rawRemaining == "" || rawReset == "" {
		return nil
	}
	limit, err := strconv.Atoi(rawLimit)
	if err != nil {
		return nil
	}
	remaining, err := strconv.Atoi(rawRemaining)
	if err != nil {
		return nil
	}
	reset, err := strconv.ParseInt(rawReset, 10, 64)
	if err != nil {
		return nil
	}
	l := &Limits{
		Limit:     limit,
		Remaining: remaining,
		Reset:     unixTime(reset),
	}
	return l
}
