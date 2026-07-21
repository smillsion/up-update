package app

import (
	"context"
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

const (
	deliveryPageSize   = 20
	followingPageSize  = 50
	maxFollowingImport = 20
	followingFetchers  = 3
	followingFetchTime = 20 * time.Second
)

func requestPage(r *http.Request) (int, error) {
	value := strings.TrimSpace(r.URL.Query().Get("page"))
	if value == "" {
		return 1, nil
	}
	page, err := strconv.Atoi(value)
	if err != nil || page < 1 {
		return 0, errors.New("页码必须是正整数")
	}
	return page, nil
}

func pageCount(total, pageSize int) int {
	if total == 0 {
		return 0
	}
	return (total + pageSize - 1) / pageSize
}

func writeBiliCookieError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errBiliCookieMissing):
		writeError(w, http.StatusBadRequest, "bilibili_missing", err.Error())
	case errors.Is(err, errBiliCookieInvalid):
		writeError(w, http.StatusBadRequest, "bilibili_invalid", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "bilibili_cookie", "无法读取 B 站 Cookie")
	}
}

func (a *App) recordBiliAuthError(userID int64, err error) {
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || !providerErr.Auth {
		return
	}
	now := time.Now().Unix()
	_, _ = a.db.Exec(`UPDATE integrations SET bili_status='invalid',bili_error=?,bili_last_validated=?,updated_at=? WHERE user_id=?`, cleanError(err.Error()), now, now, userID)
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
		writeBiliCookieError(w, err)
		return
	}
	creator, err := a.bili.GetCreator(r.Context(), mid, cookie)
	if err != nil {
		a.recordBiliAuthError(u.ID, err)
		writeError(w, 502, "bilibili_failed", err.Error())
		return
	}
	videos, err := a.bili.GetLatestVideos(r.Context(), mid, cookie)
	if err != nil {
		a.recordBiliAuthError(u.ID, err)
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

type deliveryPageView struct {
	Items      []deliveryView `json:"items"`
	Page       int            `json:"page"`
	PageSize   int            `json:"pageSize"`
	Total      int            `json:"total"`
	TotalPages int            `json:"totalPages"`
}

func (a *App) listDeliveriesHandler(w http.ResponseWriter, r *http.Request) {
	page, err := requestPage(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_page", err.Error())
		return
	}
	u := userFrom(r)
	var total int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM deliveries WHERE user_id=?`, u.ID).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "database", "无法读取通知总数")
		return
	}
	rows, err := a.db.Query(`SELECT d.id,d.status,d.attempts,d.last_error,d.created_at,d.sent_at,v.bvid,v.title,v.url,c.name,c.avatar FROM deliveries d JOIN videos v ON v.bvid=d.bvid JOIN creators c ON c.mid=v.creator_mid WHERE d.user_id=? ORDER BY d.created_at DESC,d.id DESC LIMIT ? OFFSET ?`, u.ID, deliveryPageSize, (page-1)*deliveryPageSize)
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
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "database", "无法读取通知记录")
		return
	}
	writeJSON(w, http.StatusOK, deliveryPageView{Items: items, Page: page, PageSize: deliveryPageSize, Total: total, TotalPages: pageCount(total, deliveryPageSize)})
}

type followingView struct {
	MID        string `json:"mid"`
	Name       string `json:"name"`
	Avatar     string `json:"avatar"`
	Subscribed bool   `json:"subscribed"`
}

type followingPageView struct {
	Items      []followingView `json:"items"`
	Page       int             `json:"page"`
	PageSize   int             `json:"pageSize"`
	Total      int             `json:"total"`
	TotalPages int             `json:"totalPages"`
}

func (a *App) listFollowingsHandler(w http.ResponseWriter, r *http.Request) {
	page, err := requestPage(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_page", err.Error())
		return
	}
	u := userFrom(r)
	cookie, err := a.loadBiliCookie(u.ID)
	if err != nil {
		writeBiliCookieError(w, err)
		return
	}
	followings, err := a.bili.GetFollowings(r.Context(), cookie, page, followingPageSize)
	if err != nil {
		a.recordBiliAuthError(u.ID, err)
		writeError(w, http.StatusBadGateway, "bilibili_failed", err.Error())
		return
	}
	subscribed := map[string]bool{}
	rows, err := a.db.Query(`SELECT creator_mid FROM subscriptions WHERE user_id=?`, u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database", "无法读取现有订阅")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var mid string
		if err := rows.Scan(&mid); err != nil {
			writeError(w, http.StatusInternalServerError, "database", "无法读取现有订阅")
			return
		}
		subscribed[mid] = true
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "database", "无法读取现有订阅")
		return
	}
	items := make([]followingView, 0, len(followings.Items))
	for _, item := range followings.Items {
		items = append(items, followingView{MID: item.MID, Name: item.Name, Avatar: item.Avatar, Subscribed: subscribed[item.MID]})
	}
	writeJSON(w, http.StatusOK, followingPageView{Items: items, Page: page, PageSize: followingPageSize, Total: followings.Total, TotalPages: pageCount(followings.Total, followingPageSize)})
}

func (a *App) importFollowingsHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Page int      `json:"page"`
		MIDs []string `json:"mids"`
	}
	if decodeJSON(r, &input) != nil || input.Page < 1 {
		writeError(w, http.StatusBadRequest, "invalid_request", "关注列表导入参数不正确")
		return
	}
	selected := make([]string, 0, len(input.MIDs))
	seen := map[string]bool{}
	for _, value := range input.MIDs {
		mid, err := parseMID(value)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_mid", err.Error())
			return
		}
		if !seen[mid] {
			seen[mid] = true
			selected = append(selected, mid)
		}
	}
	if len(selected) == 0 || len(selected) > maxFollowingImport {
		writeError(w, http.StatusBadRequest, "invalid_selection", "每次请选择 1 到 20 个关注账号")
		return
	}
	u := userFrom(r)
	cookie, err := a.loadBiliCookie(u.ID)
	if err != nil {
		writeBiliCookieError(w, err)
		return
	}
	followings, err := a.bili.GetFollowings(r.Context(), cookie, input.Page, followingPageSize)
	if err != nil {
		a.recordBiliAuthError(u.ID, err)
		writeError(w, http.StatusBadGateway, "bilibili_failed", err.Error())
		return
	}
	available := make(map[string]Following, len(followings.Items))
	for _, item := range followings.Items {
		available[item.MID] = item
	}
	for _, mid := range selected {
		if _, ok := available[mid]; !ok {
			writeError(w, http.StatusConflict, "following_page_changed", "关注列表已发生变化，请刷新后重新选择")
			return
		}
	}
	existing := map[string]bool{}
	rows, err := a.db.Query(`SELECT creator_mid FROM subscriptions WHERE user_id=?`, u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database", "无法读取现有订阅")
		return
	}
	for rows.Next() {
		var mid string
		if err := rows.Scan(&mid); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "database", "无法读取现有订阅")
			return
		}
		existing[mid] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		writeError(w, http.StatusInternalServerError, "database", "无法读取现有订阅")
		return
	}
	rows.Close()
	toImport := make([]Following, 0, len(selected))
	mids := make([]string, 0, len(selected))
	for _, mid := range selected {
		if existing[mid] {
			continue
		}
		toImport = append(toImport, available[mid])
		mids = append(mids, mid)
	}
	if len(toImport) == 0 {
		writeJSON(w, http.StatusOK, map[string]int{"imported": 0, "skipped": len(selected), "initialized": 0, "pending": 0})
		return
	}
	fetchContext, cancel := context.WithTimeout(r.Context(), followingFetchTime)
	fetched := a.bili.GetLatestVideosBatch(fetchContext, mids, cookie, followingFetchers)
	cancel()
	fetchByMID := make(map[string]VideoFetchResult, len(fetched))
	for _, result := range fetched {
		fetchByMID[result.MID] = result
		if result.Err != nil {
			a.recordBiliAuthError(u.ID, result.Err)
		}
	}
	interval := a.currentPollIntervalSeconds(time.Now())
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database", "无法导入关注列表")
		return
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	importedMIDs := map[string]bool{}
	for _, item := range toImport {
		result := fetchByMID[item.MID]
		latest := Video{}
		if result.Err == nil && len(result.Videos) > 0 {
			latest = result.Videos[len(result.Videos)-1]
		}
		if result.Err == nil {
			_, err = tx.Exec(`INSERT INTO creators(mid,name,avatar,latest_bvid,latest_title,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(mid) DO UPDATE SET name=excluded.name,avatar=excluded.avatar,latest_bvid=excluded.latest_bvid,latest_title=excluded.latest_title,updated_at=excluded.updated_at`, item.MID, item.Name, item.Avatar, latest.BVID, latest.Title, now)
		} else {
			_, err = tx.Exec(`INSERT INTO creators(mid,name,avatar,updated_at) VALUES(?,?,?,?) ON CONFLICT(mid) DO UPDATE SET name=excluded.name,avatar=excluded.avatar,updated_at=excluded.updated_at`, item.MID, item.Name, item.Avatar, now)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database", "无法保存关注账号")
			return
		}
		insert, err := tx.Exec(`INSERT OR IGNORE INTO subscriptions(user_id,creator_mid,baseline_bvid,subscribed_at) VALUES(?,?,?,?)`, u.ID, item.MID, latest.BVID, now)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "database", "无法创建订阅")
			return
		}
		count, _ := insert.RowsAffected()
		if count > 0 {
			importedMIDs[item.MID] = true
		}
		nextPoll := now
		if result.Err == nil {
			nextPoll = now + int64(interval)
		}
		if _, err := tx.Exec(`INSERT INTO poll_states(creator_mid,next_poll_at) VALUES(?,?) ON CONFLICT(creator_mid) DO UPDATE SET next_poll_at=MIN(poll_states.next_poll_at,excluded.next_poll_at)`, item.MID, nextPoll); err != nil {
			writeError(w, http.StatusInternalServerError, "database", "无法创建轮询状态")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "database", "无法导入关注列表")
		return
	}
	initialized := 0
	pending := 0
	for _, result := range fetched {
		if !importedMIDs[result.MID] {
			continue
		}
		if result.Err != nil {
			pending++
			continue
		}
		if err := a.recordVideos(r.Context(), result.MID, result.Videos); err != nil {
			a.logger.Error("record imported videos failed", "mid", result.MID, "error", err)
			_, _ = a.db.Exec(`UPDATE poll_states SET next_poll_at=? WHERE creator_mid=?`, time.Now().Unix(), result.MID)
			pending++
			continue
		}
		checkedAt := time.Now().Unix()
		_, _ = a.db.Exec(`UPDATE poll_states SET last_polled_at=?,next_poll_at=?,failure_count=0,last_error='' WHERE creator_mid=?`, checkedAt, checkedAt+int64(interval), result.MID)
		initialized++
	}
	imported := len(importedMIDs)
	writeJSON(w, http.StatusOK, map[string]int{"imported": imported, "skipped": len(selected) - imported, "initialized": initialized, "pending": pending})
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
