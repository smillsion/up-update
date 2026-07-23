package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListSubscriptionsIncludesLatestPublishedAt(t *testing.T) {
	a := testApp(t)
	handler := a.Routes()
	cookie, user := loginRequest(t, handler, "admin", "admin-password")
	_, _ = a.db.Exec(`INSERT INTO creators(mid,name,latest_bvid,latest_title,updated_at) VALUES('with-time','With Time','TIMED-BV','Timed video',100),('without-time','Without Time','LEGACY-BV','Legacy video',100)`)
	_, _ = a.db.Exec(`INSERT INTO videos(bvid,creator_mid,title,url,published_at,detected_at) VALUES('TIMED-BV','with-time','Timed video','https://example/timed',1753241220,100)`)
	_, _ = a.db.Exec(`INSERT INTO subscriptions(user_id,creator_mid,subscribed_at) VALUES(?,'with-time',200),(?,'without-time',100)`, user.ID, user.ID)

	request := httptest.NewRequest(http.MethodGet, "/api/subscriptions", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var items []subscriptionView
	if err := json.NewDecoder(response.Body).Decode(&items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].LatestPublishedAt == nil || *items[0].LatestPublishedAt != 1753241220 {
		t.Fatalf("items=%+v", items)
	}
	if items[1].LatestPublishedAt != nil {
		t.Fatalf("legacy latestPublishedAt=%v, want nil", *items[1].LatestPublishedAt)
	}
}
