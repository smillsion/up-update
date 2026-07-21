package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func settingsRequest(method, target, body string, userID int64) *http.Request {
	request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	return request.WithContext(context.WithValue(request.Context(), userContextKey, currentUser{ID: userID}))
}

func adminUserIDForTest(t *testing.T, a *App) int64 {
	t.Helper()
	var userID int64
	if err := a.db.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	return userID
}

func TestSaveBarkKeepsExistingKeyWhenInputIsEmpty(t *testing.T) {
	a := testApp(t)
	userID := adminUserIDForTest(t, a)
	encrypted, _ := a.vault.Encrypt("existing-key")
	_, _ = a.db.Exec(`UPDATE integrations SET bark_key_enc=? WHERE user_id=?`, encrypted, userID)
	request := settingsRequest(http.MethodPut, "/api/settings/bark", `{"server":"https://example.test","deviceKey":"","level":"critical","sound":"bell","quietEnabled":true,"quietStart":"12:00","quietEnd":"14:00"}`, userID)
	response := httptest.NewRecorder()
	a.saveBarkHandler(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	settings, err := a.loadBark(userID)
	if err != nil {
		t.Fatal(err)
	}
	if settings.Key != "existing-key" || settings.Level != "critical" || settings.Sound != "bell" || !settings.QuietEnabled {
		t.Fatalf("saved settings=%+v", settings)
	}
}

func TestSaveBarkRequiresKeyWhenNotConfigured(t *testing.T) {
	a := testApp(t)
	userID := adminUserIDForTest(t, a)
	request := settingsRequest(http.MethodPut, "/api/settings/bark", `{"server":"https://example.test","deviceKey":"","level":"active","sound":"","quietEnabled":false,"quietStart":"12:00","quietEnd":"14:00"}`, userID)
	response := httptest.NewRecorder()
	a.saveBarkHandler(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestBarkTestUsesDraftWithoutSavingIt(t *testing.T) {
	var received BarkMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "message": "success"})
	}))
	defer server.Close()
	a := testApp(t)
	userID := adminUserIDForTest(t, a)
	encrypted, _ := a.vault.Encrypt("saved-key")
	_, _ = a.db.Exec(`UPDATE integrations SET bark_key_enc=?,bark_level='active',bark_sound='' WHERE user_id=?`, encrypted, userID)
	body, _ := json.Marshal(map[string]any{"server": server.URL, "deviceKey": "", "level": "critical", "sound": "alarm"})
	request := settingsRequest(http.MethodPost, "/api/settings/bark/test", string(body), userID)
	response := httptest.NewRecorder()
	a.testBarkHandler(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if received.DeviceKey != "saved-key" || received.Level != "critical" || received.Sound != "alarm" {
		t.Fatalf("received=%+v", received)
	}
	stored, err := a.loadBark(userID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Level != "active" || stored.Sound != "" {
		t.Fatalf("draft was persisted: %+v", stored)
	}
}

func TestEffectiveBarkLevelUsesQuietWindow(t *testing.T) {
	settings := barkSettings{Level: "critical", QuietEnabled: true, QuietStart: "12:00", QuietEnd: "14:00"}
	if got := effectiveBarkLevel(settings, shanghaiTime(12, 0)); got != "passive" {
		t.Fatalf("quiet level=%s, want passive", got)
	}
	if got := effectiveBarkLevel(settings, shanghaiTime(14, 0)); got != "critical" {
		t.Fatalf("level after quiet window=%s, want critical", got)
	}
	settings.QuietStart, settings.QuietEnd = "23:00", "07:00"
	if got := effectiveBarkLevel(settings, time.Date(2026, 7, 21, 1, 0, 0, 0, shanghaiLocation)); got != "passive" {
		t.Fatalf("cross-midnight quiet level=%s, want passive", got)
	}
}
