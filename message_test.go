package pushover

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func validMessage() Message {
	return Message{
		User:    testUser,
		Message: "Backup of database \"example\" finished in 16 minutes.",
	}
}

func validEmergency() Message {
	m := validMessage()
	m.Priority = PriorityEmergency
	m.Retry = MinEmergencyRetry
	m.Expire = time.Hour
	return m
}

func TestMessageValidate(t *testing.T) {
	tests := map[string]struct {
		modify func(m *Message)
		valid  bool
	}{
		"minimal":                {modify: func(m *Message) {}, valid: true},
		"missing user":           {modify: func(m *Message) { m.User = "" }},
		"missing message":        {modify: func(m *Message) { m.Message = "" }},
		"user with space":        {modify: func(m *Message) { m.User = testUser + ", " + testUser }},
		"multiple users":         {modify: func(m *Message) { m.User = testUser + "," + testGroup }, valid: true},
		"empty user in list":     {modify: func(m *Message) { m.User = testUser + "," }},
		"max users":              {modify: func(m *Message) { m.User = repeatJoin(testUser, MaxUsersPerMessage) }, valid: true},
		"too many users":         {modify: func(m *Message) { m.User = repeatJoin(testUser, MaxUsersPerMessage+1) }},
		"long message accepted":  {modify: func(m *Message) { m.Message = strings.Repeat("世", MaxMessageLength+100) }, valid: true},
		"long title accepted":    {modify: func(m *Message) { m.Title = strings.Repeat("a", MaxTitleLength+100) }, valid: true},
		"max URL":                {modify: func(m *Message) { m.URL = strings.Repeat("a", MaxURLLength) }, valid: true},
		"URL too long":           {modify: func(m *Message) { m.URL = strings.Repeat("a", MaxURLLength+1) }},
		"URL title too long":     {modify: func(m *Message) { m.URLTitle = strings.Repeat("a", MaxURLTitleLength+1) }},
		"devices":                {modify: func(m *Message) { m.Device = "iphone,droid_4-x" }, valid: true},
		"empty device in list":   {modify: func(m *Message) { m.Device = "iphone," }},
		"device invalid char":    {modify: func(m *Message) { m.Device = "my phone" }},
		"device too long":        {modify: func(m *Message) { m.Device = strings.Repeat("a", MaxDeviceNameLength+1) }},
		"HTML":                   {modify: func(m *Message) { m.HTML = true }, valid: true},
		"HTML and monospace":     {modify: func(m *Message) { m.HTML, m.Monospace = true, true }},
		"priority too low":       {modify: func(m *Message) { m.Priority = -3 }},
		"priority too high":      {modify: func(m *Message) { m.Priority = 3 }},
		"TTL":                    {modify: func(m *Message) { m.TTL = time.Hour }, valid: true},
		"negative TTL":           {modify: func(m *Message) { m.TTL = -time.Second }},
		"sub-second TTL":         {modify: func(m *Message) { m.TTL = time.Millisecond }},
		"timestamp before epoch": {modify: func(m *Message) { m.Timestamp = time.Unix(-1, 0) }},
		"retry without emergency": {
			modify: func(m *Message) { m.Retry = time.Minute },
		},
		"tags without emergency": {
			modify: func(m *Message) { m.Tags = []string{"a"} },
		},
		"callback without emergency": {
			modify: func(m *Message) { m.Callback = "https://example.com/cb" },
		},
		"emergency": {
			modify: func(m *Message) { *m = validEmergency() },
			valid:  true,
		},
		"emergency with callback and tags": {
			modify: func(m *Message) {
				*m = validEmergency()
				m.Callback = "https://example.com/cb?secret=abc"
				m.Tags = []string{"s=mail01", "l=chicago"}
			},
			valid: true,
		},
		"emergency retry too short": {
			modify: func(m *Message) { *m = validEmergency(); m.Retry = 29 * time.Second },
		},
		"emergency missing expire": {
			modify: func(m *Message) { *m = validEmergency(); m.Expire = 0 },
		},
		"emergency expire too long": {
			modify: func(m *Message) { *m = validEmergency(); m.Expire = MaxEmergencyExpire + time.Second },
		},
		"emergency relative callback": {
			modify: func(m *Message) { *m = validEmergency(); m.Callback = "/callback" },
		},
		"emergency tag with comma": {
			modify: func(m *Message) { *m = validEmergency(); m.Tags = []string{"a,b"} },
		},
		"attachment": {
			modify: func(m *Message) { m.Attachment = &Attachment{Data: []byte("x"), ContentType: "image/png"} },
			valid:  true,
		},
		"empty attachment": {
			modify: func(m *Message) { m.Attachment = &Attachment{} },
		},
		"attachment too large": {
			modify: func(m *Message) { m.Attachment = &Attachment{Data: make([]byte, MaxAttachmentSize+1)} },
		},
		"attachment filename injection": {
			modify: func(m *Message) {
				m.Attachment = &Attachment{Data: []byte("x"), Filename: "a.png\r\nContent-Type: text/html"}
			},
		},
		"attachment invalid content type": {
			modify: func(m *Message) { m.Attachment = &Attachment{Data: []byte("x"), ContentType: "image/png\r\nX: y"} },
		},
		"attachment content type header injection": {
			modify: func(m *Message) { m.Attachment = &Attachment{Data: []byte("x"), ContentType: "image/png;\r\n\r\nx=y"} },
		},
		"zero encryption key": {
			modify: func(m *Message) { m.EncryptionKey = &EncryptionKey{} },
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			m := validMessage()
			tc.modify(&m)
			err := m.Validate()
			if tc.valid && err != nil {
				t.Errorf("Expected valid message.\nError: %s", err)
			}
			if !tc.valid && !errors.Is(err, ErrInvalidRequest) {
				t.Errorf("Expected ErrInvalidRequest, got %v.", err)
			}
		})
	}
}

func TestSendAllParameters(t *testing.T) {
	c, f := newFakeAPI(t, http.StatusOK, `{"status":1,"request":"647d2300-702c-4b38-8b2f-d56326ae460b","receipt":"rLqVuqTRh62UzxtmqiaLzQmVcPgiCy"}`)
	f.setHeader("X-Limit-App-Limit", "10000")
	f.setHeader("X-Limit-App-Remaining", "7496")
	f.setHeader("X-Limit-App-Reset", "1393653600")

	msg := Message{
		User:      testUser,
		Message:   "Server mail01 failed to respond to 3 pings",
		Title:     "Backup finished - SQL1",
		Device:    "droid4,iphone",
		Monospace: true,
		Priority:  PriorityEmergency,
		Sound:     SoundSiren,
		Timestamp: time.Unix(1331249662, 0),
		TTL:       90*time.Second + 500*time.Millisecond,
		URL:       "twitter://direct_message?screen_name=someuser",
		URLTitle:  "Reply to @someuser",
		Retry:     60 * time.Second,
		Expire:    30 * time.Minute,
		Callback:  "https://example.com/callback",
		Tags:      []string{"s=mail01", "r=23", "l=chicago"},
	}
	resp, err := c.Send(t.Context(), msg)
	if err != nil {
		t.Fatalf("Failed to send message.\nError: %s", err)
	}

	got := f.last(t)
	assertRequest(t, got, http.MethodPost, "/1/messages.json")
	if got.ContentType != "application/x-www-form-urlencoded" {
		t.Errorf("Unexpected content type %q.", got.ContentType)
	}
	if ua := got.Header.Get("User-Agent"); ua != DefaultUserAgent {
		t.Errorf("Unexpected User-Agent %q.", ua)
	}
	assertParams(t, got.Params, map[string]string{
		"user":      testUser,
		"message":   msg.Message,
		"title":     msg.Title,
		"device":    "droid4,iphone",
		"monospace": "1",
		"priority":  "2",
		"sound":     "siren",
		"timestamp": "1331249662",
		"ttl":       "90",
		"url":       msg.URL,
		"url_title": msg.URLTitle,
		"retry":     "60",
		"expire":    "1800",
		"callback":  msg.Callback,
		"tags":      "s=mail01,r=23,l=chicago",
	})

	if resp.Request != "647d2300-702c-4b38-8b2f-d56326ae460b" {
		t.Errorf("Unexpected request ID %q.", resp.Request)
	}
	if resp.Receipt != "rLqVuqTRh62UzxtmqiaLzQmVcPgiCy" {
		t.Errorf("Unexpected receipt %q.", resp.Receipt)
	}
	if resp.Limits == nil {
		t.Fatal("Expected limits from response headers.")
	}
	if resp.Limits.Limit != 10000 || resp.Limits.Remaining != 7496 || !resp.Limits.Reset.Equal(time.Unix(1393653600, 0)) {
		t.Errorf("Unexpected limits %+v.", *resp.Limits)
	}
}

func TestSendMinimalOmitsOptionalParameters(t *testing.T) {
	c, f := newFakeAPI(t, http.StatusOK, `{"status":1,"request":"r"}`)
	resp, err := c.Send(t.Context(), validMessage())
	if err != nil {
		t.Fatalf("Failed to send message.\nError: %s", err)
	}
	assertParams(t, f.last(t).Params, map[string]string{
		"user":    testUser,
		"message": validMessage().Message,
	})
	if resp.Receipt != "" {
		t.Errorf("Expected no receipt, got %q.", resp.Receipt)
	}
	if resp.Limits != nil {
		t.Errorf("Expected no limits without headers, got %+v.", *resp.Limits)
	}
}

func TestSendHTMLAndLowPriority(t *testing.T) {
	c, f := newFakeAPI(t, http.StatusOK, `{"status":1,"request":"r"}`)
	msg := validMessage()
	msg.HTML = true
	msg.Priority = PriorityLowest
	_, err := c.Send(t.Context(), msg)
	if err != nil {
		t.Fatalf("Failed to send message.\nError: %s", err)
	}
	assertParams(t, f.last(t).Params, map[string]string{
		"user":     testUser,
		"message":  msg.Message,
		"html":     "1",
		"priority": "-2",
	})
}

func TestSendAttachment(t *testing.T) {
	c, f := newFakeAPI(t, http.StatusOK, `{"status":1,"request":"r"}`)
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	tests := map[string]struct {
		attachment Attachment
		wantName   string
		wantType   string
	}{
		"explicit": {
			attachment: Attachment{Data: []byte{0xff, 0xd8, 0xff}, Filename: `my "photo".jpg`, ContentType: "image/jpeg"},
			wantName:   `my "photo".jpg`,
			wantType:   "image/jpeg",
		},
		"detected": {
			attachment: Attachment{Data: png},
			wantName:   "attachment",
			wantType:   "image/png",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			msg := validMessage()
			msg.Title = "With attachment"
			msg.Attachment = &tc.attachment
			_, err := c.Send(t.Context(), msg)
			if err != nil {
				t.Fatalf("Failed to send message.\nError: %s", err)
			}
			got := f.last(t)
			if !strings.HasPrefix(got.ContentType, "multipart/form-data; boundary=") {
				t.Fatalf("Expected multipart/form-data, got %q.", got.ContentType)
			}
			assertParams(t, got.Params, map[string]string{
				"user":    testUser,
				"message": msg.Message,
				"title":   msg.Title,
			})
			if !bytes.Equal(got.File, tc.attachment.Data) {
				t.Errorf("Attachment data mismatch.")
			}
			if got.FileName != tc.wantName {
				t.Errorf("Expected filename %q, got %q.", tc.wantName, got.FileName)
			}
			if got.FileType != tc.wantType {
				t.Errorf("Expected attachment content type %q, got %q.", tc.wantType, got.FileType)
			}
		})
	}
}

func TestSendEncrypted(t *testing.T) {
	c, f := newFakeAPI(t, http.StatusOK, `{"status":1,"request":"r"}`)
	key, err := NewEncryptionKey()
	if err != nil {
		t.Fatalf("Failed to generate key.\nError: %s", err)
	}
	msg := validMessage()
	msg.Title = "hellorld"
	msg.URL = "https://example.com/secret"
	msg.URLTitle = "Secret link"
	msg.Sound = SoundMagic
	msg.EncryptionKey = key
	_, err = c.Send(t.Context(), msg)
	if err != nil {
		t.Fatalf("Failed to send message.\nError: %s", err)
	}
	params := f.last(t).Params
	if params.Get("encrypted") != "1" {
		t.Errorf("Expected encrypted=1, got %q.", params.Get("encrypted"))
	}
	if params.Get("sound") != "magic" || params.Get("user") != testUser {
		t.Errorf("Expected unencrypted user and sound, got %v.", params)
	}
	for name, plaintext := range map[string]string{
		"message":   msg.Message,
		"title":     msg.Title,
		"url":       msg.URL,
		"url_title": msg.URLTitle,
	} {
		value := params.Get(name)
		if value == plaintext {
			t.Errorf("Expected %s to be encrypted.", name)
			continue
		}
		decrypted, err := key.Decrypt(value)
		if err != nil {
			t.Errorf("Failed to decrypt %s.\nError: %s", name, err)
			continue
		}
		if decrypted != plaintext {
			t.Errorf("Expected decrypted %s %q, got %q.", name, plaintext, decrypted)
		}
	}
}

func TestSendInvalidMessageMakesNoRequest(t *testing.T) {
	c, f := newFakeAPI(t, http.StatusOK, `{"status":1,"request":"r"}`)
	_, err := c.Send(t.Context(), Message{User: testUser})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Expected ErrInvalidRequest, got %v.", err)
	}
	if f.count() != 0 {
		t.Errorf("Expected no requests, got %d.", f.count())
	}
}

func TestPriorityString(t *testing.T) {
	if PriorityEmergency.String() != "emergency" || PriorityLowest.String() != "lowest" || Priority(7).String() != "Priority(7)" {
		t.Error("Unexpected priority string.")
	}
}

func TestNop(t *testing.T) {
	resp, err := NewNop().Send(t.Context(), Message{})
	if err != nil {
		t.Fatalf("Expected no error from nop sender.\nError: %s", err)
	}
	if resp != (SendResponse{}) {
		t.Errorf("Expected empty response, got %+v.", resp)
	}
}

func repeatJoin(s string, n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = s
	}
	return strings.Join(parts, ",")
}
