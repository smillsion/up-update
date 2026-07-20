package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const sessionCookie = "up_update_session"

type currentUser struct {
	ID                  int64  `json:"id"`
	Username            string `json:"username"`
	DisplayName         string `json:"displayName"`
	Role                string `json:"role"`
	ForcePasswordChange bool   `json:"forcePasswordChange"`
	CSRFToken           string `json:"csrfToken,omitempty"`
}

type contextKey string

const userContextKey contextKey = "user"

func randomToken(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func userFrom(r *http.Request) currentUser { return r.Context().Value(userContextKey).(currentUser) }

func (a *App) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || cookie.Value == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
			return
		}
		var user currentUser
		err = a.db.QueryRow(`SELECT u.id,u.username,u.display_name,u.role,u.force_password_change,s.csrf_token
			FROM sessions s JOIN users u ON u.id=s.user_id
			WHERE s.token_hash=? AND s.expires_at>? AND u.enabled=1`, tokenHash(cookie.Value), time.Now().Unix()).
			Scan(&user.ID, &user.Username, &user.DisplayName, &user.Role, &user.ForcePasswordChange, &user.CSRFToken)
		if err != nil {
			http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: a.cfg.SecureCookies})
			writeError(w, http.StatusUnauthorized, "unauthorized", "登录已失效")
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *App) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtleString(r.Header.Get("X-CSRF-Token"), userFrom(r).CSRFToken) == false {
			writeError(w, http.StatusForbidden, "csrf", "请求校验失败，请刷新页面")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func subtleString(left, right string) bool {
	if len(left) != len(right) || left == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func (a *App) passwordChanged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if userFrom(r).ForcePasswordChange {
			writeError(w, http.StatusForbidden, "password_change_required", "请先修改临时密码")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if userFrom(r).Role != "admin" {
			writeError(w, http.StatusForbidden, "forbidden", "没有管理员权限")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type loginAttempt struct {
	Count int
	First time.Time
}
type loginLimiter struct {
	sync.Mutex
	Attempts map[string]loginAttempt
}

func newLoginLimiter() *loginLimiter { return &loginLimiter{Attempts: make(map[string]loginAttempt)} }
func (l *loginLimiter) allowed(address string) bool {
	host, _, _ := net.SplitHostPort(address)
	if host == "" {
		host = address
	}
	l.Lock()
	defer l.Unlock()
	now := time.Now()
	attempt := l.Attempts[host]
	if now.Sub(attempt.First) > 15*time.Minute {
		delete(l.Attempts, host)
		return true
	}
	return attempt.Count < 10
}
func (l *loginLimiter) failed(address string) {
	host, _, _ := net.SplitHostPort(address)
	if host == "" {
		host = address
	}
	l.Lock()
	defer l.Unlock()
	now := time.Now()
	attempt := l.Attempts[host]
	if attempt.First.IsZero() || now.Sub(attempt.First) > 15*time.Minute {
		attempt = loginAttempt{First: now}
	}
	attempt.Count++
	l.Attempts[host] = attempt
}
func (l *loginLimiter) clear(address string) {
	host, _, _ := net.SplitHostPort(address)
	l.Lock()
	delete(l.Attempts, host)
	l.Unlock()
}

func (a *App) loginHandler(w http.ResponseWriter, r *http.Request) {
	if !a.login.allowed(r.RemoteAddr) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "登录尝试过多，请稍后再试")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if decodeJSON(r, &input) != nil {
		writeError(w, 400, "invalid_request", "请输入用户名和密码")
		return
	}
	var user currentUser
	var passwordHash string
	var enabled bool
	err := a.db.QueryRow(`SELECT id,username,display_name,password_hash,role,enabled,force_password_change FROM users WHERE username=?`, strings.TrimSpace(input.Username)).Scan(&user.ID, &user.Username, &user.DisplayName, &passwordHash, &user.Role, &enabled, &user.ForcePasswordChange)
	if err != nil || !enabled || !verifyPassword(passwordHash, input.Password) {
		a.login.failed(r.RemoteAddr)
		time.Sleep(250 * time.Millisecond)
		writeError(w, 401, "invalid_credentials", "用户名或密码不正确")
		return
	}
	token, err := randomToken(32)
	if err != nil {
		writeError(w, 500, "internal", "无法创建会话")
		return
	}
	csrf, _ := randomToken(24)
	expires := time.Now().Add(30 * 24 * time.Hour)
	_, err = a.db.Exec(`INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at,created_at) VALUES(?,?,?,?,?)`, tokenHash(token), user.ID, csrf, expires.Unix(), time.Now().Unix())
	if err != nil {
		writeError(w, 500, "internal", "无法创建会话")
		return
	}
	a.login.clear(r.RemoteAddr)
	user.CSRFToken = csrf
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", Expires: expires, MaxAge: 30 * 24 * 3600, HttpOnly: true, Secure: a.cfg.SecureCookies, SameSite: http.SameSiteStrictMode})
	writeJSON(w, 200, user)
}

func (a *App) meHandler(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, userFrom(r)) }
func (a *App) logoutHandler(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_, _ = a.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, tokenHash(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.cfg.SecureCookies, SameSite: http.SameSiteStrictMode})
	w.WriteHeader(204)
}

func (a *App) changePasswordHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Current string `json:"currentPassword"`
		New     string `json:"newPassword"`
	}
	if decodeJSON(r, &input) != nil || len(input.New) < 10 {
		writeError(w, 400, "weak_password", "新密码至少需要 10 个字符")
		return
	}
	u := userFrom(r)
	var currentHash string
	if err := a.db.QueryRow(`SELECT password_hash FROM users WHERE id=?`, u.ID).Scan(&currentHash); err != nil || !verifyPassword(currentHash, input.Current) {
		writeError(w, 400, "invalid_password", "当前密码不正确")
		return
	}
	hash, err := hashPassword(input.New)
	if err != nil {
		writeError(w, 500, "internal", "无法保存密码")
		return
	}
	_, err = a.db.Exec(`UPDATE users SET password_hash=?,force_password_change=0 WHERE id=?`, hash, u.ID)
	if err != nil {
		writeError(w, 500, "internal", "无法保存密码")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func isNotFound(err error) bool { return err == sql.ErrNoRows }
