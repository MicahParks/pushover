package pushover

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// openSSLKey and the vectors below were produced with the OpenSSL shell script in Pushover's documentation at
// https://pushover.net/api#e2ee to verify interoperability with the reference implementation.
const openSSLKey = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

var openSSLVectors = []struct {
	plaintext string
	encrypted string
}{
	{
		plaintext: "hellorld",
		encrypted: "mIxnsggUh1QrnYZmonDRH+PUvklwgxjiLxUVCREDKPE34DM4aP3LU5nV9YVpQ7/ORileV0NkPca0BpScd46i+MITMjG6bagxvMyvYtZDpUc=",
	},
	{
		plaintext: "This has been encrypted",
		encrypted: "svTlK8I+m57gL5LkannDlRqVLdaVeqwePl4easkkSQdWSz9M3bJMGcfHr/L8Lr2QkhiiTPP3Z9z8pOKd2J9rpBxYY3VFRrwwxliVkWC/SYPtjCYzVu6TLZtf22AtCVkE",
	},
	{
		plaintext: "Héllo, 世界 🔐",
		encrypted: "Cao4tiA4TWmosBl+1k9gEap7idb72tZXiTIuZaTOCM5wt77F2TCtF8LG5UGCoPvHh6dJgF0w80NcpJySnCt5l3FvWu+cpxJ5QfsVyMB2Vh9xA+nyfTmQ09o11OVzLfse",
	},
}

func TestEncryptionDecryptOpenSSLVectors(t *testing.T) {
	key, err := ParseEncryptionKey(openSSLKey)
	if err != nil {
		t.Fatalf("Failed to parse key.\nError: %s", err)
	}
	for _, v := range openSSLVectors {
		got, err := key.Decrypt(v.encrypted)
		if err != nil {
			t.Fatalf("Failed to decrypt OpenSSL vector for %q.\nError: %s", v.plaintext, err)
		}
		if got != v.plaintext {
			t.Errorf("Expected %q, got %q.", v.plaintext, got)
		}
	}
}

func TestEncryptionRoundTrip(t *testing.T) {
	key, err := NewEncryptionKey()
	if err != nil {
		t.Fatalf("Failed to generate key.\nError: %s", err)
	}
	for _, plaintext := range []string{"", "a", "exactly sixteen!", strings.Repeat("🔐", MaxMessageLength)} {
		encrypted, err := key.Encrypt(plaintext)
		if err != nil {
			t.Fatalf("Failed to encrypt.\nError: %s", err)
		}
		raw, err := base64.StdEncoding.DecodeString(encrypted)
		if err != nil {
			t.Fatalf("Encrypted value is not standard Base64.\nError: %s", err)
		}
		if (len(raw)-ivSize-macSize)%16 != 0 {
			t.Fatalf("Unexpected encrypted length %d.", len(raw))
		}
		decrypted, err := key.Decrypt(encrypted)
		if err != nil {
			t.Fatalf("Failed to decrypt.\nError: %s", err)
		}
		if decrypted != plaintext {
			t.Errorf("Expected %q, got %q.", plaintext, decrypted)
		}
	}
}

func TestEncryptionRandomIV(t *testing.T) {
	key, err := ParseEncryptionKey(openSSLKey)
	if err != nil {
		t.Fatalf("Failed to parse key.\nError: %s", err)
	}
	a, err := key.Encrypt("same")
	if err != nil {
		t.Fatalf("Failed to encrypt.\nError: %s", err)
	}
	b, err := key.Encrypt("same")
	if err != nil {
		t.Fatalf("Failed to encrypt.\nError: %s", err)
	}
	if a == b {
		t.Error("Expected different ciphertexts for the same plaintext due to random IVs.")
	}
}

func TestEncryptionDecryptRejectsInvalid(t *testing.T) {
	key, err := ParseEncryptionKey(openSSLKey)
	if err != nil {
		t.Fatalf("Failed to parse key.\nError: %s", err)
	}
	otherKey, err := NewEncryptionKey()
	if err != nil {
		t.Fatalf("Failed to generate key.\nError: %s", err)
	}
	valid := openSSLVectors[0].encrypted
	raw, _ := base64.StdEncoding.DecodeString(valid)

	tampered := []byte(string(raw))
	tampered[ivSize] ^= 1

	tests := map[string]struct {
		key   *EncryptionKey
		value string
	}{
		"wrong key":       {key: otherKey, value: valid},
		"modified":        {key: key, value: base64.StdEncoding.EncodeToString(tampered)},
		"truncated":       {key: key, value: base64.StdEncoding.EncodeToString(raw[:len(raw)-1])},
		"too short":       {key: key, value: base64.StdEncoding.EncodeToString(raw[:ivSize+macSize])},
		"invalid base64":  {key: key, value: "not base64!"},
		"empty":           {key: key, value: ""},
		"URL-safe base64": {key: key, value: strings.NewReplacer("+", "-", "/", "_").Replace(valid)},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := tc.key.Decrypt(tc.value)
			if !errors.Is(err, ErrDecrypt) {
				t.Errorf("Expected ErrDecrypt, got %v.", err)
			}
		})
	}
}

func TestEncryptionDecryptRejectsInvalidPaddingAndGzip(t *testing.T) {
	key, err := ParseEncryptionKey(openSSLKey)
	if err != nil {
		t.Fatalf("Failed to parse key.\nError: %s", err)
	}
	// Each payload is encrypted and authenticated with the correct key, so only padding or gzip validation can reject it.
	tests := map[string][]byte{
		"zero padding":     append(bytes.Repeat([]byte{1}, 15), 0),
		"padding too long": append(bytes.Repeat([]byte{1}, 15), 17),
		"bad padding":      append(bytes.Repeat([]byte{1}, 14), 3, 2),
		"not gzip":         pkcs7Pad([]byte("plain text"), aes.BlockSize),
	}
	for name, padded := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := key.Decrypt(encryptRaw(t, key, padded))
			if !errors.Is(err, ErrDecrypt) {
				t.Errorf("Expected ErrDecrypt, got %v.", err)
			}
		})
	}
}

// encryptRaw encrypts already padded data without compression and appends a valid HMAC.
func encryptRaw(t *testing.T, key *EncryptionKey, padded []byte) string {
	t.Helper()
	block, err := aes.NewCipher(key.key[:])
	if err != nil {
		t.Fatalf("Failed to create cipher.\nError: %s", err)
	}
	out := make([]byte, ivSize+len(padded))
	cipher.NewCBCEncrypter(block, out[:ivSize]).CryptBlocks(out[ivSize:], padded)
	mac := hmac.New(sha256.New, key.key[:])
	mac.Write(out)
	return base64.StdEncoding.EncodeToString(mac.Sum(out))
}

func TestEncryptionZeroKeyRejected(t *testing.T) {
	var key EncryptionKey
	_, err := key.Encrypt("secret")
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("Expected ErrInvalidRequest when encrypting with a zero key, got %v.", err)
	}
	_, err = key.Decrypt(openSSLVectors[0].encrypted)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("Expected ErrInvalidRequest when decrypting with a zero key, got %v.", err)
	}
}

func TestParseEncryptionKey(t *testing.T) {
	key, err := ParseEncryptionKey(strings.ToUpper(openSSLKey))
	if err != nil {
		t.Fatalf("Failed to parse uppercase key.\nError: %s", err)
	}
	if key.Hex() != openSSLKey {
		t.Errorf("Expected hex %q, got %q.", openSSLKey, key.Hex())
	}
	for _, invalid := range []string{"", "00", openSSLKey + "00", strings.Repeat("zz", 32)} {
		_, err = ParseEncryptionKey(invalid)
		if !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("Expected ErrInvalidRequest for %q, got %v.", invalid, err)
		}
	}
}

func TestEncryptionKeyFormattingIsRedacted(t *testing.T) {
	key, err := ParseEncryptionKey(openSSLKey)
	if err != nil {
		t.Fatalf("Failed to parse key.\nError: %s", err)
	}
	for _, format := range []string{"%s", "%v", "%+v", "%#v"} {
		for _, v := range []any{key, *key, Message{EncryptionKey: key}} {
			s := fmt.Sprintf(format, v)
			if strings.Contains(s, openSSLKey) || strings.Contains(s, "1 2 3 4 5") {
				t.Errorf("Formatting with %s revealed the key: %s", format, s)
			}
		}
	}
}

func FuzzDecrypt(f *testing.F) {
	key, err := ParseEncryptionKey(openSSLKey)
	if err != nil {
		f.Fatalf("Failed to parse key.\nError: %s", err)
	}
	for _, v := range openSSLVectors {
		f.Add(v.encrypted)
	}
	f.Fuzz(func(t *testing.T, value string) {
		_, _ = key.Decrypt(value)
	})
}
