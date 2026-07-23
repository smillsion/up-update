package app

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

type biliQRSession struct {
	mu        sync.Mutex
	ID        string
	UserID    int64
	QRURL     string
	Key       string
	Cookie    string
	ExpiresAt time.Time
	LastPoll  time.Time
	Status    string
	Message   string
}

type biliQRStartView struct {
	SessionID string `json:"sessionId"`
	QRURL     string `json:"qrUrl"`
	ExpiresAt int64  `json:"expiresAt"`
}

type biliQRPollView struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Name    string `json:"name,omitempty"`
}

func (a *App) activeQRSession(userID int64, now time.Time) *biliQRSession {
	for id, session := range a.qrSessions {
		if now.After(session.ExpiresAt) {
			delete(a.qrSessions, id)
			continue
		}
		if session.UserID == userID {
			return session
		}
	}
	return nil
}

func qrStartView(session *biliQRSession) biliQRStartView {
	return biliQRStartView{SessionID: session.ID, QRURL: session.QRURL, ExpiresAt: session.ExpiresAt.Unix()}
}

func (a *App) startBilibiliQRHandler(w http.ResponseWriter, r *http.Request) {
	userID := userFrom(r).ID
	now := time.Now()
	a.qrMu.Lock()
	if session := a.activeQRSession(userID, now); session != nil {
		view := qrStartView(session)
		a.qrMu.Unlock()
		writeJSON(w, http.StatusOK, view)
		return
	}
	a.qrMu.Unlock()

	qr, err := a.bili.StartQRLogin(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "bilibili_qr_failed", err.Error())
		return
	}
	id, err := randomToken(24)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "random", "无法创建扫码会话")
		return
	}
	session := &biliQRSession{ID: id, UserID: userID, QRURL: qr.URL, Key: qr.Key, Cookie: qr.Cookies, ExpiresAt: qr.ExpiresAt, Status: "waiting", Message: "请使用哔哩哔哩客户端扫码"}
	a.qrMu.Lock()
	if existing := a.activeQRSession(userID, time.Now()); existing != nil {
		session = existing
	} else {
		a.qrSessions[id] = session
	}
	a.qrMu.Unlock()
	writeJSON(w, http.StatusCreated, qrStartView(session))
}

func (a *App) pollBilibiliQRHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID := userFrom(r).ID
	a.qrMu.Lock()
	session := a.qrSessions[id]
	if session == nil || session.UserID != userID {
		a.qrMu.Unlock()
		writeError(w, http.StatusNotFound, "qrcode_not_found", "扫码会话不存在或已结束")
		return
	}
	if time.Now().After(session.ExpiresAt) {
		delete(a.qrSessions, id)
		a.qrMu.Unlock()
		writeJSON(w, http.StatusOK, biliQRPollView{Status: "expired", Message: "二维码已过期，请重新生成"})
		return
	}
	a.qrMu.Unlock()

	session.mu.Lock()
	defer session.mu.Unlock()
	if since := time.Since(session.LastPoll); !session.LastPoll.IsZero() && since < time.Second {
		writeJSON(w, http.StatusOK, biliQRPollView{Status: session.Status, Message: session.Message})
		return
	}
	session.LastPoll = time.Now()
	result, err := a.bili.PollQRLogin(r.Context(), session.Key, session.Cookie)
	if err != nil {
		writeError(w, http.StatusBadGateway, "bilibili_qr_failed", err.Error())
		return
	}
	session.Cookie = result.Cookie
	session.Status = result.Status
	session.Message = result.Message
	if result.Status == "expired" {
		a.removeQRSession(id, session)
		writeJSON(w, http.StatusOK, biliQRPollView{Status: "expired", Message: "二维码已过期，请重新生成"})
		return
	}
	if result.Status != "success" {
		writeJSON(w, http.StatusOK, biliQRPollView{Status: result.Status, Message: qrStatusMessage(result.Status, result.Message)})
		return
	}

	cookie := a.bili.EnsureDeviceCookies(r.Context(), result.Cookie)
	identity, err := a.bili.ValidateCookie(r.Context(), cookie)
	if err != nil {
		writeError(w, http.StatusBadGateway, "bilibili_qr_validation_failed", err.Error())
		return
	}
	if err := a.saveBiliCredential(r.Context(), userID, cookie, result.RefreshToken, identity.Name); err != nil {
		writeError(w, http.StatusInternalServerError, "database", "无法保存 B 站登录凭证")
		return
	}
	a.removeQRSession(id, session)
	writeJSON(w, http.StatusOK, biliQRPollView{Status: "success", Message: "扫码登录成功", Name: identity.Name})
}

func qrStatusMessage(status, fallback string) string {
	switch status {
	case "waiting":
		return "请使用哔哩哔哩客户端扫码"
	case "scanned":
		return "已扫码，请在手机上确认登录"
	}
	if fallback != "" {
		return fallback
	}
	return "正在等待扫码"
}

func (a *App) removeQRSession(id string, expected *biliQRSession) {
	a.qrMu.Lock()
	if a.qrSessions[id] == expected {
		delete(a.qrSessions, id)
	}
	a.qrMu.Unlock()
}

func (a *App) cancelBilibiliQRHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID := userFrom(r).ID
	a.qrMu.Lock()
	if session := a.qrSessions[id]; session != nil && session.UserID == userID {
		delete(a.qrSessions, id)
	}
	a.qrMu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) cancelBilibiliQRSessionsForUser(userID int64) {
	a.qrMu.Lock()
	sessions := make([]*biliQRSession, 0, 1)
	for id, session := range a.qrSessions {
		if session.UserID == userID {
			delete(a.qrSessions, id)
			sessions = append(sessions, session)
		}
	}
	a.qrMu.Unlock()

	// Wait for any in-flight poll to finish before credentials are removed.
	for _, session := range sessions {
		session.mu.Lock()
		session.mu.Unlock()
	}
}

func (a *App) saveBiliCredential(ctx context.Context, userID int64, cookie, refreshToken, name string) error {
	if cookie == "" || refreshToken == "" {
		return errors.New("B 站登录凭证不完整")
	}
	encryptedCookie, err := a.vault.Encrypt(cookie)
	if err != nil {
		return err
	}
	encryptedToken, err := a.vault.Encrypt(refreshToken)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE integrations SET bili_cookie_enc=?,bili_status='valid',bili_name=?,bili_last_validated=?,bili_error='',updated_at=? WHERE user_id=?`, encryptedCookie, name, now, now, userID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO bili_refresh_tokens(user_id,refresh_token_enc,refreshed_at) VALUES(?,?,?) ON CONFLICT(user_id) DO UPDATE SET refresh_token_enc=excluded.refresh_token_enc,refreshed_at=excluded.refreshed_at`, userID, encryptedToken, now); err != nil {
		return err
	}
	return tx.Commit()
}
