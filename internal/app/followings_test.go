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
	oldPublished := time.Now().Add(-time.Hour).Unix()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/x/web-interface/nav":
			if r.Header.Get("Cookie") == "SESSDATA=test" {
				fmt.Fprint(w, `{"code":0,"data":{"isLogin":true,"mid":123,"uname":"tester","wbi_img":{"img_url":"https://i/a1234567890123456789012345678901.png","sub_url":"https://i/b1234567890123456789012345678901.png"}}}`)
				return
			}
			if r.Header.Get("Cookie") != "" {
				t.Error("public query used an unexpected cookie")
			}
			fmt.Fprint(w, `{"code":-101,"message":"账号未登录","data":{"isLogin":false,"wbi_img":{"img_url":"https://i/a1234567890123456789012345678901.png","sub_url":"https://i/b1234567890123456789012345678901.png"}}}`)
		case "/x/relation/followings":
			if r.Header.Get("Cookie") != "SESSDATA=test" {
				t.Error("following query did not use the user's cookie")
			}
			fmt.Fprint(w, `{"code":0,"data":{"list":[{"mid":1,"uname":"Existing","face":"https://image/1.jpg"},{"mid":2,"uname":"New UP","face":"https://image/2.jpg"},{"mid":3,"uname":"Pending UP","face":"https://image/3.jpg"}],"total":3}}`)
		case "/x/space/wbi/arc/search":
			if r.Header.Get("Cookie") != "" {
				t.Error("public video query included the user's cookie")
			}
			if r.URL.Query().Get("mid") == "3" {
				fmt.Fprint(w, `{"code":-509,"message":"limited"}`)
				return
			}
			fmt.Fprintf(w, `{"code":0,"data":{"list":{"vlist":[{"bvid":"BV2latest","title":"Latest video","created":%d}]}}}`, oldPublished)
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
	if len(page.Items) != 3 || !page.Items[0].Subscribed || page.Items[1].Subscribed || page.Items[2].Subscribed {
		t.Fatalf("unexpected followings: %#v", page.Items)
	}

	body := bytes.NewBufferString(`{"page":1,"mids":["1","2","3"]}`)
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
	if result["imported"] != 2 || result["skipped"] != 1 || result["initialized"] != 1 || result["pending"] != 1 {
		t.Fatalf("unexpected import result: %#v", result)
	}
	var subscriptions, pollStates int
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM subscriptions WHERE user_id=?`, user.ID).Scan(&subscriptions)
	_ = a.db.QueryRow(`SELECT COUNT(*) FROM poll_states WHERE creator_mid IN ('2','3')`).Scan(&pollStates)
	if subscriptions != 3 || pollStates != 2 {
		t.Fatalf("subscriptions=%d pollStates=%d", subscriptions, pollStates)
	}
	var latestBVID, baseline string
	_ = a.db.QueryRow(`SELECT latest_bvid FROM creators WHERE mid='2'`).Scan(&latestBVID)
	_ = a.db.QueryRow(`SELECT baseline_bvid FROM subscriptions WHERE user_id=? AND creator_mid='2'`, user.ID).Scan(&baseline)
	if latestBVID != "BV2latest" || baseline != "BV2latest" {
		t.Fatalf("latest=%q baseline=%q", latestBVID, baseline)
	}
	var pendingNextPoll int64
	_ = a.db.QueryRow(`SELECT next_poll_at FROM poll_states WHERE creator_mid='3'`).Scan(&pendingNextPoll)
	if pendingNextPoll > time.Now().Unix() {
		t.Fatalf("pending next poll=%d, want immediate", pendingNextPoll)
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

func TestCreateSubscriptionWithoutBilibiliCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" {
			t.Error("public subscription query included a cookie")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/x/web-interface/card":
			fmt.Fprint(w, `{"code":0,"data":{"card":{"mid":"1","name":"Anonymous UP","face":"https://image/avatar.jpg"}}}`)
		case "/x/web-interface/nav":
			fmt.Fprint(w, `{"code":-101,"message":"账号未登录","data":{"isLogin":false,"wbi_img":{"img_url":"https://i/a1234567890123456789012345678901.png","sub_url":"https://i/b1234567890123456789012345678901.png"}}}`)
		case "/x/space/wbi/arc/search":
			fmt.Fprint(w, `{"code":0,"data":{"list":{"vlist":[{"bvid":"BV1","title":"Latest","created":200}]}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	a := testApp(t)
	a.bili = NewBilibiliClient(server.URL)
	handler := a.Routes()
	cookie, user := loginRequest(t, handler, "admin", "admin-password")
	request := httptest.NewRequest(http.MethodPost, "/api/subscriptions", bytes.NewBufferString(`{"uploader":"1"}`))
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", user.CSRFToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var name, baseline string
	if err := a.db.QueryRow(`SELECT c.name,s.baseline_bvid FROM subscriptions s JOIN creators c ON c.mid=s.creator_mid WHERE s.user_id=?`, user.ID).Scan(&name, &baseline); err != nil {
		t.Fatal(err)
	}
	if name != "Anonymous UP" || baseline != "BV1" {
		t.Fatalf("name=%q baseline=%q", name, baseline)
	}
}

func TestCreateSubscriptionDoesNotPersistWhenAnonymousValidationFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":-509,"message":"limited"}`)
	}))
	defer server.Close()

	a := testApp(t)
	a.bili = NewBilibiliClient(server.URL)
	handler := a.Routes()
	cookie, user := loginRequest(t, handler, "admin", "admin-password")
	request := httptest.NewRequest(http.MethodPost, "/api/subscriptions", bytes.NewBufferString(`{"uploader":"1"}`))
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", user.CSRFToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var subscriptions int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM subscriptions WHERE user_id=?`, user.ID).Scan(&subscriptions); err != nil {
		t.Fatal(err)
	}
	if subscriptions != 0 {
		t.Fatalf("subscriptions=%d, want 0", subscriptions)
	}
}
