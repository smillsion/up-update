package app

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func createMemberForDeleteTest(t *testing.T, a *App, username string) int64 {
	t.Helper()
	hash, _ := hashPassword("member-password")
	result, err := a.db.Exec(`INSERT INTO users(username,display_name,password_hash,created_at) VALUES(?,?,?,?)`, username, "Member", hash, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	userID, _ := result.LastInsertId()
	_, _ = a.db.Exec(`INSERT INTO integrations(user_id,bark_server,updated_at) VALUES(?,?,?)`, userID, "https://api.day.app", time.Now().Unix())
	return userID
}

func TestAdminDeletesUserDataAndOnlySafeOrphanCreators(t *testing.T) {
	a := testApp(t)
	handler := a.Routes()
	adminCookie, admin := loginRequest(t, handler, "admin", "admin-password")
	memberID := createMemberForDeleteTest(t, a, "member")
	_, _ = loginRequest(t, handler, "member", "member-password")

	encryptedCookie, _ := a.vault.Encrypt("SESSDATA=member")
	encryptedToken, _ := a.vault.Encrypt("refresh-token")
	_, _ = a.db.Exec(`UPDATE integrations SET bili_cookie_enc=?,bili_status='valid',bark_key_enc='encrypted-bark' WHERE user_id=?`, encryptedCookie, memberID)
	_, _ = a.db.Exec(`INSERT INTO bili_refresh_tokens(user_id,refresh_token_enc,refreshed_at) VALUES(?,?,100)`, memberID, encryptedToken)
	_, _ = a.db.Exec(`INSERT INTO creators(mid,name,updated_at) VALUES('orphan','Orphan',100),('shared-history','Shared',100)`)
	_, _ = a.db.Exec(`INSERT INTO subscriptions(user_id,creator_mid,subscribed_at) VALUES(?,'orphan',100),(?,'shared-history',100)`, memberID, memberID)
	_, _ = a.db.Exec(`INSERT INTO videos(bvid,creator_mid,title,url,published_at,detected_at) VALUES('ORPHAN-BV','orphan','Orphan video','https://example/orphan',100,100),('SHARED-BV','shared-history','Shared video','https://example/shared',100,100)`)
	_, _ = a.db.Exec(`INSERT INTO deliveries(user_id,bvid,status,next_attempt_at,created_at) VALUES(?,'ORPHAN-BV','sent',0,100),(?,'SHARED-BV','sent',0,100)`, memberID, admin.ID)
	a.qrMu.Lock()
	a.qrSessions["member-qr"] = &biliQRSession{ID: "member-qr", UserID: memberID, ExpiresAt: time.Now().Add(time.Minute)}
	a.qrMu.Unlock()

	request := httptest.NewRequest(http.MethodDelete, "/api/admin/users/"+strconv.FormatInt(memberID, 10), bytes.NewBufferString(`{"confirmUsername":"member"}`))
	request.AddCookie(adminCookie)
	request.Header.Set("X-CSRF-Token", admin.CSRFToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", response.Code, response.Body.String())
	}

	for table, query := range map[string]string{
		"users":             `SELECT COUNT(*) FROM users WHERE id=?`,
		"sessions":          `SELECT COUNT(*) FROM sessions WHERE user_id=?`,
		"integrations":      `SELECT COUNT(*) FROM integrations WHERE user_id=?`,
		"refresh tokens":    `SELECT COUNT(*) FROM bili_refresh_tokens WHERE user_id=?`,
		"subscriptions":     `SELECT COUNT(*) FROM subscriptions WHERE user_id=?`,
		"member deliveries": `SELECT COUNT(*) FROM deliveries WHERE user_id=?`,
	} {
		var count int
		if err := a.db.QueryRow(query, memberID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count=%d, err=%v", table, count, err)
		}
	}
	var orphanCount, sharedCount, adminHistory int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM creators WHERE mid='orphan'`).Scan(&orphanCount)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM creators WHERE mid='shared-history'`).Scan(&sharedCount)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM deliveries WHERE user_id=? AND bvid='SHARED-BV'`, admin.ID).Scan(&adminHistory)
	if orphanCount != 0 || sharedCount != 1 || adminHistory != 1 {
		t.Fatalf("orphan=%d shared=%d admin history=%d", orphanCount, sharedCount, adminHistory)
	}
	a.qrMu.Lock()
	_, qrExists := a.qrSessions["member-qr"]
	a.qrMu.Unlock()
	if qrExists {
		t.Fatal("deleted user's QR session still exists")
	}
}

func TestAdminDeleteUserRequiresExactConfirmationAndProtectsAdmin(t *testing.T) {
	a := testApp(t)
	handler := a.Routes()
	adminCookie, admin := loginRequest(t, handler, "admin", "admin-password")
	memberID := createMemberForDeleteTest(t, a, "CaseSensitive")

	requestDelete := func(id int64, confirmation string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodDelete, "/api/admin/users/"+strconv.FormatInt(id, 10), bytes.NewBufferString(`{"confirmUsername":"`+confirmation+`"}`))
		request.AddCookie(adminCookie)
		request.Header.Set("X-CSRF-Token", admin.CSRFToken)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	if response := requestDelete(memberID, "casesensitive"); response.Code != http.StatusBadRequest {
		t.Fatalf("mismatched confirmation status=%d", response.Code)
	}
	if response := requestDelete(admin.ID, "admin"); response.Code != http.StatusBadRequest {
		t.Fatalf("self/admin deletion status=%d", response.Code)
	}
	var count int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM users WHERE id IN (?,?)`, admin.ID, memberID).Scan(&count)
	if count != 2 {
		t.Fatalf("protected users count=%d, want 2", count)
	}
}
