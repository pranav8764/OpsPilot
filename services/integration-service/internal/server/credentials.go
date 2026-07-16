package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
)

type providerCredential struct {
	Token     string `json:"token"`
	TokenKind string `json:"token_kind"`
}

func credentialKey(encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		key, err = base64.RawStdEncoding.DecodeString(encoded)
	}
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("CREDENTIAL_ENCRYPTION_KEY must be a base64-encoded 32-byte key")
	}
	return key, nil
}

func encryptCredential(encodedKey string, value providerCredential) (string, error) {
	key, err := credentialKey(encodedKey)
	if err != nil {
		return "", err
	}
	plaintext, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, []byte("opspilot:provider-credential:v1"))
	return "v1." + base64.RawURLEncoding.EncodeToString(nonce) + "." + base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

func decryptCredential(encodedKey, encrypted string) (providerCredential, error) {
	var result providerCredential
	var version, nonceText, ciphertextText string
	if _, err := fmt.Sscanf(encrypted, "%2s.%s", &version, &nonceText); err != nil || version != "v1" {
		return result, fmt.Errorf("unsupported encrypted credential format")
	}
	// Sscanf cannot split the final two URL-safe fields reliably, so locate the
	// separator after the version explicitly.
	parts := splitDots(encrypted)
	if len(parts) != 3 || parts[0] != "v1" {
		return result, fmt.Errorf("unsupported encrypted credential format")
	}
	nonceText, ciphertextText = parts[1], parts[2]
	key, err := credentialKey(encodedKey)
	if err != nil {
		return result, err
	}
	nonce, err := base64.RawURLEncoding.DecodeString(nonceText)
	if err != nil {
		return result, fmt.Errorf("invalid encrypted credential nonce")
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(ciphertextText)
	if err != nil {
		return result, fmt.Errorf("invalid encrypted credential payload")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return result, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return result, err
	}
	if len(nonce) != aead.NonceSize() {
		return result, fmt.Errorf("invalid encrypted credential nonce")
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, []byte("opspilot:provider-credential:v1"))
	if err != nil {
		return result, fmt.Errorf("encrypted credential authentication failed")
	}
	if err := json.Unmarshal(plaintext, &result); err != nil || result.Token == "" {
		return result, fmt.Errorf("invalid credential payload")
	}
	return result, nil
}

func splitDots(value string) []string {
	parts := make([]string, 0, 3)
	start := 0
	for index, char := range value {
		if char == '.' {
			parts = append(parts, value[start:index])
			start = index + 1
		}
	}
	return append(parts, value[start:])
}
