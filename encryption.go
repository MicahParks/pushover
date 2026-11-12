package pushover

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

const (
	// EncryptionKeySize is the size of an end-to-end encryption key in bytes.
	EncryptionKeySize = 32

	ivSize          = aes.BlockSize
	macSize         = sha256.Size
	maxDecryptedLen = 1 << 20
)

// ErrDecrypt is returned when an end-to-end encrypted value can't be decrypted, because it's malformed, was encrypted
// with a different key, or was modified.
var ErrDecrypt = errors.New("pushover: failed to decrypt value")

// EncryptionKey is a 256-bit key for Pushover end-to-end encryption. The same key must be configured in the
// recipient's Pushover app, where it's represented as a 64-character hexadecimal string. See Message.EncryptionKey.
//
// Each field is encrypted independently: the plaintext is gzip compressed, encrypted with AES-256-CBC with PKCS #7
// padding and a random 16-byte IV, authenticated with HMAC-SHA256 over the IV and ciphertext using the same key, and
// encoded as standard Base64 of IV || ciphertext || HMAC. See https://pushover.net/api#e2ee
type EncryptionKey struct {
	key [EncryptionKeySize]byte
}

// NewEncryptionKey generates a random encryption key. Use EncryptionKey.Hex to get the value to configure in the
// Pushover app.
func NewEncryptionKey() (*EncryptionKey, error) {
	k := &EncryptionKey{}
	_, err := io.ReadFull(rand.Reader, k.key[:])
	if err != nil {
		return nil, fmt.Errorf("pushover: failed to generate encryption key: %w", err)
	}
	return k, nil
}

// ParseEncryptionKey parses a 64-character hexadecimal encryption key, as shown in the Pushover app.
func ParseEncryptionKey(hexKey string) (*EncryptionKey, error) {
	if len(hexKey) != hex.EncodedLen(EncryptionKeySize) {
		return nil, fmt.Errorf("%w: encryption key must be %d hexadecimal characters", ErrInvalidRequest, hex.EncodedLen(EncryptionKeySize))
	}
	k := &EncryptionKey{}
	_, err := hex.Decode(k.key[:], []byte(hexKey))
	if err != nil {
		return nil, fmt.Errorf("%w: encryption key is not valid hexadecimal", ErrInvalidRequest)
	}
	return k, nil
}

// Hex returns the key as a 64-character lowercase hexadecimal string, the format used by the Pushover app.
func (k *EncryptionKey) Hex() string {
	return hex.EncodeToString(k.key[:])
}

// String implements fmt.Stringer without revealing the key.
func (k EncryptionKey) String() string {
	return "pushover.EncryptionKey(REDACTED)"
}

// GoString implements fmt.GoStringer without revealing the key.
func (k EncryptionKey) GoString() string {
	return k.String()
}

// Encrypt encrypts a single field value for an end-to-end encrypted message. Client.Send calls this automatically
// when Message.EncryptionKey is set.
func (k *EncryptionKey) Encrypt(plaintext string) (string, error) {
	if k.isZero() {
		return "", errZeroKey
	}
	var compressed bytes.Buffer
	zw, err := gzip.NewWriterLevel(&compressed, gzip.BestCompression)
	if err != nil {
		return "", fmt.Errorf("pushover: failed to create gzip writer: %w", err)
	}
	_, err = zw.Write([]byte(plaintext))
	if err != nil {
		return "", fmt.Errorf("pushover: failed to gzip compress value: %w", err)
	}
	err = zw.Close()
	if err != nil {
		return "", fmt.Errorf("pushover: failed to gzip compress value: %w", err)
	}
	padded := pkcs7Pad(compressed.Bytes(), aes.BlockSize)

	block, err := aes.NewCipher(k.key[:])
	if err != nil {
		return "", fmt.Errorf("pushover: failed to create AES cipher: %w", err)
	}
	out := make([]byte, ivSize+len(padded), ivSize+len(padded)+macSize)
	iv := out[:ivSize]
	_, err = io.ReadFull(rand.Reader, iv)
	if err != nil {
		return "", fmt.Errorf("pushover: failed to generate IV: %w", err)
	}
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out[ivSize:], padded)

	mac := hmac.New(sha256.New, k.key[:])
	mac.Write(out)
	out = mac.Sum(out)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt decrypts a value produced by Encrypt or by any other implementation of Pushover end-to-end encryption. The
// HMAC is verified before decrypting. An error wrapping ErrDecrypt is returned if the value is malformed, was
// encrypted with a different key, or was modified.
func (k *EncryptionKey) Decrypt(encrypted string) (string, error) {
	if k.isZero() {
		return "", errZeroKey
	}
	raw, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", fmt.Errorf("%w: invalid Base64: %w", ErrDecrypt, err)
	}
	ctLen := len(raw) - ivSize - macSize
	if ctLen < aes.BlockSize || ctLen%aes.BlockSize != 0 {
		return "", fmt.Errorf("%w: invalid length", ErrDecrypt)
	}
	iv := raw[:ivSize]
	ciphertext := raw[ivSize : ivSize+ctLen]
	tag := raw[ivSize+ctLen:]

	mac := hmac.New(sha256.New, k.key[:])
	mac.Write(raw[:ivSize+ctLen])
	if !hmac.Equal(mac.Sum(nil), tag) {
		return "", fmt.Errorf("%w: HMAC mismatch, wrong key or modified value", ErrDecrypt)
	}

	block, err := aes.NewCipher(k.key[:])
	if err != nil {
		return "", fmt.Errorf("pushover: failed to create AES cipher: %w", err)
	}
	padded := make([]byte, ctLen)
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(padded, ciphertext)
	compressed, err := pkcs7Unpad(padded, aes.BlockSize)
	if err != nil {
		return "", err
	}

	zr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return "", fmt.Errorf("%w: invalid gzip data: %w", ErrDecrypt, err)
	}
	plaintext, err := io.ReadAll(io.LimitReader(zr, maxDecryptedLen+1))
	if err != nil {
		return "", fmt.Errorf("%w: invalid gzip data: %w", ErrDecrypt, err)
	}
	if len(plaintext) > maxDecryptedLen {
		return "", fmt.Errorf("%w: decompressed value exceeds %d bytes", ErrDecrypt, maxDecryptedLen)
	}
	return string(plaintext), nil
}

var errZeroKey = fmt.Errorf("%w: encryption key is all zeros, create it with ParseEncryptionKey or NewEncryptionKey", ErrInvalidRequest)

func (k *EncryptionKey) isZero() bool {
	return k.key == [EncryptionKeySize]byte{}
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	n := blockSize - len(data)%blockSize
	return append(data, bytes.Repeat([]byte{byte(n)}, n)...)
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, fmt.Errorf("%w: invalid padding", ErrDecrypt)
	}
	n := int(data[len(data)-1])
	if n == 0 || n > blockSize {
		return nil, fmt.Errorf("%w: invalid padding", ErrDecrypt)
	}
	for _, b := range data[len(data)-n:] {
		if int(b) != n {
			return nil, fmt.Errorf("%w: invalid padding", ErrDecrypt)
		}
	}
	return data[:len(data)-n], nil
}
