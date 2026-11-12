package pushover

import (
	"bytes"
	"context"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits documented by Pushover. Lengths are counted in Unicode characters (runes), not bytes.
const (
	// MaxMessageLength is the documented maximum length of a message body. The API accepts longer messages, so
	// Message.Validate does not enforce it, but text beyond it may not be shown.
	MaxMessageLength = 1024
	// MaxTitleLength is the documented maximum length of a message title. The API accepts longer titles, so
	// Message.Validate does not enforce it, but text beyond it may not be shown.
	MaxTitleLength = 250
	// MaxURLLength is the maximum length of a supplementary URL.
	MaxURLLength = 512
	// MaxURLTitleLength is the maximum length of a supplementary URL's title.
	MaxURLTitleLength = 100
	// MaxAttachmentSize is the maximum size of an attachment in bytes.
	MaxAttachmentSize = 5_242_880
	// MaxUsersPerMessage is the maximum number of comma-separated user keys in a single message.
	MaxUsersPerMessage = 50
	// MaxDeviceNameLength is the maximum length of a device name.
	MaxDeviceNameLength = 25
	// MinEmergencyRetry is the minimum Retry interval for an emergency-priority message.
	MinEmergencyRetry = 30 * time.Second
	// MaxEmergencyExpire is the maximum Expire duration for an emergency-priority message. Pushover also caps the
	// number of retries at 50 regardless of Expire.
	MaxEmergencyExpire = 3 * time.Hour
)

// Priority is the priority of a message. See https://pushover.net/api#priority
type Priority int

const (
	// PriorityLowest generates no notification. On iOS, the application badge number is increased.
	PriorityLowest Priority = -2
	// PriorityLow generates a popup or scrolling notification without sound or vibration.
	PriorityLow Priority = -1
	// PriorityNormal is the default priority. The notification plays sound, vibrates, and alerts according to the
	// user's device settings. Messages received during a user's quiet hours are delivered as PriorityLow.
	PriorityNormal Priority = 0
	// PriorityHigh bypasses the user's quiet hours and is highlighted in red in the device clients.
	PriorityHigh Priority = 1
	// PriorityEmergency repeats the notification every Message.Retry until it is acknowledged or Message.Expire
	// elapses. Sending an emergency-priority message returns a receipt.
	PriorityEmergency Priority = 2
)

// String returns the name of the priority.
func (p Priority) String() string {
	switch p {
	case PriorityLowest:
		return "lowest"
	case PriorityLow:
		return "low"
	case PriorityNormal:
		return "normal"
	case PriorityHigh:
		return "high"
	case PriorityEmergency:
		return "emergency"
	default:
		return "Priority(" + strconv.Itoa(int(p)) + ")"
	}
}

// Message is a notification to send with the Message API. User and Message are required. See https://pushover.net/api
type Message struct {
	// User is the recipient's user key or group key. Multiple user keys may be given as a comma-separated list with no
	// spaces, up to MaxUsersPerMessage. Required.
	User string
	// Message is the body of the notification. Required.
	Message string

	// Title is the message's title. If empty, the application's name is used.
	Title string
	// Device restricts delivery to the named device of the user. Multiple devices may be given as a comma-separated
	// list. If empty, or if no named device is active, the message is sent to all of the user's active devices.
	Device string
	// HTML enables HTML formatting of Message. It can't be combined with Monospace.
	HTML bool
	// Monospace displays Message in a monospace font. It can't be combined with HTML.
	Monospace bool
	// Priority is the message priority. Defaults to PriorityNormal.
	Priority Priority
	// Sound is the notification sound. If empty, the user's default sound is played. Custom sounds uploaded to the
	// account that owns the application may also be used.
	Sound Sound
	// Timestamp is the time shown for the message instead of when the Pushover API received it.
	Timestamp time.Time
	// TTL is how long the message lives on the device before it's deleted automatically. It's truncated to whole
	// seconds. Pushover ignores TTL for emergency-priority messages.
	TTL time.Duration
	// URL is a supplementary URL shown with the message.
	URL string
	// URLTitle is the title shown for URL. If empty, the URL itself is shown.
	URLTitle string
	// Attachment is an optional image attachment.
	Attachment *Attachment

	// Retry is how often an emergency-priority message is repeated until it's acknowledged. It's truncated to whole
	// seconds. Required for, and only allowed with, PriorityEmergency. Must be at least MinEmergencyRetry.
	Retry time.Duration
	// Expire is how long an emergency-priority message is repeated before it expires. It's truncated to whole
	// seconds. Required for, and only allowed with, PriorityEmergency. Must be at most MaxEmergencyExpire.
	Expire time.Duration
	// Callback is a publicly accessible URL that Pushover requests when an emergency-priority message is
	// acknowledged. Only allowed with PriorityEmergency. See ParseCallback and CallbackHandler.
	Callback string
	// Tags are stored with the receipt of an emergency-priority message so it can be canceled with
	// Client.CancelByTag. Only allowed with PriorityEmergency. Tags must not contain commas.
	Tags []string

	// EncryptionKey, if non-nil, enables end-to-end encryption. Message, Title, URL, and URLTitle are encrypted
	// locally with this key before they're sent. The recipient's device must be configured with the same key.
	// Attachments are not end-to-end encrypted.
	EncryptionKey *EncryptionKey
}

// Attachment is an image attached to a message.
type Attachment struct {
	// Data is the content of the file. Required. At most MaxAttachmentSize bytes.
	Data []byte
	// Filename is the attachment's file name. Defaults to "attachment".
	Filename string
	// ContentType is the attachment's MIME type, such as "image/jpeg". If empty, it's detected from Data.
	ContentType string
}

// SendResponse is the result of successfully sending a message.
type SendResponse struct {
	// Request is the unique request identifier assigned by Pushover. Include it when contacting Pushover support.
	Request string
	// Receipt identifies an emergency-priority message. Use it with Client.Receipt and Client.CancelReceipt. It's
	// empty for other priorities.
	Receipt string
	// Limits are the account's message limits from the response headers, or nil if they were not present.
	Limits *Limits
}

// Send sends a message. The message is validated before it's sent, and an error wrapping ErrInvalidRequest is returned
// if it's invalid. If Pushover rejects the message, the error is an *APIError.
func (c *Client) Send(ctx context.Context, msg Message) (SendResponse, error) {
	err := msg.Validate()
	if err != nil {
		return SendResponse{}, err
	}
	params, err := msg.params()
	if err != nil {
		return SendResponse{}, err
	}
	var resp struct {
		Request string `json:"request"`
		Receipt string `json:"receipt"`
	}
	r := apiRequest{
		method:     http.MethodPost,
		path:       "messages.json",
		params:     params,
		attachment: msg.Attachment,
	}
	header, err := c.api.do(ctx, r, &resp)
	if err != nil {
		return SendResponse{}, err
	}
	s := SendResponse{
		Request: resp.Request,
		Receipt: resp.Receipt,
		Limits:  parseLimitHeaders(header),
	}
	return s, nil
}

// Validate checks the message against the requirements and limits documented by Pushover. The returned error wraps
// ErrInvalidRequest. Client.Send calls Validate automatically.
func (m Message) Validate() error {
	invalid := func(format string, a ...any) error {
		return fmt.Errorf("%w: "+format, append([]any{ErrInvalidRequest}, a...)...)
	}

	if m.User == "" {
		return invalid("user is required")
	}
	if strings.ContainsFunc(m.User, isSpace) {
		return invalid("user must not contain whitespace, separate multiple user keys with commas only")
	}
	users := strings.Split(m.User, ",")
	if len(users) > MaxUsersPerMessage {
		return invalid("user has %d keys, the maximum is %d", len(users), MaxUsersPerMessage)
	}
	if slices.Contains(users, "") {
		return invalid("user contains an empty key")
	}
	if m.Message == "" {
		return invalid("message is required")
	}
	if n := utf8.RuneCountInString(m.URL); n > MaxURLLength {
		return invalid("URL is %d characters, the maximum is %d", n, MaxURLLength)
	}
	if n := utf8.RuneCountInString(m.URLTitle); n > MaxURLTitleLength {
		return invalid("URL title is %d characters, the maximum is %d", n, MaxURLTitleLength)
	}
	if m.Device != "" {
		for _, device := range strings.Split(m.Device, ",") {
			if err := validateDevice(device); err != nil {
				return err
			}
		}
	}
	if m.HTML && m.Monospace {
		return invalid("HTML and monospace can't be used together")
	}
	if m.Priority < PriorityLowest || m.Priority > PriorityEmergency {
		return invalid("priority %d is not between %d and %d", m.Priority, PriorityLowest, PriorityEmergency)
	}
	if m.TTL < 0 || (m.TTL > 0 && m.TTL < time.Second) {
		return invalid("TTL must be at least 1 second when set")
	}
	if !m.Timestamp.IsZero() && m.Timestamp.Unix() <= 0 {
		return invalid("timestamp must be after the Unix epoch")
	}

	if m.Priority == PriorityEmergency {
		if m.Retry < MinEmergencyRetry {
			return invalid("retry must be at least %s for emergency-priority messages", MinEmergencyRetry)
		}
		if m.Expire < time.Second {
			return invalid("expire must be at least 1 second for emergency-priority messages")
		}
		if m.Expire > MaxEmergencyExpire {
			return invalid("expire must be at most %s for emergency-priority messages", MaxEmergencyExpire)
		}
		if m.Callback != "" {
			u, err := url.Parse(m.Callback)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
				return invalid("callback must be an absolute HTTP or HTTPS URL")
			}
		}
		for _, tag := range m.Tags {
			if tag == "" || strings.Contains(tag, ",") {
				return invalid("tags must be non-empty and must not contain commas")
			}
		}
	} else if m.Retry != 0 || m.Expire != 0 || m.Callback != "" || len(m.Tags) > 0 {
		return invalid("retry, expire, callback, and tags are only allowed with PriorityEmergency")
	}

	if a := m.Attachment; a != nil {
		if len(a.Data) == 0 {
			return invalid("attachment data is empty")
		}
		if len(a.Data) > MaxAttachmentSize {
			return invalid("attachment is %d bytes, the maximum is %d", len(a.Data), MaxAttachmentSize)
		}
		if strings.ContainsAny(a.Filename, "\r\n") {
			return invalid("attachment filename must not contain line breaks")
		}
		if a.ContentType != "" {
			if strings.ContainsAny(a.ContentType, "\r\n") {
				return invalid("attachment content type must not contain line breaks")
			}
			if _, _, err := mime.ParseMediaType(a.ContentType); err != nil {
				return invalid("attachment content type %q is invalid: %s", a.ContentType, err)
			}
		}
	}
	if m.EncryptionKey != nil && m.EncryptionKey.isZero() {
		return errZeroKey
	}
	return nil
}

// params encodes the message as Pushover API parameters, encrypting fields if needed.
func (m Message) params() (url.Values, error) {
	text := map[string]string{
		"message":   m.Message,
		"title":     m.Title,
		"url":       m.URL,
		"url_title": m.URLTitle,
	}
	v := url.Values{}
	for name, value := range text {
		if value == "" {
			continue
		}
		if m.EncryptionKey != nil {
			var err error
			value, err = m.EncryptionKey.Encrypt(value)
			if err != nil {
				return nil, fmt.Errorf("pushover: failed to encrypt %s: %w", name, err)
			}
		}
		v.Set(name, value)
	}
	if m.EncryptionKey != nil {
		v.Set("encrypted", "1")
	}
	v.Set("user", m.User)
	if m.Device != "" {
		v.Set("device", m.Device)
	}
	if m.HTML {
		v.Set("html", "1")
	}
	if m.Monospace {
		v.Set("monospace", "1")
	}
	if m.Priority != PriorityNormal {
		v.Set("priority", strconv.Itoa(int(m.Priority)))
	}
	if m.Sound != "" {
		v.Set("sound", string(m.Sound))
	}
	if !m.Timestamp.IsZero() {
		v.Set("timestamp", strconv.FormatInt(m.Timestamp.Unix(), 10))
	}
	if m.TTL > 0 {
		v.Set("ttl", seconds(m.TTL))
	}
	if m.Priority == PriorityEmergency {
		v.Set("retry", seconds(m.Retry))
		v.Set("expire", seconds(m.Expire))
		if m.Callback != "" {
			v.Set("callback", m.Callback)
		}
		if len(m.Tags) > 0 {
			v.Set("tags", strings.Join(m.Tags, ","))
		}
	}
	return v, nil
}

// encodeMultipart encodes the parameters and attachment as a multipart/form-data body.
func encodeMultipart(params url.Values, a *Attachment) (body []byte, contentType string, err error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		for _, v := range params[k] {
			err = w.WriteField(k, v)
			if err != nil {
				return nil, "", fmt.Errorf("pushover: failed to write multipart field: %w", err)
			}
		}
	}

	filename := a.Filename
	if filename == "" {
		filename = "attachment"
	}
	partType := a.ContentType
	if partType == "" {
		partType = http.DetectContentType(a.Data)
	}
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="attachment"; filename="`+quoteEscaper.Replace(filename)+`"`)
	h.Set("Content-Type", partType)
	part, err := w.CreatePart(h)
	if err != nil {
		return nil, "", fmt.Errorf("pushover: failed to create multipart attachment: %w", err)
	}
	_, err = part.Write(a.Data)
	if err != nil {
		return nil, "", fmt.Errorf("pushover: failed to write multipart attachment: %w", err)
	}
	err = w.Close()
	if err != nil {
		return nil, "", fmt.Errorf("pushover: failed to close multipart writer: %w", err)
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

var quoteEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

func validateDevice(device string) error {
	if device == "" {
		return fmt.Errorf("%w: device names must not be empty", ErrInvalidRequest)
	}
	if n := utf8.RuneCountInString(device); n > MaxDeviceNameLength {
		return fmt.Errorf("%w: device name %q is %d characters, the maximum is %d", ErrInvalidRequest, device, n, MaxDeviceNameLength)
	}
	for _, r := range device {
		if !isDeviceRune(r) {
			return fmt.Errorf("%w: device name %q may only contain A-Z, a-z, 0-9, _, and -", ErrInvalidRequest, device)
		}
	}
	return nil
}

func isDeviceRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

func seconds(d time.Duration) string {
	return strconv.FormatInt(int64(d/time.Second), 10)
}
