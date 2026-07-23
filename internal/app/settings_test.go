package app

import (
	"bytes"
	"context"
	"database/sql"
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

func TestDeleteBilibiliCredentialsPreservesUserData(t *testing.T) {
	a := testApp(t)
	userID := adminUserIDForTest(t, a)
	encryptedCookie, _ := a.vault.Encrypt("SESSDATA=saved")
	encryptedToken, _ := a.vault.Encrypt("refresh-token")
	encryptedBark, _ := a.vault.Encrypt("bark-key")
	_, _ = a.db.Exec(`UPDATE integrations SET bili_cookie_enc=?,bili_status='valid',bili_name='tester',bili_last_validated=100,bili_error='old',bark_key_enc=? WHERE user_id=?`, encryptedCookie, encryptedBark, userID)
	_, _ = a.db.Exec(`INSERT INTO bili_refresh_tokens(user_id,refresh_token_enc,refreshed_at) VALUES(?,?,100)`, userID, encryptedToken)
	_, _ = a.db.Exec(`INSERT INTO creators(mid,name,updated_at) VALUES('1','UP',100)`)
	_, _ = a.db.Exec(`INSERT INTO subscriptions(user_id,creator_mid,subscribed_at) VALUES(?,'1',100)`, userID)
	a.qrMu.Lock()
	a.qrSessions["active"] = &biliQRSession{ID: "active", UserID: userID, ExpiresAt: time.Now().Add(time.Minute)}
	a.qrMu.Unlock()

	request := settingsRequest(http.MethodDelete, "/api/settings/bilibili", "", userID)
	response := httptest.NewRecorder()
	a.deleteBilibiliHandler(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var cookie, status, name, biliError, barkKey string
	var validated sql.NullInt64
	if err := a.db.QueryRow(`SELECT bili_cookie_enc,bili_status,bili_name,bili_last_validated,bili_error,bark_key_enc FROM integrations WHERE user_id=?`, userID).Scan(&cookie, &status, &name, &validated, &biliError, &barkKey); err != nil {
		t.Fatal(err)
	}
	if cookie != "" || status != "missing" || name != "" || validated.Valid || biliError != "" || barkKey != encryptedBark {
		t.Fatalf("integration=(%q,%q,%q,%v,%q,%q)", cookie, status, name, validated, biliError, barkKey)
	}
	var tokenCount, subscriptionCount int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM bili_refresh_tokens WHERE user_id=?`, userID).Scan(&tokenCount)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM subscriptions WHERE user_id=?`, userID).Scan(&subscriptionCount)
	if tokenCount != 0 || subscriptionCount != 1 {
		t.Fatalf("tokens=%d subscriptions=%d", tokenCount, subscriptionCount)
	}
	a.qrMu.Lock()
	_, qrExists := a.qrSessions["active"]
	a.qrMu.Unlock()
	if qrExists {
		t.Fatal("QR session still exists")
	}
}
