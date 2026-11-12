package pushover

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// UserValidation is the result of validating a user or group key.
type UserValidation struct {
	// Group reports whether the key belongs to a group rather than a user.
	Group bool
	// Devices are the names of the user's active devices.
	Devices []string
	// Licenses are the platforms the user's account is licensed for, such as "iOS", "Android", or "Desktop".
	Licenses []string
}

// ValidateUser checks that a user or group key is valid and has at least one active device. If device is not empty,
// it also checks that the named device is active.
//
// If the key or device is invalid, Pushover responds with an error, which is returned as an *APIError.
func (c *Client) ValidateUser(ctx context.Context, user, device string) (UserValidation, error) {
	if user == "" {
		return UserValidation{}, fmt.Errorf("%w: user is required", ErrInvalidRequest)
	}
	params := url.Values{}
	params.Set("user", user)
	if device != "" {
		params.Set("device", device)
	}
	var resp struct {
		Group    flexBool `json:"group"`
		Devices  []string `json:"devices"`
		Licenses []string `json:"licenses"`
	}
	r := apiRequest{
		method: http.MethodPost,
		path:   "users/validate.json",
		params: params,
		retry:  retryIdempotent,
	}
	_, err := c.api.do(ctx, r, &resp)
	if err != nil {
		return UserValidation{}, err
	}
	v := UserValidation{
		Group:    bool(resp.Group),
		Devices:  resp.Devices,
		Licenses: resp.Licenses,
	}
	return v, nil
}
