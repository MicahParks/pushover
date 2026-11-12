package pushover

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"testing"
	"time"
)

// Live tests run against the real Pushover API when PUSHOVER_TOKEN (an application API token) and PUSHOVER_USER (a
// user key) are set. Every message is sent with PriorityLowest, so it doesn't generate a notification, and a short TTL,
// so it's deleted from devices automatically. Only read-only calls are made to other APIs.
//
//	PUSHOVER_TOKEN=... PUSHOVER_USER=... go test -run Live -v
func liveClient(t *testing.T) (*Client, string) {
	t.Helper()
	token, user := os.Getenv("PUSHOVER_TOKEN"), os.Getenv("PUSHOVER_USER")
	if token == "" || user == "" {
		t.Skip("Set PUSHOVER_TOKEN and PUSHOVER_USER to run live tests against the Pushover API.")
	}
	if testing.Short() {
		t.Skip("Skipping live test in short mode.")
	}
	c, err := New(token, Options{})
	if err != nil {
		t.Fatalf("Failed to create client.\nError: %s", err)
	}
	return c, user
}

func liveMessage(user, text string) Message {
	return Message{
		User:     user,
		Message:  text,
		Title:    "github.com/MicahParks/pushover live test",
		Priority: PriorityLowest,
		TTL:      time.Hour,
	}
}

func TestLiveValidateUser(t *testing.T) {
	c, user := liveClient(t)
	v, err := c.ValidateUser(t.Context(), user, "")
	if err != nil {
		t.Fatalf("Failed to validate user.\nError: %s", err)
	}
	if v.Group || len(v.Devices) == 0 {
		t.Errorf("Expected a user with at least one device, got %+v.", v)
	}
	t.Logf("Devices: %v, licenses: %v.", v.Devices, v.Licenses)

	_, err = c.ValidateUser(t.Context(), "uQiRzpo4DXghDmr9QzzfQu27cmVRsG", "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Expected an *APIError for an invalid user key, got %v.", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest || apiErr.Request == "" || len(apiErr.Errors) == 0 || apiErr.Temporary() {
		t.Errorf("Unexpected API error for an invalid user key: %#v.", apiErr)
	}
	t.Logf("Invalid user error: %s", err)
}

func TestLiveSoundsAndLimits(t *testing.T) {
	c, _ := liveClient(t)
	sounds, err := c.Sounds(t.Context())
	if err != nil {
		t.Fatalf("Failed to get sounds.\nError: %s", err)
	}
	builtIn := []Sound{
		SoundPushover, SoundBike, SoundBugle, SoundCashRegister, SoundClassical, SoundCosmic, SoundFalling,
		SoundGamelan, SoundIncoming, SoundIntermission, SoundMagic, SoundMechanical, SoundPianoBar, SoundSiren,
		SoundSpaceAlarm, SoundTugboat, SoundAlien, SoundClimb, SoundPersistent, SoundEcho, SoundUpDown,
		SoundVibrate, SoundNone,
	}
	for _, s := range builtIn {
		if _, ok := sounds[s]; !ok {
			t.Errorf("Built-in sound %q is missing from the API response.", s)
		}
	}

	limits, err := c.Limits(t.Context())
	if err != nil {
		t.Fatalf("Failed to get limits.\nError: %s", err)
	}
	if limits.Limit <= 0 || limits.Reset.Before(time.Now()) {
		t.Errorf("Unexpected limits %+v.", limits)
	}
	t.Logf("Limits: %+v.", limits)
}

func TestLiveSend(t *testing.T) {
	c, user := liveClient(t)
	msg := liveMessage(user, "Plain <b>HTML</b> message with a supplementary URL.")
	msg.HTML = true
	msg.Sound = SoundNone
	msg.URL = "https://github.com/MicahParks/pushover"
	msg.URLTitle = "Source"
	msg.Timestamp = time.Now().Add(-time.Minute)
	resp, err := c.Send(t.Context(), msg)
	if err != nil {
		t.Fatalf("Failed to send message.\nError: %s", err)
	}
	if resp.Request == "" || resp.Receipt != "" {
		t.Errorf("Unexpected response %+v.", resp)
	}
	if resp.Limits == nil || resp.Limits.Limit <= 0 {
		t.Errorf("Expected limits from the response headers, got %+v.", resp.Limits)
	}
}

func TestLiveSendAttachment(t *testing.T) {
	c, user := liveClient(t)
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for x := range 64 {
		for y := range 64 {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 4), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	err := png.Encode(&buf, img)
	if err != nil {
		t.Fatalf("Failed to encode PNG.\nError: %s", err)
	}
	msg := liveMessage(user, "Message with a PNG attachment.")
	msg.Attachment = &Attachment{Data: buf.Bytes(), Filename: "gradient.png"}
	_, err = c.Send(t.Context(), msg)
	if err != nil {
		t.Fatalf("Failed to send message with attachment.\nError: %s", err)
	}
}

func TestLiveSendEncrypted(t *testing.T) {
	c, user := liveClient(t)
	key, err := NewEncryptionKey()
	if err != nil {
		t.Fatalf("Failed to generate key.\nError: %s", err)
	}
	msg := liveMessage(user, "End-to-end encrypted message.")
	msg.EncryptionKey = key
	_, err = c.Send(t.Context(), msg)
	if err != nil {
		t.Fatalf("Failed to send encrypted message.\nError: %s", err)
	}
}

func TestLiveReadOnlyAccountAPIs(t *testing.T) {
	c, _ := liveClient(t)
	groups, err := c.Groups(t.Context())
	if err != nil {
		t.Fatalf("Failed to list groups.\nError: %s", err)
	}
	t.Logf("Groups: %d.", len(groups))
	if len(groups) > 0 {
		group, err := c.Group(t.Context(), groups[0].Key)
		if err != nil {
			t.Fatalf("Failed to get group.\nError: %s", err)
		}
		if group.Name != groups[0].Name {
			t.Errorf("Expected group name %q, got %q.", groups[0].Name, group.Name)
		}
	}

	credits, err := c.LicenseCredits(t.Context())
	if err != nil {
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Temporary() {
			t.Fatalf("Failed to get license credits.\nError: %s", err)
		}
		t.Logf("License credits are unavailable for this application: %s", err)
		return
	}
	if credits < 0 {
		t.Errorf("Unexpected license credits %d.", credits)
	}
	t.Logf("License credits: %d.", credits)
}
