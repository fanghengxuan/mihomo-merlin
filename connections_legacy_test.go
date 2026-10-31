package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// 默认响应保留旧 connections 字段；legacy=0 时省略以减小 payload。
func TestConnectionsLegacyQuery(t *testing.T) {
	a := newBackendTestApp(t)

	rec := httptest.NewRecorder()
	a.handleConnections(rec, httptest.NewRequest("GET", "/api/connections", nil))
	var withLegacy map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &withLegacy); err != nil {
		t.Fatalf("默认响应不是 JSON: %v", err)
	}
	if _, ok := withLegacy["connections"]; !ok {
		t.Fatal("默认响应应保留 connections 字段")
	}

	rec = httptest.NewRecorder()
	a.handleConnections(rec, httptest.NewRequest("GET", "/api/connections?legacy=0", nil))
	var noLegacy map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &noLegacy); err != nil {
		t.Fatalf("legacy=0 响应不是 JSON: %v", err)
	}
	if _, ok := noLegacy["connections"]; ok {
		t.Fatal("legacy=0 不应返回 connections 字段")
	}
	if _, ok := noLegacy["rows"]; !ok {
		t.Fatal("legacy=0 仍应返回 rows")
	}
}
