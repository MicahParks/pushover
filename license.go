package pushover

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// LicenseOS is the platform a license is assigned to. Values other than the constants below are passed to Pushover
// as is.
type LicenseOS string

// Platforms for license assignment.
const (
	// LicenseOSAny assigns the license to the first platform the user registers a device on, or to the platform of
	// the user's first unlicensed device. This is recommended by Pushover.
	LicenseOSAny LicenseOS = ""
	// LicenseOSAndroid assigns an Android license.
	LicenseOSAndroid LicenseOS = "Android"
	// LicenseOSiOS assigns an iOS license.
	LicenseOSiOS LicenseOS = "iOS"
	// LicenseOSDesktop assigns a Desktop license.
	LicenseOSDesktop LicenseOS = "Desktop"
)

// LicenseAssignment describes a license to assign. Exactly one of User or Email is required.
type LicenseAssignment struct {
	// User is the user key of an existing Pushover user.
	User string
	// Email is the email address of a Pushover user. If no account exists with this address yet, the license is
	// assigned when one is created. The credit is deducted immediately.
	Email string
	// OS is the platform to license. Defaults to LicenseOSAny.
	OS LicenseOS
}

// AssignLicense permanently assigns one license credit of the application to a user, and returns the number of
// credits remaining. See https://pushover.net/api/licensing
//
// License assignments can't be revoked, so this request is never retried automatically, regardless of
// Options.RetryMax. If it fails with a temporary error, check LicenseCredits before trying again.
func (c *Client) AssignLicense(ctx context.Context, a LicenseAssignment) (int, error) {
	if (a.User == "") == (a.Email == "") {
		return 0, fmt.Errorf("%w: exactly one of user or email is required", ErrInvalidRequest)
	}
	params := url.Values{}
	if a.User != "" {
		params.Set("user", a.User)
	}
	if a.Email != "" {
		params.Set("email", a.Email)
	}
	if a.OS != LicenseOSAny {
		params.Set("os", string(a.OS))
	}
	var resp struct {
		Credits int `json:"credits"`
	}
	r := apiRequest{
		method: http.MethodPost,
		path:   "licenses/assign.json",
		params: params,
		retry:  retryNever,
	}
	_, err := c.api.do(ctx, r, &resp)
	if err != nil {
		return 0, err
	}
	return resp.Credits, nil
}

// LicenseCredits returns the number of license credits available to the application.
func (c *Client) LicenseCredits(ctx context.Context) (int, error) {
	var resp struct {
		Credits int `json:"credits"`
	}
	r := apiRequest{
		method: http.MethodGet,
		path:   "licenses.json",
	}
	_, err := c.api.do(ctx, r, &resp)
	if err != nil {
		return 0, err
	}
	return resp.Credits, nil
}
