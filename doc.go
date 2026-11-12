// Package pushover is a client for the Pushover API (https://pushover.net/api).
//
// It covers the server-side Pushover APIs:
//
//   - Message API: send notifications with priorities, sounds, HTML or monospace formatting, supplementary URLs,
//     image attachments, time to live, and end-to-end encryption. See Client.Send.
//   - Receipts and callbacks for emergency-priority messages. See Client.Receipt, Client.CancelReceipt,
//     Client.CancelByTag, and CallbackHandler.
//   - Sounds, application limits, and user validation. See Client.Sounds, Client.Limits, and Client.ValidateUser.
//   - Groups API to manage delivery groups. See Client.CreateGroup and related methods.
//   - Glances API to update widgets. See Client.UpdateGlance.
//   - Licensing API. See Client.AssignLicense and Client.LicenseCredits.
//   - Subscription API. See Client.MigrateSubscription, SubscribeURL, and ParseSubscriptionRedirect.
//   - Teams API, which uses a Team API token. See TeamClient.
//
// # Being friendly to the API
//
// Pushover asks API clients to follow some rules, which this package does by default:
//
//   - At most 2 concurrent HTTP requests are made per client. See Options.MaxConcurrentRequests.
//   - Requests that Pushover rejected as invalid (HTTP 4xx) are never retried. Pushover temporarily blocks IP
//     addresses that repeatedly send invalid requests, so input is validated locally first where possible.
//   - Retries after network errors or server errors (HTTP 5xx) are opt-in, and never happen sooner than 5 seconds
//     after the previous attempt. Requests like sending a message are not retried when the first attempt may have
//     succeeded. See Options.RetryMax.
//   - HTTP keep-alive is used to send requests over the same connection.
//
// # Errors
//
// Errors caused by invalid input that were caught before a request was sent wrap ErrInvalidRequest. Errors returned by
// the Pushover API are of type *APIError, which includes Pushover's error messages and the request ID to give to
// Pushover support. When the account's monthly message limit is reached, errors.Is(err, ErrQuotaExceeded) is true.
package pushover
