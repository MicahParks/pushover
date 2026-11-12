package pushover

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// SubscriptionMigration describes an existing user key to migrate to a subscription.
type SubscriptionMigration struct {
	// Subscription is the subscription's code. Required.
	Subscription string
	// User is the user's existing user key. Required.
	User string
	// DeviceName restricts the subscription to one of the user's devices.
	DeviceName string
	// Sound is the user's preferred default sound for the subscription.
	Sound Sound
}

// MigrateSubscription migrates a user key collected outside of the subscription process to a subscription. It returns
// the new subscribed user key, which should be stored in place of the original user key. See
// https://pushover.net/api/subscriptions
func (c *Client) MigrateSubscription(ctx context.Context, m SubscriptionMigration) (string, error) {
	if m.Subscription == "" {
		return "", fmt.Errorf("%w: subscription is required", ErrInvalidRequest)
	}
	if m.User == "" {
		return "", fmt.Errorf("%w: user is required", ErrInvalidRequest)
	}
	params := url.Values{}
	params.Set("subscription", m.Subscription)
	params.Set("user", m.User)
	if m.DeviceName != "" {
		err := validateDevice(m.DeviceName)
		if err != nil {
			return "", err
		}
		params.Set("device_name", m.DeviceName)
	}
	if m.Sound != "" {
		params.Set("sound", string(m.Sound))
	}
	var resp struct {
		SubscribedUserKey string `json:"subscribed_user_key"`
	}
	r := apiRequest{
		method: http.MethodPost,
		path:   "subscriptions/migrate.json",
		params: params,
	}
	_, err := c.api.do(ctx, r, &resp)
	if err != nil {
		return "", err
	}
	return resp.SubscribedUserKey, nil
}

// SubscribeURL builds the URL that starts the web-based subscription process. subscriptionURL is the subscription URL
// shown in the application's settings, such as https://pushover.net/subscribe/Example-abc123. After the user
// subscribes, Pushover redirects them to successURL, see ParseSubscriptionRedirect. If the user declines, they are
// redirected to failureURL, or to the subscription's base URL if failureURL is empty.
//
// Both successURL and failureURL must start with the subscription's base URL. To prevent forged redirects, include a
// random value tied to the user's session in successURL and verify it when the user returns.
func SubscribeURL(subscriptionURL, successURL, failureURL string) (string, error) {
	u, err := url.Parse(subscriptionURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", fmt.Errorf("%w: subscription URL must be an absolute HTTP or HTTPS URL", ErrInvalidRequest)
	}
	if successURL == "" {
		return "", fmt.Errorf("%w: success URL is required", ErrInvalidRequest)
	}
	q := u.Query()
	q.Set("success", successURL)
	if failureURL != "" {
		q.Set("failure", failureURL)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// SubscriptionRedirect is the result of the web-based subscription process, passed by Pushover as query parameters
// when it redirects the user to the success URL.
type SubscriptionRedirect struct {
	// UserKey is the user's new subscribed user key. It's empty if the user unsubscribed.
	UserKey string
	// Unsubscribed reports whether an already subscribed user chose to unsubscribe. Any stored key for the user
	// should be deleted.
	Unsubscribed bool
	// UnsubscribedUserKey is the user key that was deleted when the user unsubscribed.
	UnsubscribedUserKey string
}

// ParseSubscriptionRedirect reads the result of the web-based subscription process from the query parameters of the
// request to the success URL, such as r.URL.Query().
func ParseSubscriptionRedirect(query url.Values) SubscriptionRedirect {
	s := SubscriptionRedirect{
		UserKey:             query.Get("pushover_user_key"),
		Unsubscribed:        query.Get("pushover_unsubscribed") == "1",
		UnsubscribedUserKey: query.Get("pushover_unsubscribed_user_key"),
	}
	return s
}
