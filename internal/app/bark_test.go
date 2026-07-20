package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBarkClientAcceptsOfficialSuccessResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var message BarkMessage
		if r.URL.Path != "/push" {
			t.Errorf("path=%s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
			t.Error(err)
		}
		if message.DeviceKey != "device-key" || message.URL == "" {
			t.Errorf("message=%#v", message)
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 200, "message": "success"})
	}))
	defer server.Close()
	err := NewBarkClient().Send(context.Background(), server.URL, BarkMessage{DeviceKey: "device-key", Body: "body", URL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBarkClientAcceptsLegacySuccessResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "success"})
	}))
	defer server.Close()

	err := NewBarkClient().Send(context.Background(), server.URL, BarkMessage{DeviceKey: "device-key", Body: "body"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBarkClientRejectsErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 500, "message": "push failed"})
	}))
	defer server.Close()

	err := NewBarkClient().Send(context.Background(), server.URL, BarkMessage{DeviceKey: "device-key", Body: "body"})
	if err == nil || err.Error() != "push failed" {
		t.Fatalf("error=%v, want push failed", err)
	}
}
