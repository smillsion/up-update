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
		Configured   bool   `json:"configured"`
		Server       string `json:"server"`
		Level        string `json:"level"`
		Sound        string `json:"sound"`
		QuietEnabled bool   `json:"quietEnabled"`
		QuietStart   string `json:"quietStart"`
		QuietEnd     string `json:"quietEnd"`
	} `json:"bark"`
}

func (a *App) getSettingsHandler(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var result userSettings
	var cookie, key string
	var validated sql.NullInt64
	err := a.db.QueryRow(`SELECT i.bili_cookie_enc,i.bili_status,i.bili_name,i.bili_last_validated,i.bili_error,i.bark_server,i.bark_key_enc,i.bark_level,i.bark_sound,i.bark_quiet_enabled,i.bark_quiet_start,i.bark_quiet_end,EXISTS(SELECT 1 FROM bili_refresh_tokens t WHERE t.user_id=i.user_id AND t.refresh_token_enc<>'') FROM integrations i WHERE i.user_id=?`, u.ID).
		Scan(&cookie, &result.Bilibili.Status, &result.Bilibili.Name, &validated, &result.Bilibili.Error, &result.Bark.Server, &key, &result.Bark.Level, &result.Bark.Sound, &result.Bark.QuietEnabled, &result.Bark.QuietStart, &result.Bark.QuietEnd, &result.Bilibili.AutoRefresh)
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

type barkSettingsInput struct {
	Server       string `json:"server"`
	DeviceKey    string `json:"deviceKey"`
	Level        string `json:"level"`
	Sound        string `json:"sound"`
	QuietEnabled bool   `json:"quietEnabled"`
	QuietStart   string `json:"quietStart"`
	QuietEnd     string `json:"quietEnd"`
}

func normalizeBarkSettingsInput(input *barkSettingsInput) {
	input.Server = normalizeServerURL(input.Server)
	input.DeviceKey = strings.TrimSpace(input.DeviceKey)
	input.Sound = strings.TrimSpace(input.Sound)
	if input.Level == "" {
		input.Level = "active"
	}
	if input.QuietStart == "" {
		input.QuietStart = "12:00"
	}
	if input.QuietEnd == "" {
		input.QuietEnd = "14:00"
	}
}

func validateBarkQuietInput(start, end string) error {
	startMinute, startErr := parseClock(start)
	endMinute, endErr := parseClock(end)
	if startErr != nil || endErr != nil {
		return &validationError{"午休延迟时间格式不正确"}
	}
	if startMinute == endMinute {
		return &validationError{"午休延迟开始和结束时间不能相同"}
	}
	return nil
}

type validationError struct{ message string }

func (e *validationError) Error() string { return e.message }

func (a *App) saveBarkHandler(w http.ResponseWriter, r *http.Request) {
	var input barkSettingsInput
	if decodeJSON(r, &input) != nil {
		writeError(w, 400, "invalid_request", "设置格式不正确")
		return
	}
	normalizeBarkSettingsInput(&input)
	u := userFrom(r)
	var encrypted string
	if err := a.db.QueryRow(`SELECT bark_key_enc FROM integrations WHERE user_id=?`, u.ID).Scan(&encrypted); err != nil {
		writeError(w, 500, "database", "无法读取 Bark 设置")
		return
	}
	keyForValidation := input.DeviceKey
	if keyForValidation == "" && encrypted != "" {
		keyForValidation = "saved"
	}
	if err := validateBarkInput(input.Server, keyForValidation, input.Level); err != nil {
		writeError(w, 400, "invalid_bark", err.Error())
		return
	}
	if err := validateBarkQuietInput(input.QuietStart, input.QuietEnd); err != nil {
		writeError(w, 400, "invalid_bark", err.Error())
		return
	}
	var err error
	if input.DeviceKey != "" {
		encrypted, err = a.vault.Encrypt(input.DeviceKey)
		if err != nil {
			writeError(w, 500, "encryption", "无法加密 Device Key")
			return
		}
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, 500, "database", "无法保存 Bark 设置")
		return
	}
	defer tx.Rollback()
	if input.DeviceKey != "" {
		_, err = tx.Exec(`UPDATE integrations SET bark_server=?,bark_key_enc=?,bark_level=?,bark_sound=?,bark_quiet_enabled=?,bark_quiet_start=?,bark_quiet_end=?,updated_at=? WHERE user_id=?`, input.Server, encrypted, input.Level, input.Sound, input.QuietEnabled, input.QuietStart, input.QuietEnd, time.Now().Unix(), u.ID)
	} else {
		_, err = tx.Exec(`UPDATE integrations SET bark_server=?,bark_level=?,bark_sound=?,bark_quiet_enabled=?,bark_quiet_start=?,bark_quiet_end=?,updated_at=? WHERE user_id=?`, input.Server, input.Level, input.Sound, input.QuietEnabled, input.QuietStart, input.QuietEnd, time.Now().Unix(), u.ID)
	}
	if err == nil {
		_, err = tx.Exec(`UPDATE deliveries SET deferred_until=0 WHERE user_id=? AND status='pending' AND deferred_until>0`, u.ID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		writeError(w, 500, "database", "无法保存 Bark 设置")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) testBarkHandler(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if r.ContentLength == 0 {
		settings, err := a.loadBark(u.ID)
		if err != nil {
			writeError(w, 400, "bark_missing", "请先保存 Bark 设置")
			return
		}
		if err = a.sendBarkTest(r, settings.Server, settings.Key, settings.Level, settings.Sound); err != nil {
			writeError(w, 502, "bark_failed", err.Error())
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	var input barkSettingsInput
	if decodeJSON(r, &input) != nil {
		writeError(w, 400, "invalid_request", "设置格式不正确")
		return
	}
	normalizeBarkSettingsInput(&input)
	key := input.DeviceKey
	if key == "" {
		settings, err := a.loadBark(u.ID)
		if err != nil {
			writeError(w, 400, "bark_missing", "请输入 Device Key")
			return
		}
		key = settings.Key
	}
	if err := validateBarkInput(input.Server, key, input.Level); err != nil {
		writeError(w, 400, "invalid_bark", err.Error())
		return
	}
	if err := a.sendBarkTest(r, input.Server, key, input.Level, input.Sound); err != nil {
		writeError(w, 502, "bark_failed", err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) sendBarkTest(r *http.Request, server, key, level, sound string) error {
	return a.bark.Send(r.Context(), server, BarkMessage{DeviceKey: key, Title: "up-update 测试通知", Body: "Bark 推送配置成功", Group: "up-update", Level: level, Sound: sound})
}

type barkSettings struct {
	Server, Key, Level, Sound, QuietStart, QuietEnd string
	QuietEnabled                                    bool
}

func (a *App) loadBark(userID int64) (settings barkSettings, err error) {
	var encrypted string
	err = a.db.QueryRow(`SELECT bark_server,bark_key_enc,bark_level,bark_sound,bark_quiet_enabled,bark_quiet_start,bark_quiet_end FROM integrations WHERE user_id=?`, userID).Scan(&settings.Server, &encrypted, &settings.Level, &settings.Sound, &settings.QuietEnabled, &settings.QuietStart, &settings.QuietEnd)
	if err != nil || encrypted == "" {
		return barkSettings{}, sql.ErrNoRows
	}
	settings.Key, err = a.vault.Decrypt(encrypted)
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
