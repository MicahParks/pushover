package pushover

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// TeamClient is a client for the Pushover for Teams API, which authenticates with a Team API token rather than an
// application API token. It is safe for concurrent use. See https://pushover.net/api/teams
type TeamClient struct {
	api *api
}

// NewTeamClient creates a new TeamClient with the given Team API token.
func NewTeamClient(teamToken string, options Options) (*TeamClient, error) {
	a, err := newAPI(teamToken, options)
	if err != nil {
		return nil, err
	}
	return &TeamClient{api: a}, nil
}

// Team describes a Pushover for Teams team.
type Team struct {
	// Name is the team's name.
	Name string
	// Users are the team's members.
	Users []TeamUser
}

// TeamUser is a member of a team.
type TeamUser struct {
	// ID identifies the user within Pushover.
	ID string
	// Name is the user's name.
	Name string
	// Email is the user's email address.
	Email string
	// Administrator reports whether the user is a team administrator.
	Administrator bool
	// Devices are the user's devices.
	Devices []TeamDevice
}

// TeamDevice is a device of a team member.
type TeamDevice struct {
	// Name is the device's name.
	Name string
	// OS is the device's operating system, such as "iOS".
	OS string
	// OSVersion is the version of the device's operating system.
	OSVersion string
	// Enabled reports whether the device is enabled.
	Enabled bool
	// AdministrativelyDisabled reports whether a team administrator disabled the device.
	AdministrativelyDisabled bool
}

// TeamInvitation describes a user to add to a team.
type TeamInvitation struct {
	// Email is the user's email address. Required.
	Email string
	// Name is the user's full name.
	Name string
	// Password is the user's initial password. If empty, a random password is generated and emailed to the user in
	// cleartext.
	Password string
	// InstantLogin includes an Instant Login link, valid for 7 days, in the welcome email.
	InstantLogin bool
	// Administrator adds the user as a team administrator.
	Administrator bool
	// Group is the name of the delivery group to add the user to. Defaults to the auto-updating Team delivery group.
	// The group is created if it doesn't exist.
	Group string
}

// Team gets the team's name and members.
func (t *TeamClient) Team(ctx context.Context) (Team, error) {
	var resp struct {
		Name  string `json:"name"`
		Users []struct {
			ID            string   `json:"id"`
			Name          string   `json:"name"`
			Email         string   `json:"email"`
			Administrator flexBool `json:"administrator"`
			Devices       []struct {
				Name                     string   `json:"name"`
				OS                       string   `json:"os"`
				OSVersion                string   `json:"os_version"`
				Enabled                  flexBool `json:"enabled"`
				AdministrativelyDisabled flexBool `json:"administratively_disabled"`
			} `json:"devices"`
		} `json:"users"`
	}
	r := apiRequest{
		method: http.MethodGet,
		path:   "teams.json",
	}
	_, err := t.api.do(ctx, r, &resp)
	if err != nil {
		return Team{}, err
	}
	team := Team{
		Name:  resp.Name,
		Users: make([]TeamUser, 0, len(resp.Users)),
	}
	for _, u := range resp.Users {
		user := TeamUser{
			ID:            u.ID,
			Name:          u.Name,
			Email:         u.Email,
			Administrator: bool(u.Administrator),
			Devices:       make([]TeamDevice, 0, len(u.Devices)),
		}
		for _, d := range u.Devices {
			user.Devices = append(user.Devices, TeamDevice{
				Name:                     d.Name,
				OS:                       d.OS,
				OSVersion:                d.OSVersion,
				Enabled:                  bool(d.Enabled),
				AdministrativelyDisabled: bool(d.AdministrativelyDisabled),
			})
		}
		team.Users = append(team.Users, user)
	}
	return team, nil
}

// AddUser adds a user to the team, creating their Pushover account if needed, and emails them an invitation.
//
// This request sends an email, so it's never retried automatically, regardless of Options.RetryMax.
func (t *TeamClient) AddUser(ctx context.Context, invitation TeamInvitation) error {
	if invitation.Email == "" {
		return fmt.Errorf("%w: email is required", ErrInvalidRequest)
	}
	params := url.Values{}
	params.Set("email", invitation.Email)
	if invitation.Name != "" {
		params.Set("name", invitation.Name)
	}
	if invitation.Password != "" {
		params.Set("password", invitation.Password)
	}
	if invitation.InstantLogin {
		params.Set("instant", "true")
	}
	if invitation.Administrator {
		params.Set("admin", "true")
	}
	if invitation.Group != "" {
		params.Set("group", invitation.Group)
	}
	r := apiRequest{
		method: http.MethodPost,
		path:   "teams/add_user.json",
		params: params,
		retry:  retryNever,
	}
	_, err := t.api.do(ctx, r, nil)
	return err
}

// RevokeInvitation revokes a pending invitation for a user that has not accepted it yet.
func (t *TeamClient) RevokeInvitation(ctx context.Context, email string) error {
	return t.emailAction(ctx, "teams/revoke_invitation.json", email)
}

// RemoveUser removes a user from the team and its delivery groups. The user's Pushover account is not deleted. At
// least one administrator must remain on the team.
func (t *TeamClient) RemoveUser(ctx context.Context, email string) error {
	return t.emailAction(ctx, "teams/remove_user.json", email)
}

func (t *TeamClient) emailAction(ctx context.Context, path, email string) error {
	if email == "" {
		return fmt.Errorf("%w: email is required", ErrInvalidRequest)
	}
	params := url.Values{}
	params.Set("email", email)
	r := apiRequest{
		method: http.MethodPost,
		path:   path,
		params: params,
	}
	_, err := t.api.do(ctx, r, nil)
	return err
}
