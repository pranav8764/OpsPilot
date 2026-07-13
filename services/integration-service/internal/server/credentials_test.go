package server

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestCredentialEncryptionRoundTripAndTamperDetection(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))
	original := providerCredential{Token: "provider-secret-token", TokenKind: "bearer"}
	encrypted, err := encryptCredential(key, original)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encrypted, original.Token) {
		t.Fatal("ciphertext exposed the provider token")
	}
	decoded, err := decryptCredential(key, encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != original {
		t.Fatalf("round trip mismatch: %#v", decoded)
	}

	last := encrypted[len(encrypted)-1]
	replacement := byte('A')
	if last == replacement {
		replacement = 'B'
	}
	if _, err := decryptCredential(key, encrypted[:len(encrypted)-1]+string(replacement)); err == nil {
		t.Fatal("tampered ciphertext must be rejected")
	}
}

func TestCredentialKeyRequires32Bytes(t *testing.T) {
	short := base64.StdEncoding.EncodeToString([]byte("too-short"))
	if _, err := encryptCredential(short, providerCredential{Token: "x"}); err == nil {
		t.Fatal("short encryption keys must be rejected")
	}
}
