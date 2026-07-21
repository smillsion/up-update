package app

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,32}$`)

type adminUserView struct {
	ID                  int64  `json:"id"`
	Username            string `json:"username"`
	DisplayName         string `json:"displayName"`
	Role                string `json:"role"`
	Enabled             bool   `json:"enabled"`
	ForcePasswordChange bool   `json:"forcePasswordChange"`
	CreatedAt           int64  `json:"createdAt"`
	BilibiliStatus      string `json:"bilibiliStatus"`
	BarkConfigured      bool   `json:"barkConfigured"`
	Subscriptions       int    `json:"subscriptions"`
}

func (a *App) listUsersHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`SELECT u.id,u.username,u.display_name,u.role,u.enabled,u.force_password_change,u.created_at,i.bili_status,(i.bark_key_enc<>''),COUNT(s.id) FROM users u JOIN integrations i ON i.user_id=u.id LEFT JOIN subscriptions s ON s.user_id=u.id GROUP BY u.id ORDER BY u.created_at`)
	if err != nil {
		writeError(w, 500, "database", "无法读取用户")
		return
	}
	defer rows.Close()
	items := []adminUserView{}
	for rows.Next() {
		var item adminUserView
		if rows.Scan(&item.ID, &item.Username, &item.DisplayName, &item.Role, &item.Enabled, &item.ForcePasswordChange, &item.CreatedAt, &item.BilibiliStatus, &item.BarkConfigured, &item.Subscriptions) == nil {
			items = append(items, item)
		}
	}
	writeJSON(w, 200, items)
}

func (a *App) createUserHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username          string `json:"username"`
		DisplayName       string `json:"displayName"`
		TemporaryPassword string `json:"temporaryPassword"`
	}
	if decodeJSON(r, &input) != nil {
		writeError(w, 400, "invalid_request", "用户信息格式不正确")
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if !usernamePattern.MatchString(input.Username) {
		writeError(w, 400, "invalid_username", "用户名需为 3–32 位字母、数字、点、横线或下划线")
		return
	}
	if input.DisplayName == "" || len([]rune(input.DisplayName)) > 40 {
		writeError(w, 400, "invalid_display_name", "请输入不超过 40 个字符的显示名称")
		return
	}
	if len(input.TemporaryPassword) < 10 {
		writeError(w, 400, "weak_password", "临时密码至少需要 10 个字符")
		return
	}
	hash, err := hashPassword(input.TemporaryPassword)
	if err != nil {
		writeError(w, 500, "internal", "无法处理密码")
		return
	}
	now := time.Now().Unix()
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, 500, "database", "无法创建用户")
		return
	}
	defer tx.Rollback()
	result, err := tx.Exec(`INSERT INTO users(username,display_name,password_hash,force_password_change,created_at) VALUES(?,?,?,?,?)`, input.Username, input.DisplayName, hash, 1, now)
	if err != nil {
		writeError(w, 409, "duplicate_username", "用户名已存在")
		return
	}
	id, _ := result.LastInsertId()
	_, err = tx.Exec(`INSERT INTO integrations(user_id,bark_server,updated_at) VALUES(?,?,?)`, id, a.cfg.DefaultBarkServer, now)
	if err != nil || tx.Commit() != nil {
		writeError(w, 500, "database", "无法创建用户")
		return
	}
	writeJSON(w, 201, map[string]any{"id": id, "username": input.Username})
}

func adminUserID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}
func (a *App) updateUserHandler(w http.ResponseWriter, r *http.Request) {
	id, err := adminUserID(r)
	if err != nil {
		writeError(w, 400, "invalid_id", "用户编号不正确")
		return
	}
	var input struct {
		DisplayName string `json:"displayName"`
		Enabled     bool   `json:"enabled"`
	}
	if decodeJSON(r, &input) != nil || strings.TrimSpace(input.DisplayName) == "" {
		writeError(w, 400, "invalid_request", "显示名称不能为空")
		return
	}
	current := userFrom(r)
	if current.ID == id && !input.Enabled {
		writeError(w, 400, "cannot_disable_self", "不能停用当前账号")
		return
	}
	result, err := a.db.Exec(`UPDATE users SET display_name=?,enabled=? WHERE id=?`, strings.TrimSpace(input.DisplayName), input.Enabled, id)
	if err != nil {
		writeError(w, 500, "database", "无法更新用户")
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		writeError(w, 404, "not_found", "用户不存在")
		return
	}
	if !input.Enabled {
		_, _ = a.db.Exec(`DELETE FROM sessions WHERE user_id=?`, id)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (a *App) resetPasswordHandler(w http.ResponseWriter, r *http.Request) {
	id, err := adminUserID(r)
	if err != nil {
		writeError(w, 400, "invalid_id", "用户编号不正确")
		return
	}
	if userFrom(r).ID == id {
		writeError(w, 400, "cannot_reset_self", "请在个人设置中修改自己的密码")
		return
	}
	var input struct {
		TemporaryPassword string `json:"temporaryPassword"`
	}
	if decodeJSON(r, &input) != nil || len(input.TemporaryPassword) < 10 {
		writeError(w, 400, "weak_password", "临时密码至少需要 10 个字符")
		return
	}
	hash, err := hashPassword(input.TemporaryPassword)
	if err != nil {
		writeError(w, 500, "internal", "无法处理密码")
		return
	}
	result, err := a.db.Exec(`UPDATE users SET password_hash=?,force_password_change=1 WHERE id=?`, hash, id)
	if err != nil {
		writeError(w, 500, "database", "无法重置密码")
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		writeError(w, 404, "not_found", "用户不存在")
		return
	}
	_, _ = a.db.Exec(`DELETE FROM sessions WHERE user_id=?`, id)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) getSystemHandler(w http.ResponseWriter, r *http.Request) {
	schedule := a.loadPollSchedule()
	status := scheduleStatusAt(schedule, time.Now())
	var creators, users, pending int
	a.db.QueryRow(`SELECT COUNT(*) FROM creators WHERE EXISTS(SELECT 1 FROM subscriptions s WHERE s.creator_mid=creators.mid AND s.enabled=1)`).Scan(&creators)
	a.db.QueryRow(`SELECT COUNT(*) FROM users WHERE enabled=1`).Scan(&users)
	a.db.QueryRow(`SELECT COUNT(*) FROM deliveries WHERE status='pending'`).Scan(&pending)
	writeJSON(w, 200, map[string]any{
		"pollIntervalSeconds": schedule.Free.IntervalMinutes * 60,
		"pollSchedule":        schedule, "currentPeriod": status.CurrentPeriod,
		"currentIntervalMinutes": status.CurrentIntervalMinutes, "nextTransitionAt": status.NextTransitionAt,
		"activeCreators": creators, "activeUsers": users, "pendingDeliveries": pending,
	})
}
func (a *App) updateSystemHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		PollSchedule        *pollSchedule `json:"pollSchedule"`
		PollIntervalSeconds *int          `json:"pollIntervalSeconds"`
	}
	if decodeJSON(r, &input) != nil || (input.PollSchedule == nil && input.PollIntervalSeconds == nil) {
		writeError(w, 400, "invalid_schedule", "轮询时间表格式不正确")
		return
	}
	var schedule pollSchedule
	if input.PollSchedule != nil {
		schedule = *input.PollSchedule
	} else {
		if *input.PollIntervalSeconds < 60 || *input.PollIntervalSeconds > 86400 {
			writeError(w, 400, "invalid_interval", "轮询间隔必须在 60–86400 秒之间")
			return
		}
		schedule = a.loadPollSchedule()
		schedule.Free.IntervalMinutes = (*input.PollIntervalSeconds + 59) / 60
	}
	if err := validatePollSchedule(schedule); err != nil {
		writeError(w, 400, "invalid_schedule", err.Error())
		return
	}
	encoded, err := encodePollSchedule(schedule)
	if err != nil {
		writeError(w, 500, "encoding", "无法保存轮询时间表")
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, 500, "database", "无法保存系统设置")
		return
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO app_settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, pollScheduleKey, encoded)
	if err == nil {
		_, err = tx.Exec(`INSERT INTO app_settings(key,value) VALUES('poll_interval_seconds',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, strconv.Itoa(schedule.Free.IntervalMinutes*60))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		writeError(w, 500, "database", "无法保存系统设置")
		return
	}
	a.requeueNormalPolls(time.Now())
	writeJSON(w, 200, map[string]bool{"ok": true})
}
