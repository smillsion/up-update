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

func TestBarkSpaceURL(t *testing.T) {
	if got := barkSpaceURL("546195"); got != "bilibili://space/546195" {
		t.Fatalf("barkSpaceURL=%q", got)
	}
}

func TestDeliveryUsesBilibiliSpaceDeepLink(t *testing.T) {
	var received BarkMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
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
	if received.URL != "bilibili://space/546195" {
		t.Fatalf("Bark URL=%q", received.URL)
	}
}
