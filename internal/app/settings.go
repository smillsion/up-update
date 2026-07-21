package app

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	errBiliCookieMissing = errors.New("请先在设置中保存并验证 B 站 Cookie")
	errBiliCookieInvalid = errors.New("B 站 Cookie 当前不可用，请先前往设置更新")
)

type userSettings struct {
	Bilibili struct {
		Configured    bool   `json:"configured"`
		AutoRefresh   bool   `json:"autoRefresh"`
		Status        string `json:"status"`
		Name          string `json:"name"`
		LastValidated *int64 `json:"lastValidated"`
		Error         string `json:"error"`
	} `json:"bilibili"`
	Bark struct {
		Configured bool   `json:"configured"`
		Server     string `json:"server"`
		Level      string `json:"level"`
		Sound      string `json:"sound"`
	} `json:"bark"`
}

func (a *App) getSettingsHandler(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var result userSettings
	var cookie, key string
	var validated sql.NullInt64
	err := a.db.QueryRow(`SELECT i.bili_cookie_enc,i.bili_status,i.bili_name,i.bili_last_validated,i.bili_error,i.bark_server,i.bark_key_enc,i.bark_level,i.bark_sound,EXISTS(SELECT 1 FROM bili_refresh_tokens t WHERE t.user_id=i.user_id AND t.refresh_token_enc<>'') FROM integrations i WHERE i.user_id=?`, u.ID).
		Scan(&cookie, &result.Bilibili.Status, &result.Bilibili.Name, &validated, &result.Bilibili.Error, &result.Bark.Server, &key, &result.Bark.Level, &result.Bark.Sound, &result.Bilibili.AutoRefresh)
	if err != nil {
		writeError(w, 500, "database", "无法读取设置")
		return
	}
	result.Bilibili.Configured = cookie != ""
	result.Bark.Configured = key != ""
	if validated.Valid {
		result.Bilibili.LastValidated = &validated.Int64
	}
	writeJSON(w, 200, result)
}

func (a *App) saveBilibiliHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Cookie string `json:"cookie"`
	}
	if decodeJSON(r, &input) != nil || strings.TrimSpace(input.Cookie) == "" {
		writeError(w, 400, "invalid_cookie", "请输入 Cookie")
		return
	}
	identity, err := a.bili.ValidateCookie(r.Context(), input.Cookie)
	if err != nil {
		writeError(w, 400, "cookie_validation_failed", err.Error())
		return
	}
	encrypted, err := a.vault.Encrypt(strings.TrimSpace(input.Cookie))
	if err != nil {
		writeError(w, 500, "encryption", "无法加密 Cookie")
		return
	}
	now := time.Now().Unix()
	u := userFrom(r)
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, 500, "database", "无法保存 Cookie")
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE integrations SET bili_cookie_enc=?,bili_status='valid',bili_name=?,bili_last_validated=?,bili_error='',updated_at=? WHERE user_id=?`, encrypted, identity.Name, now, now, u.ID); err == nil {
		_, err = tx.Exec(`DELETE FROM bili_refresh_tokens WHERE user_id=?`, u.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		writeError(w, 500, "database", "无法保存 Cookie")
		return
	}
	writeJSON(w, 200, map[string]any{"status": "valid", "name": identity.Name, "lastValidated": now})
}

func validateBarkInput(server, key, level string) error {
	parsed, err := url.Parse(normalizeServerURL(server))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return &validationError{"Bark Server 必须是 HTTP 或 HTTPS 地址"}
	}
	if strings.TrimSpace(key) == "" {
		return &validationError{"Device Key 不能为空"}
	}
	switch level {
	case "active", "timeSensitive", "passive", "critical":
	default:
		return &validationError{"通知级别不受支持"}
	}
	return nil
}

type validationError struct{ message string }

func (e *validationError) Error() string { return e.message }

func (a *App) saveBarkHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Server    string `json:"server"`
		DeviceKey string `json:"deviceKey"`
		Level     string `json:"level"`
		Sound     string `json:"sound"`
	}
	if decodeJSON(r, &input) != nil {
		writeError(w, 400, "invalid_request", "设置格式不正确")
		return
	}
	input.Server = normalizeServerURL(input.Server)
	input.DeviceKey = strings.TrimSpace(input.DeviceKey)
	if input.Level == "" {
		input.Level = "active"
	}
	if err := validateBarkInput(input.Server, input.DeviceKey, input.Level); err != nil {
		writeError(w, 400, "invalid_bark", err.Error())
		return
	}
	encrypted, err := a.vault.Encrypt(input.DeviceKey)
	if err != nil {
		writeError(w, 500, "encryption", "无法加密 Device Key")
		return
	}
	u := userFrom(r)
	_, err = a.db.Exec(`UPDATE integrations SET bark_server=?,bark_key_enc=?,bark_level=?,bark_sound=?,updated_at=? WHERE user_id=?`, input.Server, encrypted, input.Level, strings.TrimSpace(input.Sound), time.Now().Unix(), u.ID)
	if err != nil {
		writeError(w, 500, "database", "无法保存 Bark 设置")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) testBarkHandler(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	server, key, level, sound, err := a.loadBark(u.ID)
	if err != nil {
		writeError(w, 400, "bark_missing", "请先保存 Bark 设置")
		return
	}
	if err = a.bark.Send(r.Context(), server, BarkMessage{DeviceKey: key, Title: "up-update 测试通知", Body: "Bark 推送配置成功", Group: "up-update", Level: level, Sound: sound}); err != nil {
		writeError(w, 502, "bark_failed", err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) loadBark(userID int64) (server, key, level, sound string, err error) {
	var encrypted string
	err = a.db.QueryRow(`SELECT bark_server,bark_key_enc,bark_level,bark_sound FROM integrations WHERE user_id=?`, userID).Scan(&server, &encrypted, &level, &sound)
	if err != nil || encrypted == "" {
		return "", "", "", "", sql.ErrNoRows
	}
	key, err = a.vault.Decrypt(encrypted)
	return
}

func (a *App) loadBiliCookie(userID int64) (string, error) {
	var encrypted, status string
	err := a.db.QueryRow(`SELECT bili_cookie_enc,bili_status FROM integrations WHERE user_id=?`, userID).Scan(&encrypted, &status)
	if err != nil || encrypted == "" {
		return "", errBiliCookieMissing
	}
	if status != "valid" {
		return "", errBiliCookieInvalid
	}
	return a.vault.Decrypt(encrypted)
}
