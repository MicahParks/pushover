[![Go Reference](https://pkg.go.dev/badge/github.com/MicahParks/pushover.svg)](https://pkg.go.dev/github.com/MicahParks/pushover)
[![Test](https://github.com/MicahParks/pushover/actions/workflows/test.yml/badge.svg)](https://github.com/MicahParks/pushover/actions/workflows/test.yml)

# pushover

A Go client for the [Pushover API](https://pushover.net/api), with no dependencies outside the standard library.

* Covers every server-side Pushover API: Messages, Receipts and Callbacks, Sounds, Limits, User Validation, Groups,
  Glances, Licensing, Subscriptions, and Teams.
* Supports every message option, including image attachments, HTML and monospace formatting, time to live, and
  emergency priority with receipts, callbacks, and tags.
* Implements Pushover's [end-to-end encryption](https://pushover.net/api#e2ee), tested against the reference OpenSSL
  implementation.
* Validates input that Pushover would reject before sending it, because Pushover temporarily blocks IP addresses that
  send too many invalid requests.
* Follows Pushover's [API etiquette](https://pushover.net/api#friendly) by default: at most 2 concurrent requests,
  never retrying rejected requests, and waiting at least 5 seconds between opt-in retries. Retries don't resend a
  message after a timeout, when it may already have been delivered.
* Returns structured errors with Pushover's messages and request IDs.

```go
import "github.com/MicahParks/pushover"
```

## Basic usage

Register an application on [pushover.net](https://pushover.net/apps/build) to get an API token. For complete programs,
see the [`examples`](examples) directory.

```go
client, err := pushover.New(apiToken, pushover.Options{})
if err != nil {
	log.Fatalf("Failed to create Pushover client.\nError: %s", err)
}

msg := pushover.Message{
	User:     userKey, // A user key, a group key, or a comma-separated list of user keys.
	Message:  "Backup of database \"example\" finished in 16 minutes.",
	Title:    "Backup finished",
	Priority: pushover.PriorityLow,
	Sound:    pushover.SoundCashRegister,
	URL:      "https://example.com/backups",
	URLTitle: "View backups",
	TTL:      24 * time.Hour,
}
resp, err := client.Send(ctx, msg)
if err != nil {
	log.Fatalf("Failed to send Pushover message.\nError: %s", err)
}
log.Printf("Sent message with request ID %s.", resp.Request)
```

## Emergency priority

Emergency-priority messages repeat until they are acknowledged or expire. Sending one returns a receipt.

```go
resp, err := client.Send(ctx, pushover.Message{
	User:     userKey,
	Message:  "Server mail01 failed to respond to 3 pings.",
	Priority: pushover.PriorityEmergency,
	Retry:    time.Minute,      // At least 30 seconds.
	Expire:   30 * time.Minute, // At most 3 hours.
	Tags:     []string{"s=mail01", "l=chicago"},
	Callback: "https://example.com/pushover/callback?secret=" + secret, // Optional.
})

receipt, err := client.Receipt(ctx, resp.Receipt) // Poll no more than once every 5 seconds.
err = client.CancelReceipt(ctx, resp.Receipt)     // Stop retrying one message.
err = client.CancelByTag(ctx, "l=chicago")        // Stop retrying every message with a tag.
```

Instead of polling, use `pushover.CallbackHandler` to receive acknowledgments. Pushover does not sign callback
requests, so put a secret in the callback URL and check it. See [`examples/callback`](examples/callback/main.go).

```go
http.Handle("POST /pushover/callback", pushover.CallbackHandler(func(ctx context.Context, cb pushover.Callback) error {
	log.Printf("%s was acknowledged by %s.", cb.Receipt, cb.AcknowledgedBy)
	return nil // Returning an error responds with HTTP 500 and Pushover retries in one minute.
}))
```

## Attachments

```go
data, err := os.ReadFile("front-door.jpg")

_, err = client.Send(ctx, pushover.Message{
	User:       userKey,
	Message:    "Motion detected at the front door.",
	Attachment: &pushover.Attachment{Data: data, Filename: "front-door.jpg"}, // The content type is detected.
})
```

## End-to-end encryption

With end-to-end encryption, the message, title, URL, and URL title are encrypted before they leave your application.
The recipient's Pushover app must be configured with the same 64-character hexadecimal key. Attachments are not
end-to-end encrypted.

```go
key, err := pushover.ParseEncryptionKey(hexKey) // Or pushover.NewEncryptionKey() and key.Hex().

_, err = client.Send(ctx, pushover.Message{
	User:          userKey,
	Message:       "The vault combination is 12-34-56.",
	EncryptionKey: key,
})
```

## Errors

```go
_, err := client.Send(ctx, msg)
var apiErr *pushover.APIError
switch {
case err == nil:
	// The message was queued for delivery.
case errors.Is(err, pushover.ErrInvalidRequest):
	// The input is invalid, so nothing was sent.
case errors.As(err, &apiErr):
	// Pushover responded with an error. apiErr.Errors has the details, and apiErr.Request identifies the request for
	// Pushover support. If errors.Is(err, pushover.ErrQuotaExceeded), the monthly message limit was reached, and
	// apiErr.Limits, when not nil, says when it resets. If apiErr.Temporary() is false, repeating the request won't help.
default:
	// A network error, a timeout, or a canceled context. The message may or may not have been delivered.
}
```

## Options

The zero value of `pushover.Options` is ready to use. Retries are off by default.

```go
client, err := pushover.New(apiToken, pushover.Options{
	HTTPClient:            &http.Client{Timeout: 30 * time.Second}, // Default: a client with a 1 minute timeout.
	Limiter:               rate.NewLimiter(rate.Every(time.Second), 1), // Any type with Wait(ctx) error.
	MaxConcurrentRequests: 2,                                         // Default: 2, as Pushover asks.
	RetryMax:              2,                                         // Retry network errors and HTTP 5xx.
	RetryWait:             10 * time.Second,                          // At least 5 seconds.
})
```

Requests that Pushover rejected with HTTP 4xx are never retried. Requests that would have a different effect if they
were processed twice, such as sending a message, are only retried after a server error or a failed connection, not
after a timeout, because the first attempt may have succeeded. License assignments and Team invitations are never
retried.

## Other APIs

```go
// Sounds, limits, and user validation.
sounds, err := client.Sounds(ctx)
limits, err := client.Limits(ctx)
validation, err := client.ValidateUser(ctx, userKey, "") // Optionally check a device name.

// Delivery groups.
groupKey, err := client.CreateGroup(ctx, "On call")
err = client.AddGroupUser(ctx, groupKey, pushover.GroupUser{User: userKey, Memo: "Alice"})
err = client.DisableGroupUser(ctx, groupKey, pushover.GroupUser{User: userKey})
group, err := client.Group(ctx, groupKey)

// Glances (widgets). Nil fields are left unchanged.
title, percent := "Disk usage", 72
err = client.UpdateGlance(ctx, pushover.Glance{User: userKey, Title: &title, Percent: &percent})

// Licensing.
credits, err := client.AssignLicense(ctx, pushover.LicenseAssignment{Email: "alice@example.com"})

// Subscriptions.
subscribeURL, err := pushover.SubscribeURL(subscriptionURL, successURL, failureURL)
result := pushover.ParseSubscriptionRedirect(r.URL.Query())
subscribedKey, err := client.MigrateSubscription(ctx, pushover.SubscriptionMigration{Subscription: code, User: userKey})

// Teams use a Team API token instead of an application API token.
team, err := pushover.NewTeamClient(teamToken, pushover.Options{})
err = team.AddUser(ctx, pushover.TeamInvitation{Email: "bob@example.com", InstantLogin: true})
```

## Testing code that sends messages

`*pushover.Client` implements the `pushover.Sender` interface. Accept a `pushover.Sender` to substitute a fake in tests,
or use `pushover.NewNop()` to disable notifications by configuration.

```go
var sender pushover.Sender = pushover.NewNop()
if conf.PushoverEnabled {
	sender, err = pushover.New(conf.PushoverToken, pushover.Options{})
}
```

## Running the tests

```sh
go test -race ./...
```

Live tests against the real Pushover API run when `PUSHOVER_TOKEN` and `PUSHOVER_USER` are set. They only send
lowest-priority messages, which don't notify, with a 1 hour time to live, and make no other changes to the account.

```sh
PUSHOVER_TOKEN=... PUSHOVER_USER=... go test -run Live -v
```

## Not implemented

The [Open Client API](https://pushover.net/api/client), used to build apps that receive Pushover messages, is out of
scope.

## License

Apache License 2.0. Pushover is a registered trademark of Pushover, LLC. This project is not affiliated with Pushover,
LLC.
