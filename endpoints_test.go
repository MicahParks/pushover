package pushover

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestReceipt(t *testing.T) {
	const receipt = "rLqVuqTRh62UzxtmqiaLzQmVcPgiCy"
	c, f := newFakeAPI(t, http.StatusOK, `{"status":1,"acknowledged":1,"acknowledged_at":1360019238,"acknowledged_by":"`+testUser+`","acknowledged_by_device":"iphone","last_delivered_at":1360001238,"expired":1,"expires_at":1360019290,"called_back":0,"called_back_at":0,"request":"r"}`)
	got, err := c.Receipt(t.Context(), receipt)
	if err != nil {
		t.Fatalf("Failed to get receipt.\nError: %s", err)
	}
	req := f.last(t)
	assertRequest(t, req, http.MethodGet, "/1/receipts/"+receipt+".json")
	assertParams(t, req.Params, nil)
	want := Receipt{
		Acknowledged:         true,
		AcknowledgedAt:       time.Unix(1360019238, 0),
		AcknowledgedBy:       testUser,
		AcknowledgedByDevice: "iphone",
		LastDeliveredAt:      time.Unix(1360001238, 0),
		Expired:              true,
		ExpiresAt:            time.Unix(1360019290, 0),
	}
	if got != want {
		t.Errorf("Unexpected receipt.\nExpected: %+v\nGot:      %+v", want, got)
	}
	if !got.CalledBackAt.IsZero() {
		t.Error("Expected a 0 timestamp to become the zero time.")
	}
}

func TestCancelReceipt(t *testing.T) {
	c, f := newFakeAPI(t, http.StatusOK, `{"status":1,"request":"r"}`)
	err := c.CancelReceipt(t.Context(), "rLqVuqTRh62UzxtmqiaLzQmVcPgiCy")
	if err != nil {
		t.Fatalf("Failed to cancel receipt.\nError: %s", err)
	}
	req := f.last(t)
	assertRequest(t, req, http.MethodPost, "/1/receipts/rLqVuqTRh62UzxtmqiaLzQmVcPgiCy/cancel.json")
	assertParams(t, req.Params, nil)
}

func TestCancelByTag(t *testing.T) {
	c, f := newFakeAPI(t, http.StatusOK, `{"status":1,"request":"r"}`)
	err := c.CancelByTag(t.Context(), "l=chicago/1 2")
	if err != nil {
		t.Fatalf("Failed to cancel by tag.\nError: %s", err)
	}
	req := f.last(t)
	assertRequest(t, req, http.MethodPost, "/1/receipts/cancel_by_tag/l=chicago%2F1%202.json")
	assertParams(t, req.Params, nil)
}

func TestEmptyPathParametersRejected(t *testing.T) {
	c, f := newFakeAPI(t, http.StatusOK, `{"status":1,"request":"r"}`)
	ctx := t.Context()
	errs := map[string]error{
		"Receipt":          func() error { _, err := c.Receipt(ctx, ""); return err }(),
		"CancelReceipt":    c.CancelReceipt(ctx, ""),
		"CancelByTag":      c.CancelByTag(ctx, ""),
		"Group":            func() error { _, err := c.Group(ctx, ""); return err }(),
		"AddGroupUser":     c.AddGroupUser(ctx, "", GroupUser{User: testUser}),
		"AddGroupUserUser": c.AddGroupUser(ctx, testGroup, GroupUser{}),
		"RemoveGroupUser":  c.RemoveGroupUser(ctx, testGroup, GroupUser{}),
		"RenameGroup":      c.RenameGroup(ctx, "", "name"),
		"RenameGroupName":  c.RenameGroup(ctx, testGroup, ""),
		"CreateGroup":      func() error { _, err := c.CreateGroup(ctx, ""); return err }(),
		"ValidateUser":     func() error { _, err := c.ValidateUser(ctx, "", ""); return err }(),
	}
	for name, err := range errs {
		if !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("%s: expected ErrInvalidRequest, got %v.", name, err)
		}
	}
	if f.count() != 0 {
		t.Errorf("Expected no requests, got %d.", f.count())
	}
}

func TestSounds(t *testing.T) {
	c, f := newFakeAPI(t, http.StatusOK, `{"sounds":{"pushover":"Pushover (default)","bike":"Bike","mycustom":"My Custom"},"status":1,"request":"r"}`)
	sounds, err := c.Sounds(t.Context())
	if err != nil {
		t.Fatalf("Failed to get sounds.\nError: %s", err)
	}
	assertRequest(t, f.last(t), http.MethodGet, "/1/sounds.json")
	if len(sounds) != 3 || sounds[SoundBike] != "Bike" || sounds["mycustom"] != "My Custom" {
		t.Errorf("Unexpected sounds %v.", sounds)
	}
}

func TestLimits(t *testing.T) {
	c, f := newFakeAPI(t, http.StatusOK, `{"limit":10000,"remaining":7496,"reset":1393653600,"status":1,"request":"r"}`)
	limits, err := c.Limits(t.Context())
	if err != nil {
		t.Fatalf("Failed to get limits.\nError: %s", err)
	}
	assertRequest(t, f.last(t), http.MethodGet, "/1/apps/limits.json")
	want := Limits{Limit: 10000, Remaining: 7496, Reset: time.Unix(1393653600, 0)}
	if limits != want {
		t.Errorf("Expected %+v, got %+v.", want, limits)
	}
}

func TestParseLimitHeadersMalformed(t *testing.T) {
	for _, h := range []http.Header{
		{},
		{"X-Limit-App-Limit": {"10000"}},
		{"X-Limit-App-Limit": {"x"}, "X-Limit-App-Remaining": {"1"}, "X-Limit-App-Reset": {"1"}},
		{"X-Limit-App-Limit": {"1"}, "X-Limit-App-Remaining": {"x"}, "X-Limit-App-Reset": {"1"}},
		{"X-Limit-App-Limit": {"1"}, "X-Limit-App-Remaining": {"1"}, "X-Limit-App-Reset": {"x"}},
	} {
		if l := parseLimitHeaders(h); l != nil {
			t.Errorf("Expected nil limits for %v, got %+v.", h, *l)
		}
	}
}

func TestValidateUser(t *testing.T) {
	c, f := newFakeAPI(t, http.StatusOK, `{"status":1,"group":0,"devices":["iphone","pixel"],"licenses":["iOS","Android"],"request":"r"}`)
	v, err := c.ValidateUser(t.Context(), testUser, "iphone")
	if err != nil {
		t.Fatalf("Failed to validate user.\nError: %s", err)
	}
	req := f.last(t)
	assertRequest(t, req, http.MethodPost, "/1/users/validate.json")
	assertParams(t, req.Params, map[string]string{"user": testUser, "device": "iphone"})
	if v.Group || strings.Join(v.Devices, ",") != "iphone,pixel" || strings.Join(v.Licenses, ",") != "iOS,Android" {
		t.Errorf("Unexpected validation %+v.", v)
	}

	f.setBody(`{"status":1,"group":1,"devices":[],"licenses":[],"request":"r"}`)
	v, err = c.ValidateUser(t.Context(), testGroup, "")
	if err != nil {
		t.Fatalf("Failed to validate group.\nError: %s", err)
	}
	assertParams(t, f.last(t).Params, map[string]string{"user": testGroup})
	if !v.Group {
		t.Error("Expected a group.")
	}
}

func TestGroups(t *testing.T) {
	c, f := newFakeAPI(t, http.StatusOK, `{"status":1,"request":"r","group":"`+testGroup+`"}`)
	ctx := t.Context()

	key, err := c.CreateGroup(ctx, "On call")
	if err != nil {
		t.Fatalf("Failed to create group.\nError: %s", err)
	}
	req := f.last(t)
	assertRequest(t, req, http.MethodPost, "/1/groups.json")
	assertParams(t, req.Params, map[string]string{"name": "On call"})
	if key != testGroup {
		t.Errorf("Unexpected group key %q.", key)
	}

	f.setBody(`{"status":1,"request":"r","groups":[{"group":"` + testGroup + `","name":"my group"}]}`)
	groups, err := c.Groups(ctx)
	if err != nil {
		t.Fatalf("Failed to list groups.\nError: %s", err)
	}
	assertRequest(t, f.last(t), http.MethodGet, "/1/groups.json")
	if len(groups) != 1 || groups[0] != (GroupSummary{Key: testGroup, Name: "my group"}) {
		t.Errorf("Unexpected groups %+v.", groups)
	}

	f.setBody(`{"status":1,"request":"r","name":"My Delivery Group","users":[{"user":"uoJCttEFQo8uoZ6REZHMGBjX2pmcdJ","device":null,"memo":"Jim","disabled":false},{"user":"uZgLtMeFSRgdxLbpWbDDPM9v4wmEKu","device":"iphone","memo":"Mary","disabled":true,"name":"Mary Smith","email":"mary@example.com"}]}`)
	group, err := c.Group(ctx, testGroup)
	if err != nil {
		t.Fatalf("Failed to get group.\nError: %s", err)
	}
	assertRequest(t, f.last(t), http.MethodGet, "/1/groups/"+testGroup+".json")
	want := Group{
		Name: "My Delivery Group",
		Members: []GroupMember{
			{User: "uoJCttEFQo8uoZ6REZHMGBjX2pmcdJ", Memo: "Jim"},
			{User: "uZgLtMeFSRgdxLbpWbDDPM9v4wmEKu", Device: "iphone", Memo: "Mary", Disabled: true, Name: "Mary Smith", Email: "mary@example.com"},
		},
	}
	if group.Name != want.Name || len(group.Members) != 2 || group.Members[0] != want.Members[0] || group.Members[1] != want.Members[1] {
		t.Errorf("Unexpected group.\nExpected: %+v\nGot:      %+v", want, group)
	}

	f.setBody(`{"status":1,"request":"r"}`)
	err = c.AddGroupUser(ctx, testGroup, GroupUser{User: testUser, Device: "iphone", Memo: "Jim"})
	if err != nil {
		t.Fatalf("Failed to add group user.\nError: %s", err)
	}
	req = f.last(t)
	assertRequest(t, req, http.MethodPost, "/1/groups/"+testGroup+"/add_user.json")
	assertParams(t, req.Params, map[string]string{"user": testUser, "device": "iphone", "memo": "Jim"})

	err = c.AddGroupUser(ctx, testGroup, GroupUser{User: testUser, Memo: strings.Repeat("a", MaxGroupMemoLength+1)})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("Expected ErrInvalidRequest for a long memo, got %v.", err)
	}

	actions := map[string]func(ctx context.Context, groupKey string, user GroupUser) error{
		"remove_user":  c.RemoveGroupUser,
		"disable_user": c.DisableGroupUser,
		"enable_user":  c.EnableGroupUser,
	}
	for action, fn := range actions {
		err = fn(ctx, testGroup, GroupUser{User: testUser})
		if err != nil {
			t.Fatalf("Failed to %s.\nError: %s", action, err)
		}
		req = f.last(t)
		assertRequest(t, req, http.MethodPost, "/1/groups/"+testGroup+"/"+action+".json")
		assertParams(t, req.Params, map[string]string{"user": testUser})

		err = fn(ctx, testGroup, GroupUser{User: testUser, Device: "pixel", Memo: "ignored"})
		if err != nil {
			t.Fatalf("Failed to %s with device.\nError: %s", action, err)
		}
		assertParams(t, f.last(t).Params, map[string]string{"user": testUser, "device": "pixel"})
	}

	err = c.RenameGroup(ctx, testGroup, "New name")
	if err != nil {
		t.Fatalf("Failed to rename group.\nError: %s", err)
	}
	req = f.last(t)
	assertRequest(t, req, http.MethodPost, "/1/groups/"+testGroup+"/rename.json")
	assertParams(t, req.Params, map[string]string{"name": "New name"})
}

func TestUpdateGlance(t *testing.T) {
	c, f := newFakeAPI(t, http.StatusOK, `{"status":1,"request":"r"}`)
	err := c.UpdateGlance(t.Context(), Glance{
		User:    testUser,
		Device:  "watch",
		Title:   ptr("Widgets Sold"),
		Text:    ptr("1,024"),
		Subtext: ptr(""),
		Count:   ptr(-5),
		Percent: ptr(100),
	})
	if err != nil {
		t.Fatalf("Failed to update glance.\nError: %s", err)
	}
	req := f.last(t)
	assertRequest(t, req, http.MethodPost, "/1/glances.json")
	assertParams(t, req.Params, map[string]string{
		"user":    testUser,
		"device":  "watch",
		"title":   "Widgets Sold",
		"text":    "1,024",
		"subtext": "",
		"count":   "-5",
		"percent": "100",
	})
	if _, ok := req.Params["subtext"]; !ok {
		t.Error("Expected an empty subtext to be sent to clear it.")
	}

	invalid := []Glance{
		{Title: ptr("x")},
		{User: testUser},
		{User: testUser, Percent: ptr(101)},
		{User: testUser, Percent: ptr(-1)},
		{User: testUser, Text: ptr(strings.Repeat("a", MaxGlanceTextLength+1))},
		{User: testUser, Count: ptr(1), Device: "bad device"},
	}
	for _, g := range invalid {
		err = c.UpdateGlance(t.Context(), g)
		if !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("Expected ErrInvalidRequest for %+v, got %v.", g, err)
		}
	}
}

func TestLicensing(t *testing.T) {
	c, f := newFakeAPI(t, http.StatusOK, `{"status":1,"credits":5,"request":"r"}`)
	credits, err := c.AssignLicense(t.Context(), LicenseAssignment{User: testUser, OS: LicenseOSiOS})
	if err != nil {
		t.Fatalf("Failed to assign license.\nError: %s", err)
	}
	req := f.last(t)
	assertRequest(t, req, http.MethodPost, "/1/licenses/assign.json")
	assertParams(t, req.Params, map[string]string{"user": testUser, "os": "iOS"})
	if credits != 5 {
		t.Errorf("Expected 5 credits, got %d.", credits)
	}

	_, err = c.AssignLicense(t.Context(), LicenseAssignment{Email: "user@example.com"})
	if err != nil {
		t.Fatalf("Failed to assign license by email.\nError: %s", err)
	}
	assertParams(t, f.last(t).Params, map[string]string{"email": "user@example.com"})

	credits, err = c.LicenseCredits(t.Context())
	if err != nil {
		t.Fatalf("Failed to get license credits.\nError: %s", err)
	}
	assertRequest(t, f.last(t), http.MethodGet, "/1/licenses.json")
	if credits != 5 {
		t.Errorf("Expected 5 credits, got %d.", credits)
	}

	_, err = c.AssignLicense(t.Context(), LicenseAssignment{User: testUser, OS: "FuturePlatform"})
	if err != nil {
		t.Fatalf("Expected unknown platforms to be passed through.\nError: %s", err)
	}
	assertParams(t, f.last(t).Params, map[string]string{"user": testUser, "os": "FuturePlatform"})

	for _, a := range []LicenseAssignment{
		{},
		{User: testUser, Email: "user@example.com"},
	} {
		_, err = c.AssignLicense(t.Context(), a)
		if !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("Expected ErrInvalidRequest for %+v, got %v.", a, err)
		}
	}
}

func TestMigrateSubscription(t *testing.T) {
	c, f := newFakeAPI(t, http.StatusOK, `{"status":1,"subscribed_user_key":"sPfjsD2fGzEd9TR52DU31Hv4A61Vvk","request":"r"}`)
	key, err := c.MigrateSubscription(t.Context(), SubscriptionMigration{
		Subscription: "Forum-f504h08fhlasdfj",
		User:         testUser,
		DeviceName:   "iphone",
		Sound:        SoundCosmic,
	})
	if err != nil {
		t.Fatalf("Failed to migrate subscription.\nError: %s", err)
	}
	req := f.last(t)
	assertRequest(t, req, http.MethodPost, "/1/subscriptions/migrate.json")
	assertParams(t, req.Params, map[string]string{
		"subscription": "Forum-f504h08fhlasdfj",
		"user":         testUser,
		"device_name":  "iphone",
		"sound":        "cosmic",
	})
	if key != "sPfjsD2fGzEd9TR52DU31Hv4A61Vvk" {
		t.Errorf("Unexpected subscribed user key %q.", key)
	}

	for _, m := range []SubscriptionMigration{{User: testUser}, {Subscription: "s"}} {
		_, err = c.MigrateSubscription(t.Context(), m)
		if !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("Expected ErrInvalidRequest for %+v, got %v.", m, err)
		}
	}
}

func TestSubscribeURL(t *testing.T) {
	got, err := SubscribeURL(
		"https://pushover.net/subscribe/Forum-f504h08fhlasdfj",
		"https://forum.example.com/settings/enable_pushover?rand=m5St1Fe67ly5d6",
		"https://forum.example.com/settings",
	)
	if err != nil {
		t.Fatalf("Failed to build subscribe URL.\nError: %s", err)
	}
	want := "https://pushover.net/subscribe/Forum-f504h08fhlasdfj?failure=https%3A%2F%2Fforum.example.com%2Fsettings&success=https%3A%2F%2Fforum.example.com%2Fsettings%2Fenable_pushover%3Frand%3Dm5St1Fe67ly5d6"
	if got != want {
		t.Errorf("Unexpected subscribe URL.\nExpected: %s\nGot:      %s", want, got)
	}

	got, err = SubscribeURL("https://pushover.net/subscribe/Forum-f504h08fhlasdfj", "https://forum.example.com/ok", "")
	if err != nil {
		t.Fatalf("Failed to build subscribe URL.\nError: %s", err)
	}
	if strings.Contains(got, "failure=") {
		t.Errorf("Expected no failure parameter, got %s.", got)
	}

	for _, args := range [][2]string{{"", "https://a"}, {"/subscribe/x", "https://a"}, {"https://pushover.net/subscribe/x", ""}} {
		_, err = SubscribeURL(args[0], args[1], "")
		if !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("Expected ErrInvalidRequest for %q, got %v.", args, err)
		}
	}
}

func TestParseSubscriptionRedirect(t *testing.T) {
	u, _ := url.Parse("https://forum.example.com/settings/enable_pushover?rand=m5St1Fe67ly5d6&pushover_user_key=sPfjsD2fGzEd9TR52DU31Hv4A61Vvk")
	got := ParseSubscriptionRedirect(u.Query())
	if got != (SubscriptionRedirect{UserKey: "sPfjsD2fGzEd9TR52DU31Hv4A61Vvk"}) {
		t.Errorf("Unexpected subscription redirect %+v.", got)
	}

	u, _ = url.Parse("https://forum.example.com/settings/enable_pushover?pushover_user_key=&pushover_unsubscribed=1&pushover_unsubscribed_user_key=sPfjsD2fGzEd9TR52DU31Hv4A61Vvk")
	got = ParseSubscriptionRedirect(u.Query())
	if got != (SubscriptionRedirect{Unsubscribed: true, UnsubscribedUserKey: "sPfjsD2fGzEd9TR52DU31Hv4A61Vvk"}) {
		t.Errorf("Unexpected unsubscribe redirect %+v.", got)
	}
}

func TestTeam(t *testing.T) {
	c, f := newFakeAPI(t, http.StatusOK, `{"status":1,"request":"r","name":"my team","users":[{"id":"da901fb83e6ce7a46a3f7a6de95cef","name":"my team user","email":"user@example.com","administrator":true,"devices":[{"name":"iphone","os":"iOS","os_version":"26","enabled":true,"administratively_disabled":false}]}]}`)
	team, err := NewTeamClient(testToken, Options{BaseURL: c.api.baseURL})
	if err != nil {
		t.Fatalf("Failed to create team client.\nError: %s", err)
	}
	ctx := t.Context()

	info, err := team.Team(ctx)
	if err != nil {
		t.Fatalf("Failed to get team.\nError: %s", err)
	}
	assertRequest(t, f.last(t), http.MethodGet, "/1/teams.json")
	if info.Name != "my team" || len(info.Users) != 1 {
		t.Fatalf("Unexpected team %+v.", info)
	}
	user := info.Users[0]
	if user.ID != "da901fb83e6ce7a46a3f7a6de95cef" || user.Name != "my team user" || user.Email != "user@example.com" || !user.Administrator {
		t.Errorf("Unexpected team user %+v.", user)
	}
	wantDevice := TeamDevice{Name: "iphone", OS: "iOS", OSVersion: "26", Enabled: true}
	if len(user.Devices) != 1 || user.Devices[0] != wantDevice {
		t.Errorf("Unexpected team devices %+v.", user.Devices)
	}

	f.setBody(`{"status":1,"request":"r"}`)
	err = team.AddUser(ctx, TeamInvitation{
		Email:         "new@example.com",
		Name:          "New User",
		Password:      "correct horse battery staple",
		InstantLogin:  true,
		Administrator: true,
		Group:         "Ops",
	})
	if err != nil {
		t.Fatalf("Failed to add team user.\nError: %s", err)
	}
	req := f.last(t)
	assertRequest(t, req, http.MethodPost, "/1/teams/add_user.json")
	assertParams(t, req.Params, map[string]string{
		"email":    "new@example.com",
		"name":     "New User",
		"password": "correct horse battery staple",
		"instant":  "true",
		"admin":    "true",
		"group":    "Ops",
	})

	err = team.AddUser(ctx, TeamInvitation{Email: "minimal@example.com"})
	if err != nil {
		t.Fatalf("Failed to add team user.\nError: %s", err)
	}
	assertParams(t, f.last(t).Params, map[string]string{"email": "minimal@example.com"})

	err = team.RevokeInvitation(ctx, "new@example.com")
	if err != nil {
		t.Fatalf("Failed to revoke invitation.\nError: %s", err)
	}
	req = f.last(t)
	assertRequest(t, req, http.MethodPost, "/1/teams/revoke_invitation.json")
	assertParams(t, req.Params, map[string]string{"email": "new@example.com"})

	err = team.RemoveUser(ctx, "new@example.com")
	if err != nil {
		t.Fatalf("Failed to remove user.\nError: %s", err)
	}
	req = f.last(t)
	assertRequest(t, req, http.MethodPost, "/1/teams/remove_user.json")
	assertParams(t, req.Params, map[string]string{"email": "new@example.com"})

	count := f.count()
	for name, err := range map[string]error{
		"AddUser":          team.AddUser(ctx, TeamInvitation{}),
		"RevokeInvitation": team.RevokeInvitation(ctx, ""),
		"RemoveUser":       team.RemoveUser(ctx, ""),
	} {
		if !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("%s: expected ErrInvalidRequest, got %v.", name, err)
		}
	}
	if f.count() != count {
		t.Error("Expected no requests for invalid input.")
	}
}
