package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type biliAuthTestServer struct {
	server       *httptest.Server
	refreshCalls atomic.Int32
	confirmCalls atomic.Int32
}

func newBiliAuthTestServer(t *testing.T) *biliAuthTestServer {
	t.Helper()
	fixture := &biliAuthTestServer{}
	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/x/passport-login/web/qrcode/generate":
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "temporary", Path: "/"})
			fmt.Fprint(w, `{"code":0,"message":"0","data":{"url":"https://passport.bilibili.com/scan?qrcode_key=qr-key","qrcode_key":"qr-key"}}`)
		case "/x/passport-login/web/qrcode/poll":
			if r.URL.Query().Get("qrcode_key") != "qr-key" {
				t.Errorf("unexpected qrcode key: %s", r.URL.RawQuery)
			}
			for name, value := range map[string]string{"SESSDATA": "login-session", "bili_jct": "login-csrf", "DedeUserID": "123"} {
				http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/"})
			}
			fmt.Fprint(w, `{"code":0,"message":"0","data":{"refresh_token":"login-refresh","code":0,"message":"扫码登录成功"}}`)
		case "/x/frontend/finger/spi":
			fmt.Fprint(w, `{"code":0,"message":"0","data":{"b_3":"device-3","b_4":"device-4"}}`)
		case "/x/web-interface/nav":
			if !strings.Contains(r.Header.Get("Cookie"), "SESSDATA=") {
				fmt.Fprint(w, `{"code":-101,"message":"账号未登录","data":{}}`)
				return
			}
			fmt.Fprint(w, `{"code":0,"message":"0","data":{"isLogin":true,"mid":123,"uname":"tester"}}`)
		case "/x/passport-login/web/cookie/info":
			fmt.Fprint(w, `{"code":0,"message":"0","data":{"refresh":true,"timestamp":1720000000000}}`)
		case "/x/passport-login/web/cookie/refresh":
			fixture.refreshCalls.Add(1)
			if err := r.ParseForm(); err != nil || r.Form.Get("refresh_token") != "old-refresh" || r.Form.Get("refresh_csrf") != "refresh-csrf" || r.Form.Get("csrf") != "old-csrf" {
				t.Errorf("unexpected refresh form: %v, %v", r.Form, err)
			}
			for name, value := range map[string]string{"SESSDATA": "new-session", "bili_jct": "new-csrf", "DedeUserID": "123", "sid": "new-sid"} {
				http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/"})
			}
			fmt.Fprint(w, `{"code":0,"message":"0","data":{"status":0,"refresh_token":"new-refresh"}}`)
		case "/x/passport-login/web/confirm/refresh":
			fixture.confirmCalls.Add(1)
			if err := r.ParseForm(); err != nil || r.Form.Get("refresh_token") != "old-refresh" || r.Form.Get("csrf") != "new-csrf" {
				t.Errorf("unexpected confirm form: %v, %v", r.Form, err)
			}
			if !strings.Contains(r.Header.Get("Cookie"), "SESSDATA=new-session") {
				t.Errorf("confirm used wrong cookie: %s", r.Header.Get("Cookie"))
			}
			fmt.Fprint(w, `{"code":0,"message":"0","data":{}}`)
		default:
			if strings.HasPrefix(r.URL.Path, "/correspond/1/") {
				ciphertext := strings.TrimPrefix(r.URL.Path, "/correspond/1/")
				if len(ciphertext) != 256 {
					t.Errorf("correspond path length=%d", len(ciphertext))
				}
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, `<!doctype html><html><head><link rel="stylesheet" href="/style.css"></head><body><div id="1-name">refresh-csrf</div></body></html>`)
				return
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func (fixture *biliAuthTestServer) client() *BilibiliClient {
	client := NewBilibiliClient(fixture.server.URL)
	client.passportURL = fixture.server.URL
	client.wwwURL = fixture.server.URL
	return client
}

func TestBilibiliQRLoginAndCookieRefresh(t *testing.T) {
	fixture := newBiliAuthTestServer(t)
	client := fixture.client()
	ctx := context.Background()
	qr, err := client.StartQRLogin(ctx)
	if err != nil || qr.Key != "qr-key" || !strings.Contains(qr.Cookies, "sid=temporary") {
		t.Fatalf("start QR: %#v, %v", qr, err)
	}
	status, err := client.PollQRLogin(ctx, qr.Key, qr.Cookies)
	if err != nil || status.Status != "success" || status.RefreshToken != "login-refresh" || cookieValue(status.Cookie, "SESSDATA") != "login-session" {
		t.Fatalf("poll QR: %#v, %v", status, err)
	}
	cookie := client.EnsureDeviceCookies(ctx, status.Cookie)
	if cookieValue(cookie, "buvid3") != "device-3" || cookieValue(cookie, "buvid4") != "device-4" {
		t.Fatalf("device cookies missing: %s", cookie)
	}

	oldCookie := "buvid3=device-3; SESSDATA=old-session; bili_jct=old-csrf; DedeUserID=123"
	info, err := client.CookieRefreshInfo(ctx, oldCookie)
	if err != nil || !info.Refresh || info.Timestamp != 1720000000000 {
		t.Fatalf("refresh info: %#v, %v", info, err)
	}
	refreshed, err := client.RefreshCookie(ctx, oldCookie, "old-refresh", info.Timestamp)
	if err != nil || refreshed.RefreshToken != "new-refresh" || cookieValue(refreshed.Cookie, "SESSDATA") != "new-session" || cookieValue(refreshed.Cookie, "buvid3") != "device-3" {
		t.Fatalf("refresh: %#v, %v", refreshed, err)
	}
	if err := client.ConfirmCookieRefresh(ctx, refreshed.Cookie, "old-refresh"); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if fixture.refreshCalls.Load() != 1 || fixture.confirmCalls.Load() != 1 {
		t.Fatalf("refresh calls=%d confirm calls=%d", fixture.refreshCalls.Load(), fixture.confirmCalls.Load())
	}
}

func TestMaintenanceRefreshesStoredBilibiliCredential(t *testing.T) {
	fixture := newBiliAuthTestServer(t)
	a := testApp(t)
	a.bili = fixture.client()
	var userID int64
	if err := a.db.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	oldCookie := "buvid3=device-3; SESSDATA=old-session; bili_jct=old-csrf; DedeUserID=123"
	encryptedCookie, _ := a.vault.Encrypt(oldCookie)
	encryptedToken, _ := a.vault.Encrypt("old-refresh")
	_, _ = a.db.Exec(`UPDATE integrations SET bili_cookie_enc=?,bili_status='valid',bili_last_validated=? WHERE user_id=?`, encryptedCookie, time.Now().Add(-7*time.Hour).Unix(), userID)
	_, _ = a.db.Exec(`INSERT INTO bili_refresh_tokens(user_id,refresh_token_enc,refreshed_at) VALUES(?,?,?)`, userID, encryptedToken, time.Now().Add(-7*time.Hour).Unix())

	a.validateOldestCookie(context.Background())
	var storedCookie, storedToken, status string
	if err := a.db.QueryRow(`SELECT i.bili_cookie_enc,t.refresh_token_enc,i.bili_status FROM integrations i JOIN bili_refresh_tokens t ON t.user_id=i.user_id WHERE i.user_id=?`, userID).Scan(&storedCookie, &storedToken, &status); err != nil {
		t.Fatal(err)
	}
	decryptedCookie, _ := a.vault.Decrypt(storedCookie)
	decryptedToken, _ := a.vault.Decrypt(storedToken)
	if cookieValue(decryptedCookie, "SESSDATA") != "new-session" || decryptedToken != "new-refresh" || status != "valid" {
		t.Fatalf("stored credential=(%q,%q,%q)", decryptedCookie, decryptedToken, status)
	}
}

func TestQRHandlersBindSessionAndSaveCredential(t *testing.T) {
	fixture := newBiliAuthTestServer(t)
	a := testApp(t)
	a.bili = fixture.client()
	var userID int64
	if err := a.db.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	requestFor := func(method, target string, id int64) *http.Request {
		r := httptest.NewRequest(method, target, nil)
		return r.WithContext(context.WithValue(r.Context(), userContextKey, currentUser{ID: id}))
	}
	withID := func(r *http.Request, id string) *http.Request {
		route := chi.NewRouteContext()
		route.URLParams.Add("id", id)
		return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, route))
	}

	startRecorder := httptest.NewRecorder()
	a.startBilibiliQRHandler(startRecorder, requestFor(http.MethodPost, "/api/settings/bilibili/qrcode", userID))
	if startRecorder.Code != http.StatusCreated {
		t.Fatalf("start status=%d body=%s", startRecorder.Code, startRecorder.Body.String())
	}
	var started biliQRStartView
	if err := json.NewDecoder(startRecorder.Body).Decode(&started); err != nil || started.SessionID == "" {
		t.Fatalf("start response=%#v, %v", started, err)
	}

	foreignRecorder := httptest.NewRecorder()
	a.pollBilibiliQRHandler(foreignRecorder, withID(requestFor(http.MethodPost, "/poll", userID+1), started.SessionID))
	if foreignRecorder.Code != http.StatusNotFound {
		t.Fatalf("foreign poll status=%d", foreignRecorder.Code)
	}

	pollRecorder := httptest.NewRecorder()
	a.pollBilibiliQRHandler(pollRecorder, withID(requestFor(http.MethodPost, "/poll", userID), started.SessionID))
	if pollRecorder.Code != http.StatusOK {
		t.Fatalf("poll status=%d body=%s", pollRecorder.Code, pollRecorder.Body.String())
	}
	var polled biliQRPollView
	if err := json.NewDecoder(pollRecorder.Body).Decode(&polled); err != nil || polled.Status != "success" || polled.Name != "tester" {
		t.Fatalf("poll response=%#v, %v", polled, err)
	}
	var encryptedToken string
	if err := a.db.QueryRow(`SELECT refresh_token_enc FROM bili_refresh_tokens WHERE user_id=?`, userID).Scan(&encryptedToken); err != nil {
		t.Fatal(err)
	}
	refreshToken, _ := a.vault.Decrypt(encryptedToken)
	if refreshToken != "login-refresh" {
		t.Fatalf("refresh token=%q", refreshToken)
	}
}

func TestManualCookieSaveDisablesAutomaticRefresh(t *testing.T) {
	fixture := newBiliAuthTestServer(t)
	a := testApp(t)
	a.bili = fixture.client()
	var userID int64
	if err := a.db.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	encryptedToken, _ := a.vault.Encrypt("old-refresh")
	_, _ = a.db.Exec(`INSERT INTO bili_refresh_tokens(user_id,refresh_token_enc,refreshed_at) VALUES(?,?,?)`, userID, encryptedToken, time.Now().Unix())
	request := httptest.NewRequest(http.MethodPut, "/api/settings/bilibili", bytes.NewBufferString(`{"cookie":"SESSDATA=manual; bili_jct=manual-csrf"}`))
	request = request.WithContext(context.WithValue(request.Context(), userContextKey, currentUser{ID: userID}))
	recorder := httptest.NewRecorder()
	a.saveBilibiliHandler(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("save status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM bili_refresh_tokens WHERE user_id=?`, userID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("refresh tokens=%d, %v", count, err)
	}
}

func TestCookieMergeRemovesExpiredValues(t *testing.T) {
	merged := mergeCookieHeader("a=1; SESSDATA=old", []*http.Cookie{{Name: "SESSDATA", Value: "new"}, {Name: "a", MaxAge: -1}})
	values, _ := parseCookieHeader(merged)
	if values["SESSDATA"] != "new" || values["a"] != "" {
		t.Fatalf("merged cookie=%q", merged)
	}
}

func TestRefreshFormsAreURLSafe(t *testing.T) {
	values := url.Values{"refresh_token": {"a+b/c="}}
	if !strings.Contains(values.Encode(), "a%2Bb%2Fc%3D") {
		t.Fatalf("unexpected form encoding: %s", values.Encode())
	}
}

func TestQRStatusJSONShape(t *testing.T) {
	encoded, err := json.Marshal(biliQRPollView{Status: "waiting", Message: "等待扫码"})
	if err != nil || string(encoded) != `{"status":"waiting","message":"等待扫码"}` {
		t.Fatalf("poll view=%s, %v", encoded, err)
	}
}
