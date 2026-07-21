package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
	_, _ = a.db.Exec(`INSERT INTO creators(mid,name,updated_at) VALUES('1','UP',100)`)
	_, _ = a.db.Exec(`INSERT INTO videos(bvid,creator_mid,title,url,published_at,detected_at) VALUES('BV1','1','Video','https://example/video',100,100)`)
	_, _ = a.db.Exec(`INSERT INTO deliveries(user_id,bvid,next_attempt_at,deferred_until,created_at) VALUES(?,'BV1',100,9999999999,100)`, userID)
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
	var deferredUntil int64
	if err := a.db.QueryRow(`SELECT deferred_until FROM deliveries WHERE user_id=? AND bvid='BV1'`, userID).Scan(&deferredUntil); err != nil || deferredUntil != 0 {
		t.Fatalf("deferred_until=%d, err=%v; want queue re-evaluation", deferredUntil, err)
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
