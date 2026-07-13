package middleware

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type metricKey struct {
	method string
	status int
}

type metricValue struct {
	requests int64
	duration float64
}

// HTTPMetrics intentionally labels only by method and status to avoid a
// workspace/project-ID cardinality explosion in Prometheus.
type HTTPMetrics struct {
	mutex  sync.Mutex
	values map[metricKey]metricValue
}

func NewHTTPMetrics() *HTTPMetrics {
	return &HTTPMetrics{values: make(map[metricKey]metricValue)}
}

func (metrics *HTTPMetrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(recorder, r)
		key := metricKey{method: r.Method, status: recorder.statusCode}
		metrics.mutex.Lock()
		value := metrics.values[key]
		value.requests++
		value.duration += time.Since(start).Seconds()
		metrics.values[key] = value
		metrics.mutex.Unlock()
	})
}

func (metrics *HTTPMetrics) Handler(w http.ResponseWriter, _ *http.Request) {
	metrics.mutex.Lock()
	keys := make([]metricKey, 0, len(metrics.values))
	values := make(map[metricKey]metricValue, len(metrics.values))
	for key, value := range metrics.values {
		keys = append(keys, key)
		values[key] = value
	}
	metrics.mutex.Unlock()
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].method == keys[j].method {
			return keys[i].status < keys[j].status
		}
		return keys[i].method < keys[j].method
	})
	var output strings.Builder
	output.WriteString("# HELP opspilot_gateway_http_requests_total HTTP requests handled.\n")
	output.WriteString("# TYPE opspilot_gateway_http_requests_total counter\n")
	for _, key := range keys {
		value := values[key]
		fmt.Fprintf(&output, "opspilot_gateway_http_requests_total{method=%q,status=%q} %d\n", key.method, fmt.Sprint(key.status), value.requests)
	}
	output.WriteString("# HELP opspilot_gateway_http_request_duration_seconds HTTP request duration.\n")
	output.WriteString("# TYPE opspilot_gateway_http_request_duration_seconds summary\n")
	for _, key := range keys {
		value := values[key]
		labels := fmt.Sprintf("method=%q,status=%q", key.method, fmt.Sprint(key.status))
		fmt.Fprintf(&output, "opspilot_gateway_http_request_duration_seconds_sum{%s} %.6f\n", labels, value.duration)
		fmt.Fprintf(&output, "opspilot_gateway_http_request_duration_seconds_count{%s} %d\n", labels, value.requests)
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(output.String()))
}
