// Copyright 2026 The Sigstore Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package restapi

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-openapi/errors"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	pkgapi "github.com/sigstore/timestamp-authority/v2/pkg/api"
	"github.com/sigstore/timestamp-authority/v2/pkg/log"
)

func TestWrapMetrics(t *testing.T) {
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := wrapMetrics(dummyHandler)

	tests := []struct {
		name           string
		path           string
		method         string
		expectedPath   string
		expectedMethod string
	}{
		{
			name:           "Valid ping route GET",
			path:           "/ping",
			method:         "GET",
			expectedPath:   "/ping",
			expectedMethod: "GET",
		},
		{
			name:           "Valid timestamp route POST",
			path:           "/api/v1/timestamp",
			method:         "POST",
			expectedPath:   "/api/v1/timestamp",
			expectedMethod: "POST",
		},
		{
			name:           "Valid certchain route GET",
			path:           "/api/v1/timestamp/certchain",
			method:         "GET",
			expectedPath:   "/api/v1/timestamp/certchain",
			expectedMethod: "GET",
		},
		{
			name:           "Unrecognized route GET",
			path:           "/invalid/route",
			method:         "GET",
			expectedPath:   "unrecognized",
			expectedMethod: "GET",
		},
		{
			name:           "Unrecognized route with valid suffix",
			path:           "/api/v1/timestamp/extra",
			method:         "GET",
			expectedPath:   "unrecognized",
			expectedMethod: "GET",
		},
		{
			name:           "Unrecognized route with trailing slash",
			path:           "/api/v1/timestamp/",
			method:         "POST",
			expectedPath:   "unrecognized",
			expectedMethod: "POST",
		},
		{
			name:           "Unrecognized HTTP Method",
			path:           "/api/v1/timestamp",
			method:         "CUSTOM_METHOD",
			expectedPath:   "/api/v1/timestamp",
			expectedMethod: "unrecognized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset metrics before each subtest to isolate counts
			pkgapi.MetricRequestCount.Reset()

			req := httptest.NewRequest(tt.method, tt.path, nil)
			rr := httptest.NewRecorder()

			wrapped.ServeHTTP(rr, req)

			count := testutil.ToFloat64(pkgapi.MetricRequestCount.With(map[string]string{
				"path":   tt.expectedPath,
				"method": tt.expectedMethod,
				"code":   "200",
			}))

			if count != 1 {
				t.Errorf("expected metric request count to be 1, got %f for labels path=%q, method=%q, code=200", count, tt.expectedPath, tt.expectedMethod)
			}
		})
	}
}

func TestHTTPRequestFields_MarshalLogObject(t *testing.T) {
	core, recorded := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)

	fields := &httpRequestFields{
		requestMethod: "POST",
		requestURL:    "http://example.com/api/v1/timestamp",
		requestSize:   128,
		status:        200,
		responseSize:  256,
		userAgent:     "test-agent",
		remoteIp:      "127.0.0.1:1234",
		latency:       150 * time.Millisecond,
		protocol:      "HTTP/1.1",
	}

	logger.Info("test", zap.Object("httpRequest", fields))

	entries := recorded.All()
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(entries))
	}

	ctxMap := entries[0].ContextMap()
	httpReqMap, ok := ctxMap["httpRequest"].(map[string]any)
	if !ok {
		t.Fatalf("expected httpRequest context map, got %v", ctxMap["httpRequest"])
	}

	if httpReqMap["requestMethod"] != "POST" {
		t.Errorf("expected requestMethod POST, got %v", httpReqMap["requestMethod"])
	}
	if httpReqMap["requestUrl"] != "http://example.com/api/v1/timestamp" {
		t.Errorf("expected requestUrl http://example.com/api/v1/timestamp, got %v", httpReqMap["requestUrl"])
	}
	if httpReqMap["requestSize"] != "128" {
		t.Errorf("expected requestSize '128', got %v", httpReqMap["requestSize"])
	}
	if httpReqMap["status"] != 200 {
		t.Errorf("expected status 200, got %v", httpReqMap["status"])
	}
	if httpReqMap["responseSize"] != "256" {
		t.Errorf("expected responseSize '256', got %v", httpReqMap["responseSize"])
	}
	if httpReqMap["userAgent"] != "test-agent" {
		t.Errorf("expected userAgent test-agent, got %v", httpReqMap["userAgent"])
	}
	if httpReqMap["remoteIp"] != "127.0.0.1:1234" {
		t.Errorf("expected remoteIp 127.0.0.1:1234, got %v", httpReqMap["remoteIp"])
	}
	if httpReqMap["latency"] != "0.150000000s" {
		t.Errorf("expected latency 0.150000000s, got %v", httpReqMap["latency"])
	}
	if httpReqMap["protocol"] != "HTTP/1.1" {
		t.Errorf("expected protocol HTTP/1.1, got %v", httpReqMap["protocol"])
	}
}

func TestZapLogEntry_Write(t *testing.T) {
	core, recorded := observer.New(zapcore.InfoLevel)
	oldLogger := log.Logger
	log.Logger = zap.New(core).Sugar()
	defer func() { log.Logger = oldLogger }()

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.TLS = &tls.ConnectionState{}
	req.Header.Set("User-Agent", "curl/7.68.0")
	req.RemoteAddr = "192.168.1.1:5000"

	formatter := &logFormatter{}
	entry := formatter.NewLogEntry(req)

	entry.Write(http.StatusOK, 4, nil, 10*time.Millisecond, "extra-info")

	entries := recorded.All()
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(entries))
	}

	if entries[0].Message != "completed request" {
		t.Errorf("expected message 'completed request', got %q", entries[0].Message)
	}

	ctxMap := entries[0].ContextMap()
	httpReqMap, ok := ctxMap["httpRequest"].(map[string]any)
	if !ok {
		t.Fatalf("expected httpRequest context map, got %v", ctxMap["httpRequest"])
	}
	if !strings.HasPrefix(httpReqMap["requestUrl"].(string), "https://") {
		t.Errorf("expected https scheme for TLS request, got %v", httpReqMap["requestUrl"])
	}
	if ctxMap["extra"] != "extra-info" {
		t.Errorf("expected extra field 'extra-info', got %v", ctxMap["extra"])
	}
}

func TestZapLogEntry_Panic(t *testing.T) {
	core, recorded := observer.New(zapcore.ErrorLevel)
	oldLogger := log.Logger
	log.Logger = zap.New(core).Sugar()
	defer func() { log.Logger = oldLogger }()

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	formatter := &logFormatter{}
	entry := formatter.NewLogEntry(req)

	entry.Panic("something went wrong", []byte("stacktrace info"))

	entries := recorded.All()
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(entries))
	}

	if entries[0].Level != zapcore.ErrorLevel {
		t.Errorf("expected error level, got %v", entries[0].Level)
	}
	if !strings.Contains(entries[0].Message, "panic detected: something went wrong") {
		t.Errorf("expected panic message in log, got %q", entries[0].Message)
	}
}

func TestSetupGlobalMiddleware_Logging(t *testing.T) {
	core, recorded := observer.New(zapcore.InfoLevel)
	oldLogger := log.Logger
	log.Logger = zap.New(core).Sugar()
	defer func() { log.Logger = oldLogger }()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	middlewareHandler := setupGlobalMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/timestamp", nil)
	rr := httptest.NewRecorder()

	middlewareHandler.ServeHTTP(rr, req)

	entries := recorded.All()
	var requestLog *observer.LoggedEntry
	for i := range entries {
		if entries[i].Message == "completed request" {
			requestLog = &entries[i]
			break
		}
	}

	if requestLog == nil {
		t.Fatal("expected 'completed request' log entry")
	}

	ctxMap := requestLog.ContextMap()
	if _, ok := ctxMap["httpRequest"]; !ok {
		t.Errorf("expected httpRequest in log context map, got %v", ctxMap)
	}
}

func TestLogAndServeError(t *testing.T) {
	core, recorded := observer.New(zapcore.DebugLevel)
	oldLogger := log.Logger
	log.Logger = zap.New(core).Sugar()
	defer func() { log.Logger = oldLogger }()

	t.Run("4xx error is logged as Warn with code", func(t *testing.T) {
		recorded.TakeAll()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()

		apiErr := errors.New(http.StatusBadRequest, "bad request from client")
		logAndServeError(rr, req, apiErr)

		entries := recorded.All()
		if len(entries) < 1 {
			t.Fatal("expected at least 1 log entry")
		}

		if entries[0].Level != zapcore.WarnLevel {
			t.Errorf("expected Warn level for 4xx, got %v", entries[0].Level)
		}
		if entries[0].ContextMap()["code"] != int32(http.StatusBadRequest) {
			t.Errorf("expected code %d in context map, got %v", http.StatusBadRequest, entries[0].ContextMap()["code"])
		}
	})

	t.Run("5xx error is logged as Error with code", func(t *testing.T) {
		recorded.TakeAll()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()

		apiErr := errors.New(http.StatusInternalServerError, "internal server error")
		logAndServeError(rr, req, apiErr)

		entries := recorded.All()
		if len(entries) < 1 {
			t.Fatal("expected at least 1 log entry")
		}

		if entries[0].Level != zapcore.ErrorLevel {
			t.Errorf("expected Error level for 5xx, got %v", entries[0].Level)
		}
		if entries[0].ContextMap()["code"] != int32(http.StatusInternalServerError) {
			t.Errorf("expected code %d in context map, got %v", http.StatusInternalServerError, entries[0].ContextMap()["code"])
		}
	})
}
