package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestValidApprovalPayload(t *testing.T) {
	payload := map[string]any{"proposal_id": "proposal-1", "base_commit_sha": "commit-1"}
	canonical, _ := json.Marshal(payload)
	digest := sha256.Sum256(canonical)
	hash := hex.EncodeToString(digest[:])
	if !validApprovalPayload(canonical, hash, "proposal-1", "commit-1") {
		t.Fatal("valid canonical approval payload should pass")
	}
	if validApprovalPayload(canonical, hash, "proposal-2", "commit-1") {
		t.Fatal("proposal substitution must fail")
	}
}

func TestValidFixPath(t *testing.T) {
	valid := []string{"src/server.go", "tests/server_test.go", "config/app.yaml"}
	invalid := []string{"", "../secret", "/etc/passwd", ".env", ".github/workflows/deploy.yml", "secret.pem"}
	for _, path := range valid {
		if !validFixPath(path) {
			t.Fatalf("expected valid fix path: %s", path)
		}
	}
	for _, path := range invalid {
		if validFixPath(path) {
			t.Fatalf("expected invalid fix path: %s", path)
		}
	}
}
