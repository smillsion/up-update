package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestListDeliveriesIsPaginatedAndIsolated(t *testing.T) {
	a := testApp(t)
	handler := a.Routes()
	cookie, user := loginRequest(t, handler, "admin", "admin-password")
	_, _ = a.db.Exec(`INSERT INTO creators(mid,name,updated_at) VALUES('1','UP',100)`)
	for index := 1; index <= 25; index++ {
		bvid := fmt.Sprintf("BV%02d", index)
		_, _ = a.db.Exec(`INSERT INTO videos(bvid,creator_mid,title,url,published_at,detected_at) VALUES(?,'1',?,?,?,?)`, bvid, "Video "+bvid, "https://example/"+bvid, index, index)
		_, _ = a.db.Exec(`INSERT INTO deliveries(user_id,bvid,status,next_attempt_at,created_at) VALUES(?,?,'sent',0,?)`, user.ID, bvid, index)
	}
	hash, _ := hashPassword("member-password")
	member, _ := a.db.Exec(`INSERT INTO users(username,display_name,password_hash,created_at) VALUES('member','Member',?,100)`, hash)
	memberID, _ := member.LastInsertId()
	_, _ = a.db.Exec(`INSERT INTO videos(bvid,creator_mid,title,url,published_at,detected_at) VALUES('OTHER','1','Other','https://example/other',100,100)`)
	_, _ = a.db.Exec(`INSERT INTO deliveries(user_id,bvid,status,next_attempt_at,created_at) VALUES(?,'OTHER','sent',0,100)`, memberID)

	request := httptest.NewRequest(http.MethodGet, "/api/deliveries?page=2", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
	var page deliveryPageView
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 25 || page.TotalPages != 2 || page.PageSize != 20 || len(page.Items) != 5 {
		t.Fatalf("unexpected page: %#v", page)
	}
	if page.Items[0].BVID != "BV05" || page.Items[4].BVID != "BV01" {
		t.Fatalf("unexpected order: %#v", page.Items)
	}
}

func TestDeletePendingDelivery(t *testing.T) {
	a := testApp(t)
	handler := a.Routes()
	cookie, user := loginRequest(t, handler, "admin", "admin-password")
	_, _ = a.db.Exec(`INSERT INTO creators(mid,name,updated_at) VALUES('1','UP',100)`)
	_, _ = a.db.Exec(`INSERT INTO videos(bvid,creator_mid,title,url,published_at,detected_at) VALUES('BV1','1','Video','https://example/video',100,100)`)
	result, err := a.db.Exec(`INSERT INTO deliveries(user_id,bvid,next_attempt_at,created_at) VALUES(?,'BV1',?,?)`, user.ID, time.Now().Add(time.Hour).Unix(), time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()

	request := httptest.NewRequest(http.MethodDelete, "/api/deliveries/"+strconv.FormatInt(id, 10), nil)
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", user.CSRFToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", response.Code, response.Body.String())
	}
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM deliveries WHERE id=?`, id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("delivery still exists after cancellation")
	}
}

func TestDeleteSentDeliveryIsRejected(t *testing.T) {
	a := testApp(t)
	handler := a.Routes()
	cookie, user := loginRequest(t, handler, "admin", "admin-password")
	_, _ = a.db.Exec(`INSERT INTO creators(mid,name,updated_at) VALUES('1','UP',100)`)
	_, _ = a.db.Exec(`INSERT INTO videos(bvid,creator_mid,title,url,published_at,detected_at) VALUES('BV1','1','Video','https://example/video',100,100)`)
	result, err := a.db.Exec(`INSERT INTO deliveries(user_id,bvid,status,next_attempt_at,created_at) VALUES(?,'BV1','sent',0,100)`, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()

	request := httptest.NewRequest(http.MethodDelete, "/api/deliveries/"+strconv.FormatInt(id, 10), nil)
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", user.CSRFToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("delete sent status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDeleteDeliveryDoesNotAffectOtherUser(t *testing.T) {
	a := testApp(t)
	handler := a.Routes()
	cookie, admin := loginRequest(t, handler, "admin", "admin-password")
	hash, _ := hashPassword("member-password")
	member, err := a.db.Exec(`INSERT INTO users(username,display_name,password_hash,created_at) VALUES('member','Member',?,100)`, hash)
	if err != nil {
		t.Fatal(err)
	}
	memberID, _ := member.LastInsertId()
	_, _ = a.db.Exec(`INSERT INTO creators(mid,name,updated_at) VALUES('1','UP',100)`)
	_, _ = a.db.Exec(`INSERT INTO videos(bvid,creator_mid,title,url,published_at,detected_at) VALUES('BV1','1','Video','https://example/video',100,100)`)
	result, err := a.db.Exec(`INSERT INTO deliveries(user_id,bvid,next_attempt_at,created_at) VALUES(?,'BV1',9999999999,100)`, memberID)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()

	request := httptest.NewRequest(http.MethodDelete, "/api/deliveries/"+strconv.FormatInt(id, 10), nil)
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", admin.CSRFToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("delete other user's delivery status=%d body=%s", response.Code, response.Body.String())
	}
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM deliveries WHERE id=?`, id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("other user's delivery was deleted")
	}
}
