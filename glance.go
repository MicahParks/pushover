package pushover

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"unicode/utf8"
)

// MaxGlanceTextLength is the maximum length of the Title, Text, and Subtext of a Glance.
const MaxGlanceTextLength = 100

// Glance updates the data shown in a user's Pushover widgets, such as an Apple Watch complication.
// See https://pushover.net/api/glances
//
// Fields that are nil are left unchanged. To clear a text field, set it to a pointer to an empty string. At least one
// of Title, Text, Subtext, Count, or Percent is required.
type Glance struct {
	// User is the user key of the user whose widgets are updated. Required.
	User string
	// Device restricts the update to one of the user's devices. If empty, all of the user's widgets are updated.
	Device string
	// Title describes the data, such as "Widgets Sold". At most MaxGlanceTextLength characters.
	Title *string
	// Text is the main line of data. At most MaxGlanceTextLength characters.
	Text *string
	// Subtext is a second line of data. At most MaxGlanceTextLength characters.
	Subtext *string
	// Count is a number shown on smaller screens. It may be negative.
	Count *int
	// Percent is a value from 0 to 100, inclusive, shown as a progress indicator.
	Percent *int
}

// UpdateGlance sends new data to a user's widgets.
//
// Pushover does not throttle the Glances API, but watchOS only accepts about 50 updates per day, so Pushover
// recommends no more than one update every 20 minutes. Updates may take up to 10 minutes to appear.
func (c *Client) UpdateGlance(ctx context.Context, g Glance) error {
	if g.User == "" {
		return fmt.Errorf("%w: user is required", ErrInvalidRequest)
	}
	if g.Title == nil && g.Text == nil && g.Subtext == nil && g.Count == nil && g.Percent == nil {
		return fmt.Errorf("%w: at least one of title, text, subtext, count, or percent is required", ErrInvalidRequest)
	}
	params := url.Values{}
	params.Set("user", g.User)
	if g.Device != "" {
		err := validateDevice(g.Device)
		if err != nil {
			return err
		}
		params.Set("device", g.Device)
	}
	text := []struct {
		name  string
		value *string
	}{
		{name: "title", value: g.Title},
		{name: "text", value: g.Text},
		{name: "subtext", value: g.Subtext},
	}
	for _, t := range text {
		if t.value == nil {
			continue
		}
		if n := utf8.RuneCountInString(*t.value); n > MaxGlanceTextLength {
			return fmt.Errorf("%w: glance %s is %d characters, the maximum is %d", ErrInvalidRequest, t.name, n, MaxGlanceTextLength)
		}
		params.Set(t.name, *t.value)
	}
	if g.Count != nil {
		params.Set("count", strconv.Itoa(*g.Count))
	}
	if g.Percent != nil {
		if *g.Percent < 0 || *g.Percent > 100 {
			return fmt.Errorf("%w: glance percent must be from 0 to 100, got %d", ErrInvalidRequest, *g.Percent)
		}
		params.Set("percent", strconv.Itoa(*g.Percent))
	}
	r := apiRequest{
		method: http.MethodPost,
		path:   "glances.json",
		params: params,
		retry:  retryIdempotent,
	}
	_, err := c.api.do(ctx, r, nil)
	return err
}
