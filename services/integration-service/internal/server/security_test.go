package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestVerifySignatureFailsClosedAndAcceptsValidHMAC(t *testing.T) {
	payload := []byte(`{"action":"opened"}`)
	secret := "test-webhook-secret"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if verifySignature(payload, signature, "") {
		t.Fatal("missing webhook secret must fail closed")
	}
	if verifySignature(payload, "invalid", secret) {
		t.Fatal("malformed signature must fail")
	}
	if !verifySignature(payload, signature, secret) {
		t.Fatal("valid signature should pass")
	}
	if verifySignature([]byte("tampered"), signature, secret) {
		t.Fatal("tampered payload must fail")
	}
}

func TestValidGitBranch(t *testing.T) {
	valid := []string{"main", "feature/fix-login", "release-2026.07"}
	invalid := []string{"", "@", ".hidden", "feature..bad", "bad branch", "refs/@{bad}", "bad~ref"}
	for _, branch := range valid {
		if !validGitBranch(branch) {
			t.Fatalf("expected valid branch: %s", branch)
		}
	}
	for _, branch := range invalid {
		if validGitBranch(branch) {
			t.Fatalf("expected invalid branch: %s", branch)
		}
	}
}
