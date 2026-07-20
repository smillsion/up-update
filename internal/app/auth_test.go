package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func loginRequest(t *testing.T, handler http.Handler, username, password string) (*http.Cookie, currentUser) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", response.Code, response.Body.String())
	}
	var user currentUser
	if err := json.Unmarshal(response.Body.Bytes(), &user); err != nil {
		t.Fatal(err)
	}
	return response.Result().Cookies()[0], user
}

func TestAuthorizationAndCSRF(t *testing.T) {
	a := testApp(t)
	handler := a.Routes()
	adminCookie, admin := loginRequest(t, handler, "admin", "admin-password")

	createBody := bytes.NewBufferString(`{"username":"member","displayName":"Member","temporaryPassword":"member-password"}`)
	withoutCSRF := httptest.NewRequest(http.MethodPost, "/api/admin/users", createBody)
	withoutCSRF.AddCookie(adminCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, withoutCSRF)
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status=%d", response.Code)
	}

	hash, _ := hashPassword("member-password")
	result, err := a.db.Exec(`INSERT INTO users(username,display_name,password_hash,created_at) VALUES(?,?,?,?)`, "member", "Member", hash, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	memberID, _ := result.LastInsertId()
	_, _ = a.db.Exec(`INSERT INTO integrations(user_id,bark_server,updated_at) VALUES(?,?,?)`, memberID, "https://api.day.app", time.Now().Unix())
	memberCookie, _ := loginRequest(t, handler, "member", "member-password")
	adminRequest := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	adminRequest.AddCookie(memberCookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, adminRequest)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-admin status=%d", response.Code)
	}
	if admin.CSRFToken == "" {
		t.Fatal("login did not return CSRF token")
	}
}
