package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func testApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{DataDir: dir, DatabasePath: filepath.Join(dir, "test.db"), EncryptionKey: []byte("01234567890123456789012345678901"), AdminUsername: "admin", AdminPassword: "admin-password", DefaultBarkServer: "https://api.day.app"}
	application, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { application.Close() })
	return application
}
func TestRecordVideosUsesSubscriptionBaseline(t *testing.T) {
	a := testApp(t)
	var userID int64
	if err := a.db.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	_, _ = a.db.Exec(`INSERT INTO creators(mid,name,updated_at) VALUES('1','UP',100)`)
	_, _ = a.db.Exec(`INSERT INTO subscriptions(user_id,creator_mid,baseline_bvid,subscribed_at) VALUES(?,?,?,?)`, userID, "1", "old", 100)
	newVideo := Video{BVID: "new", Title: "new video", URL: "https://example/new", PublishedAt: 101}
	oldVideo := Video{BVID: "older", Title: "old video", URL: "https://example/old", PublishedAt: 99}
	if err := a.recordVideos(context.Background(), "1", []Video{oldVideo, newVideo}); err != nil {
		t.Fatal(err)
	}
	if err := a.recordVideos(context.Background(), "1", []Video{oldVideo, newVideo}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM deliveries`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("deliveries=%d, want 1", count)
	}
}

func TestPollOneWorksWithoutBilibiliCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" {
			t.Error("anonymous poll included a cookie")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/x/web-interface/nav":
			fmt.Fprint(w, `{"code":-101,"message":"账号未登录","data":{"isLogin":false,"wbi_img":{"img_url":"https://i/a1234567890123456789012345678901.png","sub_url":"https://i/b1234567890123456789012345678901.png"}}}`)
		case "/x/space/wbi/arc/search":
			fmt.Fprint(w, `{"code":0,"data":{"list":{"vlist":[{"bvid":"new","title":"New video","created":200}]}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	a := testApp(t)
	a.bili = NewBilibiliClient(server.URL)
	userID := adminUserIDForTest(t, a)
	_, _ = a.db.Exec(`INSERT INTO creators(mid,name,latest_bvid,updated_at) VALUES('1','UP','old',100)`)
	_, _ = a.db.Exec(`INSERT INTO subscriptions(user_id,creator_mid,baseline_bvid,subscribed_at) VALUES(?,'1','old',100)`, userID)
	_, _ = a.db.Exec(`INSERT INTO poll_states(creator_mid,next_poll_at) VALUES('1',0)`)

	a.pollOne(context.Background())

	var deliveries int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM deliveries WHERE user_id=? AND bvid='new'`, userID).Scan(&deliveries); err != nil {
		t.Fatal(err)
	}
	var pollError string
	if err := a.db.QueryRow(`SELECT last_error FROM poll_states WHERE creator_mid='1'`).Scan(&pollError); err != nil {
		t.Fatal(err)
	}
	if deliveries != 1 || pollError != "" {
		t.Fatalf("deliveries=%d pollError=%q", deliveries, pollError)
	}
}

func TestPollOnePrefersCookieFromSubscribedUser(t *testing.T) {
	const preferredCookie = "SESSDATA=subscriber"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != preferredCookie {
			t.Errorf("poll cookie=%q, want subscribed user's cookie", r.Header.Get("Cookie"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/x/web-interface/nav":
			fmt.Fprint(w, `{"code":0,"data":{"isLogin":true,"wbi_img":{"img_url":"https://i/a1234567890123456789012345678901.png","sub_url":"https://i/b1234567890123456789012345678901.png"}}}`)
		case "/x/space/wbi/arc/search":
			fmt.Fprint(w, `{"code":0,"data":{"list":{"vlist":[]}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	a := testApp(t)
	a.bili = NewBilibiliClient(server.URL)
	userID := adminUserIDForTest(t, a)
	configureTestBiliCookie(t, a, userID, preferredCookie)
	unrelatedID := createMemberForDeleteTest(t, a, "unrelated")
	configureTestBiliCookie(t, a, unrelatedID, "SESSDATA=unrelated")
	_, _ = a.db.Exec(`UPDATE integrations SET bili_last_validated=200 WHERE user_id=?`, unrelatedID)
	_, _ = a.db.Exec(`UPDATE integrations SET bili_last_validated=100 WHERE user_id=?`, userID)
	_, _ = a.db.Exec(`INSERT INTO creators(mid,name,updated_at) VALUES('1','UP',100),('2','Other',100)`)
	_, _ = a.db.Exec(`INSERT INTO subscriptions(user_id,creator_mid,subscribed_at) VALUES(?,'1',100),(?,'2',100)`, userID, unrelatedID)
	_, _ = a.db.Exec(`INSERT INTO poll_states(creator_mid,next_poll_at) VALUES('1',0)`)

	a.pollOne(context.Background())

	var pollError string
	if err := a.db.QueryRow(`SELECT last_error FROM poll_states WHERE creator_mid='1'`).Scan(&pollError); err != nil {
		t.Fatal(err)
	}
	if pollError != "" {
		t.Fatalf("poll error=%q", pollError)
	}
}

func TestPollOneMarksExpiredCookieAndUsesNextSubscriber(t *testing.T) {
	requests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie := r.Header.Get("Cookie")
		requests[cookie]++
		w.Header().Set("Content-Type", "application/json")
		if cookie == "SESSDATA=expired" {
			fmt.Fprint(w, `{"code":-101,"message":"账号未登录"}`)
			return
		}
		if cookie != "SESSDATA=valid" {
			t.Errorf("unexpected poll cookie %q", cookie)
		}
		switch r.URL.Path {
		case "/x/web-interface/nav":
			fmt.Fprint(w, `{"code":0,"data":{"isLogin":true,"wbi_img":{"img_url":"https://i/a1234567890123456789012345678901.png","sub_url":"https://i/b1234567890123456789012345678901.png"}}}`)
		case "/x/space/wbi/arc/search":
			fmt.Fprint(w, `{"code":0,"data":{"list":{"vlist":[]}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	a := testApp(t)
	a.bili = NewBilibiliClient(server.URL)
	validID := adminUserIDForTest(t, a)
	expiredID := createMemberForDeleteTest(t, a, "expired")
	configureTestBiliCookie(t, a, validID, "SESSDATA=valid")
	configureTestBiliCookie(t, a, expiredID, "SESSDATA=expired")
	_, _ = a.db.Exec(`UPDATE integrations SET bili_last_validated=100 WHERE user_id=?`, validID)
	_, _ = a.db.Exec(`UPDATE integrations SET bili_last_validated=200 WHERE user_id=?`, expiredID)
	_, _ = a.db.Exec(`INSERT INTO creators(mid,name,updated_at) VALUES('1','UP',100)`)
	_, _ = a.db.Exec(`INSERT INTO subscriptions(user_id,creator_mid,subscribed_at) VALUES(?,'1',100),(?,'1',100)`, validID, expiredID)
	_, _ = a.db.Exec(`INSERT INTO poll_states(creator_mid,next_poll_at) VALUES('1',0)`)

	a.pollOne(context.Background())

	var status string
	if err := a.db.QueryRow(`SELECT bili_status FROM integrations WHERE user_id=?`, expiredID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "invalid" || requests["SESSDATA=expired"] != 1 || requests["SESSDATA=valid"] != 2 {
		t.Fatalf("expired status=%q requests=%v", status, requests)
	}
}

func TestPollOneDoesNotRotateCookiesAfterRiskControl(t *testing.T) {
	requests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie := r.Header.Get("Cookie")
		requests[cookie]++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"code":-352,"message":"风控校验失败"}`)
	}))
	defer server.Close()

	a := testApp(t)
	a.bili = NewBilibiliClient(server.URL)
	primaryID := adminUserIDForTest(t, a)
	secondaryID := createMemberForDeleteTest(t, a, "secondary")
	configureTestBiliCookie(t, a, primaryID, "SESSDATA=primary")
	configureTestBiliCookie(t, a, secondaryID, "SESSDATA=secondary")
	_, _ = a.db.Exec(`UPDATE integrations SET bili_last_validated=200 WHERE user_id=?`, primaryID)
	_, _ = a.db.Exec(`UPDATE integrations SET bili_last_validated=100 WHERE user_id=?`, secondaryID)
	_, _ = a.db.Exec(`INSERT INTO creators(mid,name,updated_at) VALUES('1','UP',100)`)
	_, _ = a.db.Exec(`INSERT INTO subscriptions(user_id,creator_mid,subscribed_at) VALUES(?,'1',100),(?,'1',100)`, primaryID, secondaryID)
	_, _ = a.db.Exec(`INSERT INTO poll_states(creator_mid,next_poll_at) VALUES('1',0)`)

	a.pollOne(context.Background())

	var pollError string
	var failures int
	if err := a.db.QueryRow(`SELECT last_error,failure_count FROM poll_states WHERE creator_mid='1'`).Scan(&pollError, &failures); err != nil {
		t.Fatal(err)
	}
	if failures != 1 || pollError == "" || requests["SESSDATA=primary"] != 1 || requests["SESSDATA=secondary"] != 0 || requests[""] != 0 {
		t.Fatalf("failures=%d error=%q requests=%v", failures, pollError, requests)
	}
}

func TestDeliveryUsesVideoURL(t *testing.T) {
	var received BarkMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "message": "success"})
	}))
	defer server.Close()

	a := testApp(t)
	var userID int64
	if err := a.db.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	encryptedKey, _ := a.vault.Encrypt("test-device-key")
	_, _ = a.db.Exec(`UPDATE integrations SET bark_server=?,bark_key_enc=? WHERE user_id=?`, server.URL, encryptedKey, userID)
	_, _ = a.db.Exec(`INSERT INTO creators(mid,name,updated_at) VALUES('546195','UP',?)`, time.Now().Unix())
	_, _ = a.db.Exec(`INSERT INTO videos(bvid,creator_mid,title,url,published_at,detected_at) VALUES('BV1','546195','New video','https://example/video',?,?)`, time.Now().Unix(), time.Now().Unix())
	_, _ = a.db.Exec(`INSERT INTO deliveries(user_id,bvid,next_attempt_at,created_at) VALUES(?,'BV1',0,?)`, userID, time.Now().Unix())

	a.deliverOneAt(context.Background(), shanghaiTime(10, 0))
	if received.URL != "https://example/video" {
		t.Fatalf("Bark URL=%q", received.URL)
	}
	var status string
	var attempts int
	if err := a.db.QueryRow(`SELECT status,attempts FROM deliveries WHERE bvid='BV1'`).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "sent" || attempts != 1 {
		t.Fatalf("delivery=(%q,%d), want (sent,1)", status, attempts)
	}
}

func TestDeliveryDefersDuringSleepWithoutConsumingAttempt(t *testing.T) {
	var received []BarkMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var message BarkMessage
		if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
			t.Error(err)
		}
		received = append(received, message)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "message": "success"})
	}))
	defer server.Close()

	a := testApp(t)
	userID := adminUserIDForTest(t, a)
	encryptedKey, _ := a.vault.Encrypt("test-device-key")
	_, _ = a.db.Exec(`UPDATE integrations SET bark_server=?,bark_key_enc=?,bark_level='critical',bark_sound='alarm' WHERE user_id=?`, server.URL, encryptedKey, userID)
	_, _ = a.db.Exec(`INSERT INTO creators(mid,name,updated_at) VALUES('sleep-up','Sleep UP',100)`)
	_, _ = a.db.Exec(`INSERT INTO videos(bvid,creator_mid,title,url,published_at,detected_at) VALUES('SLEEP-BV','sleep-up','Sleep video','https://example/sleep',100,100)`)
	_, _ = a.db.Exec(`INSERT INTO deliveries(user_id,bvid,next_attempt_at,created_at) VALUES(?,'SLEEP-BV',0,100)`, userID)

	a.deliverOneAt(context.Background(), shanghaiTime(1, 0))
	var status, lastError string
	var attempts int
	var deferredUntil int64
	if err := a.db.QueryRow(`SELECT status,attempts,last_error,deferred_until FROM deliveries WHERE bvid='SLEEP-BV'`).Scan(&status, &attempts, &lastError, &deferredUntil); err != nil {
		t.Fatal(err)
	}
	wantUntil := shanghaiTime(8, 0).Unix()
	if len(received) != 0 || status != "pending" || attempts != 0 || lastError != "" || deferredUntil != wantUntil {
		t.Fatalf("during sleep received=%d delivery=(%s,%d,%q,%d), want pending until %d", len(received), status, attempts, lastError, deferredUntil, wantUntil)
	}

	a.deliverOneAt(context.Background(), shanghaiTime(8, 0))
	if len(received) != 1 || received[0].URL != "https://example/sleep" || received[0].Level != "critical" || received[0].Sound != "alarm" {
		t.Fatalf("received after sleep=%+v", received)
	}
	if err := a.db.QueryRow(`SELECT status,attempts FROM deliveries WHERE bvid='SLEEP-BV'`).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "sent" || attempts != 1 {
		t.Fatalf("delivery after sleep=(%s,%d), want (sent,1)", status, attempts)
	}
}

func TestDeliveryDefersDuringUserQuietWindow(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "message": "success"})
	}))
	defer server.Close()

	a := testApp(t)
	userID := adminUserIDForTest(t, a)
	encryptedKey, _ := a.vault.Encrypt("test-device-key")
	_, _ = a.db.Exec(`UPDATE integrations SET bark_server=?,bark_key_enc=?,bark_quiet_enabled=1,bark_quiet_start='12:00',bark_quiet_end='14:00' WHERE user_id=?`, server.URL, encryptedKey, userID)
	_, _ = a.db.Exec(`INSERT INTO creators(mid,name,updated_at) VALUES('quiet-up','Quiet UP',100)`)
	_, _ = a.db.Exec(`INSERT INTO videos(bvid,creator_mid,title,url,published_at,detected_at) VALUES('QUIET-BV','quiet-up','Quiet video','https://example/quiet',100,100)`)
	_, _ = a.db.Exec(`INSERT INTO deliveries(user_id,bvid,next_attempt_at,created_at) VALUES(?,'QUIET-BV',0,100)`, userID)

	a.deliverOneAt(context.Background(), shanghaiTime(12, 30))
	var deferredUntil int64
	_ = a.db.QueryRow(`SELECT deferred_until FROM deliveries WHERE bvid='QUIET-BV'`).Scan(&deferredUntil)
	if requests != 0 || deferredUntil != shanghaiTime(14, 0).Unix() {
		t.Fatalf("quiet requests=%d deferred_until=%d", requests, deferredUntil)
	}
	a.deliverOneAt(context.Background(), shanghaiTime(14, 0))
	if requests != 1 {
		t.Fatalf("requests after quiet=%d, want 1", requests)
	}
}

func TestStartupRepairsBarkSuccessDeliveries(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{DataDir: dir, DatabasePath: filepath.Join(dir, "test.db"), EncryptionKey: []byte("01234567890123456789012345678901"), AdminUsername: "admin", AdminPassword: "admin-password", DefaultBarkServer: "https://api.day.app"}
	a, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	var userID int64
	if err := a.db.QueryRow(`SELECT id FROM users WHERE username='admin'`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	_, _ = a.db.Exec(`INSERT INTO creators(mid,name,updated_at) VALUES('1','UP',100)`)
	_, _ = a.db.Exec(`INSERT INTO videos(bvid,creator_mid,title,url,published_at,detected_at) VALUES('BV1','1','Video','https://example/video',100,100)`)
	_, _ = a.db.Exec(`INSERT INTO deliveries(user_id,bvid,status,attempts,next_attempt_at,last_error,created_at) VALUES(?,'BV1','pending',5,9999999999,'success',100)`, userID)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	a, err = New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var status, lastError string
	var sentAt int64
	if err := a.db.QueryRow(`SELECT status,last_error,sent_at FROM deliveries WHERE bvid='BV1'`).Scan(&status, &lastError, &sentAt); err != nil {
		t.Fatal(err)
	}
	if status != "sent" || lastError != "" || sentAt != 100 {
		t.Fatalf("delivery=(%q,%q,%d), want (sent,empty,100)", status, lastError, sentAt)
	}
}

func TestStartupMigratesDeliveryDeferredUntil(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{DataDir: dir, DatabasePath: filepath.Join(dir, "test.db"), EncryptionKey: []byte("01234567890123456789012345678901"), AdminUsername: "admin", AdminPassword: "admin-password", DefaultBarkServer: "https://api.day.app"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a, err := New(cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", cfg.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`ALTER TABLE deliveries DROP COLUMN deferred_until`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	a, err = New(cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	exists, err := tableHasColumn(a.db, "deliveries", "deferred_until")
	if err != nil || !exists {
		t.Fatalf("deferred_until exists=%v, err=%v", exists, err)
	}
}

func TestStartupRequeuesUninitializedFollowingImports(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{DataDir: dir, DatabasePath: filepath.Join(dir, "test.db"), EncryptionKey: []byte("01234567890123456789012345678901"), AdminUsername: "admin", AdminPassword: "admin-password", DefaultBarkServer: "https://api.day.app"}
	a, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = a.db.Exec(`INSERT INTO creators(mid,name,updated_at) VALUES('pending','Pending',100),('checked','Checked',100),('failed','Failed',100)`)
	_, _ = a.db.Exec(`INSERT INTO poll_states(creator_mid,last_polled_at,next_poll_at,last_error) VALUES('pending',NULL,9999999999,''),('checked',100,9999999999,''),('failed',NULL,9999999999,'limited')`)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	a, err = New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for mid, want := range map[string]int64{"pending": 0, "checked": 9999999999, "failed": 9999999999} {
		var nextPoll int64
		if err := a.db.QueryRow(`SELECT next_poll_at FROM poll_states WHERE creator_mid=?`, mid).Scan(&nextPoll); err != nil {
			t.Fatal(err)
		}
		if nextPoll != want {
			t.Fatalf("%s next_poll_at=%d, want %d", mid, nextPoll, want)
		}
	}
}
