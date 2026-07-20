package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func configureTestBiliCookie(t *testing.T, a *App, userID int64, cookie string) {
	t.Helper()
	encrypted, err := a.vault.Encrypt(cookie)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.db.Exec(`UPDATE integrations SET bili_cookie_enc=?,bili_status='valid' WHERE user_id=?`, encrypted, userID)
	if err != nil {
		t.Fatal(err)
	}
}

func TestFollowingsCanBeListedAndImported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "SESSDATA=test" {
			t.Error("cookie missing")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/x/web-interface/nav":
			fmt.Fprint(w, `{"code":0,"data":{"isLogin":true,"mid":123,"uname":"tester"}}`)
		case "/x/relation/followings":
			fmt.Fprint(w, `{"code":0,"data":{"list":[{"mid":1,"uname":"Existing","face":"https://image/1.jpg"},{"mid":2,"uname":"New UP","face":"https://image/2.jpg"}],"total":2}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	a := testApp(t)
	a.bili = NewBilibiliClient(server.URL)
	handler := a.Routes()
	cookie, user := loginRequest(t, handler, "admin", "admin-password")
	configureTestBiliCookie(t, a, user.ID, "SESSDATA=test")
	_, _ = a.db.Exec(`INSERT INTO creators(mid,name,updated_at) VALUES('1','Existing',100)`)
	_, _ = a.db.Exec(`INSERT INTO subscriptions(user_id,creator_mid,subscribed_at) VALUES(?,'1',100)`, user.ID)

	listRequest := httptest.NewRequest(http.MethodGet, "/api/subscriptions/followings?page=1", nil)
	listRequest.AddCookie(cookie)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	var page followingPageView
	if err := json.Unmarshal(listResponse.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || !page.Items[0].Subscribed || page.Items[1].Subscribed {
		t.Fatalf("unexpected followings: %#v", page.Items)
	}

	body := bytes.NewBufferString(`{"page":1,"mids":["1","2"]}`)
	importRequest := httptest.NewRequest(http.MethodPost, "/api/subscriptions/import-followings", body)
	importRequest.AddCookie(cookie)
	importRequest.Header.Set("X-CSRF-Token", user.CSRFToken)
	importResponse := httptest.NewRecorder()
	handler.ServeHTTP(importResponse, importRequest)
	if importResponse.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", importResponse.Code, importResponse.Body.String())
	}
	var result map[string]int
	if err := json.Unmarshal(importResponse.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["imported"] != 1 || result["skipped"] != 1 {
		t.Fatalf("unexpected import result: %#v", result)
	}
	var subscriptions, pollStates int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM subscriptions WHERE user_id=?`, user.ID).Scan(&subscriptions)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM poll_states WHERE creator_mid='2'`).Scan(&pollStates)
	if subscriptions != 2 || pollStates != 1 {
		t.Fatalf("subscriptions=%d pollStates=%d", subscriptions, pollStates)
	}
	staleRequest := httptest.NewRequest(http.MethodPost, "/api/subscriptions/import-followings", bytes.NewBufferString(`{"page":1,"mids":["999"]}`))
	staleRequest.AddCookie(cookie)
	staleRequest.Header.Set("X-CSRF-Token", user.CSRFToken)
	staleResponse := httptest.NewRecorder()
	handler.ServeHTTP(staleResponse, staleRequest)
	if staleResponse.Code != http.StatusConflict {
		t.Fatalf("stale import status=%d body=%s", staleResponse.Code, staleResponse.Body.String())
	}
	oldVideo := Video{BVID: "old", Title: "Old", URL: "https://example/old", PublishedAt: time.Now().Add(-time.Hour).Unix()}
	if err := a.recordVideos(context.Background(), "2", []Video{oldVideo}); err != nil {
		t.Fatal(err)
	}
	var deliveries int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM deliveries WHERE user_id=?`, user.ID).Scan(&deliveries)
	if deliveries != 0 {
		t.Fatalf("import created %d historical deliveries", deliveries)
	}
}

func TestCreateSubscriptionWithoutCookieHasFriendlyError(t *testing.T) {
	a := testApp(t)
	handler := a.Routes()
	cookie, user := loginRequest(t, handler, "admin", "admin-password")
	request := httptest.NewRequest(http.MethodPost, "/api/subscriptions", bytes.NewBufferString(`{"uploader":"1"}`))
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", user.CSRFToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !bytes.Contains(response.Body.Bytes(), []byte(`"code":"bilibili_missing"`)) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
