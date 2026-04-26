package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const EncryptedSecretPrefix = "enc:aes256gcm:v1:"

type Codec struct {
	aead cipher.AEAD
}

func NewCodec(key string) (*Codec, error) {
	keyBytes, err := decodeKeyMaterial(key)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	return &Codec{aead: aead}, nil
}

func (c *Codec) Encrypt(plaintext string) (string, error) {
	if c == nil {
		return "", errors.New("secret codec is nil")
	}
	if plaintext == "" {
		return "", nil
	}

	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := c.aead.Seal(nil, nonce, []byte(plaintext), nil)
	payload := append(nonce, ciphertext...)
	return EncryptedSecretPrefix + base64.StdEncoding.EncodeToString(payload), nil
}

func (c *Codec) Decrypt(payload string) (string, error) {
	if c == nil {
		return "", errors.New("secret codec is nil")
	}
	if payload == "" {
		return "", nil
	}
	if !strings.HasPrefix(payload, EncryptedSecretPrefix) {
		return "", fmt.Errorf("secret payload does not use %s", EncryptedSecretPrefix)
	}

	rawPayload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(payload, EncryptedSecretPrefix))
	if err != nil {
		return "", err
	}
	if len(rawPayload) < c.aead.NonceSize() {
		return "", errors.New("secret payload is too short")
	}

	nonce := rawPayload[:c.aead.NonceSize()]
	ciphertext := rawPayload[c.aead.NonceSize():]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func ResolveEnvOrFile(name string) (string, error) {
	if value := os.Getenv(name); value != "" {
		return value, nil
	}

	filePath := os.Getenv(name + "_FILE")
	if filePath == "" {
		return "", nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func decodeKeyMaterial(key string) ([]byte, error) {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return nil, errors.New("secret key is empty")
	}

	if raw, err := base64.StdEncoding.DecodeString(trimmed); err == nil && len(raw) == 32 {
		return raw, nil
	}
	if len(trimmed) == 32 {
		return []byte(trimmed), nil
	}
	return nil, errors.New("secret key must be 32 raw bytes or base64-encoded 32 bytes")
}
