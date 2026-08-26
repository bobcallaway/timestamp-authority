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

package log

import (
	"context"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestEncodeLevel(t *testing.T) {
	tests := []struct {
		level    zapcore.Level
		expected string
	}{
		{zapcore.DebugLevel, "DEBUG"},
		{zapcore.InfoLevel, "INFO"},
		{zapcore.WarnLevel, "WARNING"},
		{zapcore.ErrorLevel, "ERROR"},
		{zapcore.DPanicLevel, "CRITICAL"},
		{zapcore.PanicLevel, "ALERT"},
		{zapcore.FatalLevel, "EMERGENCY"},
	}

	encoder := encodeLevel()
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			var result []string
			mockArr := &testArrayEncoder{appendStr: func(s string) { result = append(result, s) }}
			encoder(tt.level, mockArr)
			if len(result) != 1 || result[0] != tt.expected {
				t.Errorf("expected level %s to encode to %s, got %v", tt.level, tt.expected, result)
			}
		})
	}
}

type testArrayEncoder struct {
	zapcore.PrimitiveArrayEncoder
	appendStr func(string)
}

func (t *testArrayEncoder) AppendString(v string) {
	if t.appendStr != nil {
		t.appendStr(v)
	}
}

func TestContextLogger_Operation(t *testing.T) {
	core, recorded := observer.New(zapcore.InfoLevel)
	oldLogger := Logger
	Logger = zap.New(core).Sugar()
	defer func() { Logger = oldLogger }()

	ctx := WithRequestID(context.Background(), "req-12345")
	logger := ContextLogger(ctx)
	logger.Info("hello world")

	entries := recorded.All()
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(entries))
	}

	ctxMap := entries[0].ContextMap()
	opMap, ok := ctxMap["operation"].(map[string]any)
	if !ok {
		t.Fatalf("expected operation object in log, got %v", ctxMap["operation"])
	}
	if opMap["id"] != "req-12345" {
		t.Errorf("expected operation.id 'req-12345', got %v", opMap["id"])
	}
}

func TestRequestIDLogger(t *testing.T) {
	core, recorded := observer.New(zapcore.InfoLevel)
	oldLogger := Logger
	Logger = zap.New(core).Sugar()
	defer func() { Logger = oldLogger }()

	// Test with nil request
	nilLogger := RequestIDLogger(nil)
	nilLogger.Info("nil test")

	// Test with request with request ID
	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(WithRequestID(req.Context(), "req-67890"))
	reqLogger := RequestIDLogger(req)
	reqLogger.Info("req test")

	entries := recorded.All()
	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries, got %d", len(entries))
	}

	// First entry should not have operation
	if _, ok := entries[0].ContextMap()["operation"]; ok {
		t.Errorf("expected no operation for nil request log, got %v", entries[0].ContextMap())
	}

	// Second entry should have operation
	opMap, ok := entries[1].ContextMap()["operation"].(map[string]any)
	if !ok || opMap["id"] != "req-67890" {
		t.Errorf("expected operation.id 'req-67890', got %v", entries[1].ContextMap()["operation"])
	}
}

func TestConfigureLogger(t *testing.T) {
	ConfigureLogger("prod")
	if Logger == nil {
		t.Fatal("expected Logger to be initialized in prod mode")
	}

	ConfigureLogger("dev")
	if Logger == nil {
		t.Fatal("expected Logger to be initialized in dev mode")
	}
}
