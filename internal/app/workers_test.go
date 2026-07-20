package app

import (
	"context"
	"encoding/json"
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
	_, _ = a.db.Exec(`INSERT INTO deliveries(user_id,bvid,next_attempt_at,created_at) VALUES(?,'BV1',?,?)`, userID, time.Now().Unix(), time.Now().Unix())

	a.deliverOne(context.Background())
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
