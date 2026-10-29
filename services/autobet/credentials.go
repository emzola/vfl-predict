package autobet

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func encryptCredentialValue(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	payload := append(nonce, sealed...)
	return base64.StdEncoding.EncodeToString(payload), nil
}

func decryptCredentialValue(key []byte, ciphertext string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(ciphertext))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(decoded) < gcm.NonceSize() {
		return "", errors.New("encrypted credential payload is truncated")
	}
	nonce := decoded[:gcm.NonceSize()]
	payload := decoded[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, payload, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func credentialEncryptionKey(raw string) ([]byte, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, errors.New("auto betting credential encryption key is not configured")
	}
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
		if decoded, err := encoding.DecodeString(value); err == nil {
			if len(decoded) == 32 {
				return decoded, nil
			}
		}
	}
	if len(value) == 32 {
		return []byte(value), nil
	}
	return nil, fmt.Errorf("auto betting credential encryption key must decode to 32 bytes")
}

func maskUsername(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if local, domain, ok := strings.Cut(trimmed, "@"); ok {
		return maskSegment(local) + "@" + domain
	}
	return maskSegment(trimmed)
}

func maskSegment(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) == 1 {
		return "*"
	}
	if len(runes) == 2 {
		return string(runes[0]) + "*"
	}
	return string(runes[0]) + strings.Repeat("*", len(runes)-2) + string(runes[len(runes)-1])
}

func normalizeCredential(value string) string {
	if !utf8.ValidString(value) {
		return ""
	}
	return strings.TrimSpace(value)
}
