package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRelayWorkerErrorPreservesSafeLimitAndConflictStatus(t *testing.T) {
	for _, test := range []struct {
		status int
		code   string
	}{
		{http.StatusTooManyRequests, "USAGE_LIMIT_REACHED"},
		{http.StatusConflict, "BLOCKED"},
	} {
		recorder := httptest.NewRecorder()
		if !relayWorkerError(recorder, test.status, []byte(`{"detail":"safe explanation"}`), "BLOCKED", "fallback") {
			t.Fatal("expected worker error to be handled")
		}
		if recorder.Code != test.status || !strings.Contains(recorder.Body.String(), test.code) || !strings.Contains(recorder.Body.String(), "safe explanation") {
			t.Fatalf("unexpected relayed response: %d %s", recorder.Code, recorder.Body.String())
		}
	}
}

func TestRelayWorkerErrorHidesUnexpectedUpstreamDetail(t *testing.T) {
	recorder := httptest.NewRecorder()
	relayWorkerError(recorder, http.StatusInternalServerError, []byte(`{"detail":"database internals"}`), "FAILED", "safe fallback")
	if recorder.Code != http.StatusBadGateway || strings.Contains(recorder.Body.String(), "database internals") {
		t.Fatalf("unexpected upstream detail leaked: %s", recorder.Body.String())
	}
}
