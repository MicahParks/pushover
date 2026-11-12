package pushover

import (
	"context"
	"net/http"
)

// Sound is a notification sound. In addition to the built-in sounds below, custom sounds uploaded to the account that
// owns the application can be used by name. See https://pushover.net/api#sounds
type Sound string

// Built-in notification sounds.
const (
	SoundPushover     Sound = "pushover"
	SoundBike         Sound = "bike"
	SoundBugle        Sound = "bugle"
	SoundCashRegister Sound = "cashregister"
	SoundClassical    Sound = "classical"
	SoundCosmic       Sound = "cosmic"
	SoundFalling      Sound = "falling"
	SoundGamelan      Sound = "gamelan"
	SoundIncoming     Sound = "incoming"
	SoundIntermission Sound = "intermission"
	SoundMagic        Sound = "magic"
	SoundMechanical   Sound = "mechanical"
	SoundPianoBar     Sound = "pianobar"
	SoundSiren        Sound = "siren"
	SoundSpaceAlarm   Sound = "spacealarm"
	SoundTugboat      Sound = "tugboat"
	SoundAlien        Sound = "alien"
	SoundClimb        Sound = "climb"
	SoundPersistent   Sound = "persistent"
	SoundEcho         Sound = "echo"
	SoundUpDown       Sound = "updown"
	SoundVibrate      Sound = "vibrate"
	SoundNone         Sound = "none"
)

// Sounds returns the sounds available to the application, mapped to their descriptions. This includes the built-in
// sounds and any custom sounds uploaded to the account that owns the application.
//
// Applications that let users choose a sound should also offer a blank option that leaves Message.Sound empty, so the
// user's default sound plays.
func (c *Client) Sounds(ctx context.Context) (map[Sound]string, error) {
	var resp struct {
		Sounds map[Sound]string `json:"sounds"`
	}
	r := apiRequest{
		method: http.MethodGet,
		path:   "sounds.json",
	}
	_, err := c.api.do(ctx, r, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Sounds, nil
}
