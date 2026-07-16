package middleware

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRateLimitUsesTokenFingerprintAndReturns429(t *testing.T) {
	handler := RateLimit(2, time.Minute, slog.Default())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for index := 0; index < 3; index++ {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/test", nil)
		request.Header.Set("Authorization", "Bearer private-token")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		expected := http.StatusNoContent
		if index == 2 {
			expected = http.StatusTooManyRequests
		}
		if recorder.Code != expected {
			t.Fatalf("request %d returned %d, expected %d", index, recorder.Code, expected)
		}
		if recorder.Header().Get("X-RateLimit-Limit") != "2" {
			t.Fatal("missing rate-limit headers")
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	handler := SecurityHeaders(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Header().Get("X-Content-Type-Options") != "nosniff" || recorder.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("production security headers were not set")
	}
}

func TestRequestIDRejectsUnsafeValuesAndStoresSafeValue(t *testing.T) {
	var observed string
	handler := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		observed = RequestIDFromContext(r.Context())
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-ID", "unsafe\nvalue")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if observed == "" || observed == "unsafe\nvalue" || recorder.Header().Get("X-Request-ID") != observed {
		t.Fatal("unsafe request ID was not replaced and propagated")
	}

	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Request-ID", "safe-request_123")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if observed != "safe-request_123" {
		t.Fatal("valid request ID was not preserved")
	}
}

func TestHTTPMetricsExposeLowCardinalityCounters(t *testing.T) {
	metrics := NewHTTPMetrics()
	handler := metrics.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/workspace/secret-id", nil))
	recorder := httptest.NewRecorder()
	metrics.Handler(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := recorder.Body.String()
	if !strings.Contains(body, `opspilot_gateway_http_requests_total{method="POST",status="201"} 1`) {
		t.Fatal("metrics counter is missing")
	}
	if strings.Contains(body, "secret-id") {
		t.Fatal("metrics must not contain tenant identifiers")
	}
}
