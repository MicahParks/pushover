package pushover

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"unicode/utf8"
)

// MaxGroupMemoLength is the maximum length of a delivery group member's memo.
const MaxGroupMemoLength = 200

// GroupSummary identifies a delivery group.
type GroupSummary struct {
	// Key is the group key. Use it in place of a user key to send a message to every member of the group.
	Key string
	// Name is the group's name.
	Name string
}

// Group is a delivery group and its members.
type Group struct {
	// Name is the group's name.
	Name string
	// Members are the group's members.
	Members []GroupMember
}

// GroupMember is a member of a delivery group.
type GroupMember struct {
	// User is the member's user key.
	User string
	// Device restricts messages to one of the user's devices. If empty, messages go to all of the user's devices.
	Device string
	// Memo is free text associated with the member, such as their name.
	Memo string
	// Disabled reports whether messages to the member are temporarily disabled.
	Disabled bool
	// Name is the member's name. Only set for Team members of Team-owned groups.
	Name string
	// Email is the member's email address. Only set for Team members of Team-owned groups.
	Email string
}

// GroupUser identifies a user's membership in a delivery group.
type GroupUser struct {
	// User is the user's user key. Required.
	User string
	// Device is the name of one of the user's devices. When adding a user, it restricts messages to that device, and
	// if empty, messages go to all of the user's devices. When removing, disabling, or enabling a user, it selects the
	// membership for that device, and if empty, all of the user's memberships in the group are affected.
	Device string
	// Memo is free text associated with the member, such as their name. At most MaxGroupMemoLength characters. It's
	// only used by Client.AddGroupUser.
	Memo string
}

// CreateGroup creates a delivery group and returns its key.
//
// The Groups API requires an application token that belongs to the same account as the group. See
// https://pushover.net/api/groups
func (c *Client) CreateGroup(ctx context.Context, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("%w: group name is required", ErrInvalidRequest)
	}
	params := url.Values{}
	params.Set("name", name)
	var resp struct {
		Group string `json:"group"`
	}
	r := apiRequest{
		method: http.MethodPost,
		path:   "groups.json",
		params: params,
	}
	_, err := c.api.do(ctx, r, &resp)
	if err != nil {
		return "", err
	}
	return resp.Group, nil
}

// Groups lists the delivery groups of the account that owns the application.
func (c *Client) Groups(ctx context.Context) ([]GroupSummary, error) {
	var resp struct {
		Groups []struct {
			Group string `json:"group"`
			Name  string `json:"name"`
		} `json:"groups"`
	}
	r := apiRequest{
		method: http.MethodGet,
		path:   "groups.json",
	}
	_, err := c.api.do(ctx, r, &resp)
	if err != nil {
		return nil, err
	}
	groups := make([]GroupSummary, 0, len(resp.Groups))
	for _, g := range resp.Groups {
		groups = append(groups, GroupSummary{Key: g.Group, Name: g.Name})
	}
	return groups, nil
}

// Group gets a delivery group's name and members.
func (c *Client) Group(ctx context.Context, groupKey string) (Group, error) {
	if groupKey == "" {
		return Group{}, fmt.Errorf("%w: group key is required", ErrInvalidRequest)
	}
	var resp struct {
		Name  string `json:"name"`
		Users []struct {
			User     string   `json:"user"`
			Device   *string  `json:"device"`
			Memo     string   `json:"memo"`
			Disabled flexBool `json:"disabled"`
			Name     string   `json:"name"`
			Email    string   `json:"email"`
		} `json:"users"`
	}
	r := apiRequest{
		method: http.MethodGet,
		path:   groupPath(groupKey, ""),
	}
	_, err := c.api.do(ctx, r, &resp)
	if err != nil {
		return Group{}, err
	}
	g := Group{
		Name:    resp.Name,
		Members: make([]GroupMember, 0, len(resp.Users)),
	}
	for _, u := range resp.Users {
		m := GroupMember{
			User:     u.User,
			Memo:     u.Memo,
			Disabled: bool(u.Disabled),
			Name:     u.Name,
			Email:    u.Email,
		}
		if u.Device != nil {
			m.Device = *u.Device
		}
		g.Members = append(g.Members, m)
	}
	return g, nil
}

// AddGroupUser adds a user to a delivery group.
func (c *Client) AddGroupUser(ctx context.Context, groupKey string, user GroupUser) error {
	if n := utf8.RuneCountInString(user.Memo); n > MaxGroupMemoLength {
		return fmt.Errorf("%w: memo is %d characters, the maximum is %d", ErrInvalidRequest, n, MaxGroupMemoLength)
	}
	params := url.Values{}
	if user.Memo != "" {
		params.Set("memo", user.Memo)
	}
	return c.groupUserAction(ctx, groupKey, "add_user", user, params, retryNotIdempotent)
}

// RemoveGroupUser removes a user from a delivery group. If user.Device is empty, all of the user's memberships in the
// group are removed. Otherwise, only the membership for that device is removed.
func (c *Client) RemoveGroupUser(ctx context.Context, groupKey string, user GroupUser) error {
	return c.groupUserAction(ctx, groupKey, "remove_user", user, nil, retryNotIdempotent)
}

// DisableGroupUser temporarily stops sending messages to a member of a delivery group. If user.Device is empty, all
// of the user's memberships in the group are disabled. Otherwise, only the membership for that device is disabled.
func (c *Client) DisableGroupUser(ctx context.Context, groupKey string, user GroupUser) error {
	return c.groupUserAction(ctx, groupKey, "disable_user", user, nil, retryIdempotent)
}

// EnableGroupUser resumes sending messages to a member of a delivery group that was disabled. If user.Device is empty,
// all of the user's memberships in the group are enabled. Otherwise, only the membership for that device is enabled.
func (c *Client) EnableGroupUser(ctx context.Context, groupKey string, user GroupUser) error {
	return c.groupUserAction(ctx, groupKey, "enable_user", user, nil, retryIdempotent)
}

// RenameGroup changes the name of a delivery group. Group names are unique within an account.
func (c *Client) RenameGroup(ctx context.Context, groupKey, name string) error {
	if groupKey == "" {
		return fmt.Errorf("%w: group key is required", ErrInvalidRequest)
	}
	if name == "" {
		return fmt.Errorf("%w: group name is required", ErrInvalidRequest)
	}
	params := url.Values{}
	params.Set("name", name)
	r := apiRequest{
		method: http.MethodPost,
		path:   groupPath(groupKey, "rename"),
		params: params,
		retry:  retryIdempotent,
	}
	_, err := c.api.do(ctx, r, nil)
	return err
}

func (c *Client) groupUserAction(ctx context.Context, groupKey, action string, user GroupUser, params url.Values, retry retryPolicy) error {
	if groupKey == "" {
		return fmt.Errorf("%w: group key is required", ErrInvalidRequest)
	}
	if user.User == "" {
		return fmt.Errorf("%w: user is required", ErrInvalidRequest)
	}
	if user.Device != "" {
		err := validateDevice(user.Device)
		if err != nil {
			return err
		}
	}
	if params == nil {
		params = url.Values{}
	}
	params.Set("user", user.User)
	if user.Device != "" {
		params.Set("device", user.Device)
	}
	r := apiRequest{
		method: http.MethodPost,
		path:   groupPath(groupKey, action),
		params: params,
		retry:  retry,
	}
	_, err := c.api.do(ctx, r, nil)
	return err
}

func groupPath(groupKey, action string) string {
	p := "groups/" + url.PathEscape(groupKey)
	if action != "" {
		p += "/" + action
	}
	return p + ".json"
}
