package pushover

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Receipt is the status of an emergency-priority message. See https://pushover.net/api/receipts
type Receipt struct {
	// Acknowledged reports whether a user acknowledged the message.
	Acknowledged bool
	// AcknowledgedAt is when the message was acknowledged, or the zero time.
	AcknowledgedAt time.Time
	// AcknowledgedBy is the user key of the user that first acknowledged the message.
	AcknowledgedBy string
	// AcknowledgedByDevice is the device name of the user that first acknowledged the message.
	AcknowledgedByDevice string
	// LastDeliveredAt is when the message was last retried, or the zero time.
	LastDeliveredAt time.Time
	// Expired reports whether the message has expired and is no longer retried.
	Expired bool
	// ExpiresAt is when the message stops being retried.
	ExpiresAt time.Time
	// CalledBack reports whether Pushover requested the message's callback URL.
	CalledBack bool
	// CalledBackAt is when Pushover requested the callback URL, or the zero time.
	CalledBackAt time.Time
}

// Receipt gets the status of an emergency-priority message using the receipt from SendResponse.Receipt. Pushover asks
// that receipts are polled no more than once every 5 seconds. Receipts are available for 1 week after the message was
// received.
func (c *Client) Receipt(ctx context.Context, receipt string) (Receipt, error) {
	if receipt == "" {
		return Receipt{}, fmt.Errorf("%w: receipt is required", ErrInvalidRequest)
	}
	var resp struct {
		Acknowledged         flexBool `json:"acknowledged"`
		AcknowledgedAt       int64    `json:"acknowledged_at"`
		AcknowledgedBy       string   `json:"acknowledged_by"`
		AcknowledgedByDevice string   `json:"acknowledged_by_device"`
		LastDeliveredAt      int64    `json:"last_delivered_at"`
		Expired              flexBool `json:"expired"`
		ExpiresAt            int64    `json:"expires_at"`
		CalledBack           flexBool `json:"called_back"`
		CalledBackAt         int64    `json:"called_back_at"`
	}
	r := apiRequest{
		method: http.MethodGet,
		path:   "receipts/" + url.PathEscape(receipt) + ".json",
	}
	_, err := c.api.do(ctx, r, &resp)
	if err != nil {
		return Receipt{}, err
	}
	rec := Receipt{
		Acknowledged:         bool(resp.Acknowledged),
		AcknowledgedAt:       unixTime(resp.AcknowledgedAt),
		AcknowledgedBy:       resp.AcknowledgedBy,
		AcknowledgedByDevice: resp.AcknowledgedByDevice,
		LastDeliveredAt:      unixTime(resp.LastDeliveredAt),
		Expired:              bool(resp.Expired),
		ExpiresAt:            unixTime(resp.ExpiresAt),
		CalledBack:           bool(resp.CalledBack),
		CalledBackAt:         unixTime(resp.CalledBackAt),
	}
	return rec, nil
}

// CancelReceipt stops retrying an emergency-priority message before it expires.
func (c *Client) CancelReceipt(ctx context.Context, receipt string) error {
	if receipt == "" {
		return fmt.Errorf("%w: receipt is required", ErrInvalidRequest)
	}
	r := apiRequest{
		method: http.MethodPost,
		path:   "receipts/" + url.PathEscape(receipt) + "/cancel.json",
		retry:  retryIdempotent,
	}
	_, err := c.api.do(ctx, r, nil)
	return err
}

// CancelByTag stops retrying all active emergency-priority messages sent by the application with the given tag. See
// Message.Tags.
func (c *Client) CancelByTag(ctx context.Context, tag string) error {
	if tag == "" {
		return fmt.Errorf("%w: tag is required", ErrInvalidRequest)
	}
	r := apiRequest{
		method: http.MethodPost,
		path:   "receipts/cancel_by_tag/" + url.PathEscape(tag) + ".json",
		retry:  retryIdempotent,
	}
	_, err := c.api.do(ctx, r, nil)
	return err
}

// Callback is the request Pushover sends to Message.Callback when an emergency-priority message is acknowledged.
type Callback struct {
	// Receipt identifies the acknowledged message.
	Receipt string
	// AcknowledgedAt is when the message was acknowledged.
	AcknowledgedAt time.Time
	// AcknowledgedBy is the user key of the user that first acknowledged the message.
	AcknowledgedBy string
	// AcknowledgedByDevice is the device name of the user that first acknowledged the message.
	AcknowledgedByDevice string
}

// ParseCallback parses an acknowledgment callback request sent by Pushover.
//
// Pushover does not sign callback requests. To make sure a callback is genuine, include a secret that's unique to the
// message in the callback URL and check it, or confirm the acknowledgment with Client.Receipt.
func ParseCallback(r *http.Request) (Callback, error) {
	if r.Method != http.MethodPost {
		return Callback{}, fmt.Errorf("%w: callback must use the POST method, got %s", ErrInvalidRequest, r.Method)
	}
	err := r.ParseForm()
	if err != nil {
		return Callback{}, fmt.Errorf("%w: failed to parse callback form: %w", ErrInvalidRequest, err)
	}
	receipt := r.PostForm.Get("receipt")
	if receipt == "" {
		return Callback{}, fmt.Errorf("%w: callback is missing the receipt parameter", ErrInvalidRequest)
	}
	var acknowledgedAt time.Time
	if raw := r.PostForm.Get("acknowledged_at"); raw != "" {
		sec, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return Callback{}, fmt.Errorf("%w: callback has an invalid acknowledged_at parameter: %w", ErrInvalidRequest, err)
		}
		acknowledgedAt = unixTime(sec)
	}
	cb := Callback{
		Receipt:              receipt,
		AcknowledgedAt:       acknowledgedAt,
		AcknowledgedBy:       r.PostForm.Get("acknowledged_by"),
		AcknowledgedByDevice: r.PostForm.Get("acknowledged_by_device"),
	}
	return cb, nil
}

// CallbackHandler returns an http.Handler that receives acknowledgment callbacks from Pushover and passes them to fn.
//
// The handler responds with HTTP 200 if fn returns nil. If fn returns an error, the handler responds with HTTP 500 and
// Pushover will retry the callback in one minute. Invalid requests receive HTTP 400, or HTTP 405 for methods other
// than POST.
//
// See ParseCallback for how to verify that a callback is genuine.
func CallbackHandler(fn func(ctx context.Context, cb Callback) error) http.Handler {
	const maxCallbackBodySize = 64 << 10
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCallbackBodySize)
		cb, err := ParseCallback(r)
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		err = fn(r.Context(), cb)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}
