package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"modernc.org/sqlite"
)

type subscriptionView struct {
	ID           int64  `json:"id"`
	Enabled      bool   `json:"enabled"`
	MID          string `json:"mid"`
	Name         string `json:"name"`
	Avatar       string `json:"avatar"`
	LatestBVID   string `json:"latestBvid"`
	LatestTitle  string `json:"latestTitle"`
	SubscribedAt int64  `json:"subscribedAt"`
	LastPolledAt *int64 `json:"lastPolledAt"`
	Error        string `json:"error"`
}

func (a *App) listSubscriptionsHandler(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	rows, err := a.db.Query(`SELECT s.id,s.enabled,c.mid,c.name,c.avatar,c.latest_bvid,c.latest_title,s.subscribed_at,p.last_polled_at,COALESCE(p.last_error,'') FROM subscriptions s JOIN creators c ON c.mid=s.creator_mid LEFT JOIN poll_states p ON p.creator_mid=c.mid WHERE s.user_id=? ORDER BY s.subscribed_at DESC`, u.ID)
	if err != nil {
		writeError(w, 500, "database", "无法读取订阅")
		return
	}
	defer rows.Close()
	items := []subscriptionView{}
	for rows.Next() {
		var item subscriptionView
		var last sql.NullInt64
		if rows.Scan(&item.ID, &item.Enabled, &item.MID, &item.Name, &item.Avatar, &item.LatestBVID, &item.LatestTitle, &item.SubscribedAt, &last, &item.Error) == nil {
			if last.Valid {
				item.LastPolledAt = &last.Int64
			}
			items = append(items, item)
		}
	}
	writeJSON(w, 200, items)
}

func (a *App) createSubscriptionHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Uploader string `json:"uploader"`
	}
	if decodeJSON(r, &input) != nil {
		writeError(w, 400, "invalid_request", "请输入 UP 主 UID 或空间链接")
		return
	}
	mid, err := parseMID(input.Uploader)
	if err != nil {
		writeError(w, 400, "invalid_mid", err.Error())
		return
	}
	u := userFrom(r)
	cookie, err := a.loadBiliCookie(u.ID)
	if err != nil {
		writeError(w, 400, "bilibili_missing", err.Error())
		return
	}
	creator, err := a.bili.GetCreator(r.Context(), mid, cookie)
	if err != nil {
		writeError(w, 502, "bilibili_failed", err.Error())
		return
	}
	videos, err := a.bili.GetLatestVideos(r.Context(), mid, cookie)
	if err != nil {
		writeError(w, 502, "bilibili_failed", err.Error())
		return
	}
	now := time.Now().Unix()
	latest := Video{}
	if len(videos) > 0 {
		latest = videos[len(videos)-1]
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, 500, "database", "无法创建订阅")
		return
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO creators(mid,name,avatar,latest_bvid,latest_title,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(mid) DO UPDATE SET name=excluded.name,avatar=excluded.avatar,latest_bvid=excluded.latest_bvid,latest_title=excluded.latest_title,updated_at=excluded.updated_at`, creator.MID, creator.Name, creator.Avatar, latest.BVID, latest.Title, now)
	if err != nil {
		writeError(w, 500, "database", "无法保存 UP 主")
		return
	}
	for _, video := range videos {
		_, _ = tx.Exec(`INSERT OR IGNORE INTO videos(bvid,creator_mid,title,url,published_at,detected_at) VALUES(?,?,?,?,?,?)`, video.BVID, mid, video.Title, video.URL, video.PublishedAt, now)
	}
	result, err := tx.Exec(`INSERT INTO subscriptions(user_id,creator_mid,baseline_bvid,subscribed_at) VALUES(?,?,?,?)`, u.ID, mid, latest.BVID, now)
	if err != nil {
		var sqliteErr *sqlite.Error
		if errors.As(err, &sqliteErr) && (sqliteErr.Code() == 1555 || sqliteErr.Code() == 2067) {
			writeError(w, 409, "duplicate_subscription", "已经订阅了这个 UP 主")
			return
		}
		writeError(w, 500, "database", "无法创建订阅")
		return
	}
	_, _ = tx.Exec(`INSERT OR IGNORE INTO poll_states(creator_mid,next_poll_at) VALUES(?,?)`, mid, now+60)
	if err = tx.Commit(); err != nil {
		writeError(w, 500, "database", "无法创建订阅")
		return
	}
	id, _ := result.LastInsertId()
	writeJSON(w, 201, map[string]any{"id": id, "creator": creator, "baseline": latest})
}

func subscriptionID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}
func (a *App) updateSubscriptionHandler(w http.ResponseWriter, r *http.Request) {
	id, err := subscriptionID(r)
	if err != nil {
		writeError(w, 400, "invalid_id", "订阅编号不正确")
		return
	}
	var input struct {
		Enabled bool `json:"enabled"`
	}
	if decodeJSON(r, &input) != nil {
		writeError(w, 400, "invalid_request", "请求格式不正确")
		return
	}
	u := userFrom(r)
	result, err := a.db.Exec(`UPDATE subscriptions SET enabled=? WHERE id=? AND user_id=?`, input.Enabled, id, u.ID)
	if err != nil {
		writeError(w, 500, "database", "无法更新订阅")
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		writeError(w, 404, "not_found", "订阅不存在")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) deleteSubscriptionHandler(w http.ResponseWriter, r *http.Request) {
	id, err := subscriptionID(r)
	if err != nil {
		writeError(w, 400, "invalid_id", "订阅编号不正确")
		return
	}
	u := userFrom(r)
	result, err := a.db.Exec(`DELETE FROM subscriptions WHERE id=? AND user_id=?`, id, u.ID)
	if err != nil {
		writeError(w, 500, "database", "无法删除订阅")
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		writeError(w, 404, "not_found", "订阅不存在")
		return
	}
	w.WriteHeader(204)
}

type deliveryView struct {
	ID            int64  `json:"id"`
	Status        string `json:"status"`
	Attempts      int    `json:"attempts"`
	Error         string `json:"error"`
	CreatedAt     int64  `json:"createdAt"`
	SentAt        *int64 `json:"sentAt"`
	BVID          string `json:"bvid"`
	VideoTitle    string `json:"videoTitle"`
	VideoURL      string `json:"videoUrl"`
	CreatorName   string `json:"creatorName"`
	CreatorAvatar string `json:"creatorAvatar"`
}

func (a *App) listDeliveriesHandler(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	rows, err := a.db.Query(`SELECT d.id,d.status,d.attempts,d.last_error,d.created_at,d.sent_at,v.bvid,v.title,v.url,c.name,c.avatar FROM deliveries d JOIN videos v ON v.bvid=d.bvid JOIN creators c ON c.mid=v.creator_mid WHERE d.user_id=? ORDER BY d.created_at DESC LIMIT 100`, u.ID)
	if err != nil {
		writeError(w, 500, "database", "无法读取通知记录")
		return
	}
	defer rows.Close()
	items := []deliveryView{}
	for rows.Next() {
		var item deliveryView
		var sent sql.NullInt64
		if rows.Scan(&item.ID, &item.Status, &item.Attempts, &item.Error, &item.CreatedAt, &sent, &item.BVID, &item.VideoTitle, &item.VideoURL, &item.CreatorName, &item.CreatorAvatar) == nil {
			if sent.Valid {
				item.SentAt = &sent.Int64
			}
			items = append(items, item)
		}
	}
	writeJSON(w, 200, items)
}

func (a *App) deletePendingDeliveryHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "通知编号不正确")
		return
	}
	u := userFrom(r)
	a.deliveryMu.Lock()
	defer a.deliveryMu.Unlock()
	result, err := a.db.Exec(`DELETE FROM deliveries WHERE id=? AND user_id=? AND status='pending'`, id, u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database", "无法取消等待发送的通知")
		return
	}
	count, _ := result.RowsAffected()
	if count > 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var status string
	err = a.db.QueryRow(`SELECT status FROM deliveries WHERE id=? AND user_id=?`, id, u.ID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "通知不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database", "无法读取通知状态")
		return
	}
	writeError(w, http.StatusConflict, "delivery_not_pending", "通知已不在等待发送状态")
}

func cleanError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 300 {
		return value[:300]
	}
	return value
}
