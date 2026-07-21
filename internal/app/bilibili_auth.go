package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const refreshPublicKey = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDLgd2OAkcGVtoE3ThUREbio0Eg
Uc/prcajMKXvkCKFCWhJYJcLkcM2DKKcSeFpD/j6Boy538YXnR6VhcuUJOhH2x71
nzPjfdTcqMz7djHum0qSZA0AyCBDABUqCrfNgCiJ00Ra7GmRj+YCK1NJEuewlb40
JNrRuoEUXpabUzGB8QIDAQAB
-----END PUBLIC KEY-----`

type BiliQRCode struct {
	URL       string
	Key       string
	Cookies   string
	ExpiresAt time.Time
}

type BiliQRStatus struct {
	Status       string
	Message      string
	Cookie       string
	RefreshToken string
}

type BiliRefreshInfo struct {
	Refresh   bool  `json:"refresh"`
	Timestamp int64 `json:"timestamp"`
}

type BiliRefreshResult struct {
	Cookie       string
	RefreshToken string
	Identity     BilibiliIdentity
}

func (c *BilibiliClient) authRequest(ctx context.Context, method, target string, form url.Values, cookie string) ([]byte, []*http.Cookie, error) {
	var body io.Reader
	if method == http.MethodPost {
		body = strings.NewReader(form.Encode())
	} else if len(form) > 0 {
		target += "?" + form.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/138.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, &ProviderError{Message: "连接 B 站失败", Retryable: true}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, &ProviderError{Code: resp.StatusCode, Message: fmt.Sprintf("B 站返回 HTTP %d", resp.StatusCode), Retryable: resp.StatusCode >= 500}
	}
	return data, resp.Cookies(), nil
}

func decodeBiliEnvelope(body []byte, destination any) error {
	var envelope biliEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return errors.New("B 站返回了无法解析的数据")
	}
	if envelope.Code != 0 {
		return &ProviderError{
			Code: envelope.Code, Message: fmt.Sprintf("B 站接口错误：%s (%d)", envelope.Message, envelope.Code),
			Retryable: envelope.Code == -352 || envelope.Code == -509, Auth: envelope.Code == -101,
		}
	}
	if destination != nil && json.Unmarshal(envelope.Data, destination) != nil {
		return errors.New("B 站返回了无法解析的数据")
	}
	return nil
}

func (c *BilibiliClient) StartQRLogin(ctx context.Context) (BiliQRCode, error) {
	body, cookies, err := c.authRequest(ctx, http.MethodGet, c.passportURL+"/x/passport-login/web/qrcode/generate", nil, "")
	if err != nil {
		return BiliQRCode{}, err
	}
	var data struct {
		URL string `json:"url"`
		Key string `json:"qrcode_key"`
	}
	if err := decodeBiliEnvelope(body, &data); err != nil {
		return BiliQRCode{}, err
	}
	if data.URL == "" || data.Key == "" {
		return BiliQRCode{}, errors.New("B 站未返回登录二维码")
	}
	return BiliQRCode{URL: data.URL, Key: data.Key, Cookies: mergeCookieHeader("", cookies), ExpiresAt: time.Now().Add(3 * time.Minute)}, nil
}

func (c *BilibiliClient) PollQRLogin(ctx context.Context, key, cookie string) (BiliQRStatus, error) {
	body, cookies, err := c.authRequest(ctx, http.MethodGet, c.passportURL+"/x/passport-login/web/qrcode/poll", url.Values{"qrcode_key": {key}, "source": {"main-fe-header"}}, cookie)
	if err != nil {
		return BiliQRStatus{}, err
	}
	var data struct {
		RefreshToken string `json:"refresh_token"`
		Code         int    `json:"code"`
		Message      string `json:"message"`
	}
	if err := decodeBiliEnvelope(body, &data); err != nil {
		return BiliQRStatus{}, err
	}
	result := BiliQRStatus{Message: data.Message, Cookie: mergeCookieHeader(cookie, cookies), RefreshToken: data.RefreshToken}
	switch data.Code {
	case 0:
		if result.RefreshToken == "" || cookieValue(result.Cookie, "SESSDATA") == "" {
			return BiliQRStatus{}, errors.New("B 站登录成功但未返回完整凭证")
		}
		result.Status = "success"
	case 86101:
		result.Status = "waiting"
	case 86090:
		result.Status = "scanned"
	case 86038:
		result.Status = "expired"
	default:
		return BiliQRStatus{}, &ProviderError{Code: data.Code, Message: fmt.Sprintf("B 站扫码登录失败：%s (%d)", data.Message, data.Code)}
	}
	return result, nil
}

func (c *BilibiliClient) EnsureDeviceCookies(ctx context.Context, cookie string) string {
	if cookieValue(cookie, "buvid3") != "" && cookieValue(cookie, "buvid4") != "" {
		return cookie
	}
	var data struct {
		B3 string `json:"b_3"`
		B4 string `json:"b_4"`
	}
	if err := c.request(ctx, "/x/frontend/finger/spi", nil, cookie, &data); err != nil {
		return cookie
	}
	return mergeCookieValues(cookie, map[string]string{"buvid3": data.B3, "buvid4": data.B4})
}

func (c *BilibiliClient) CookieRefreshInfo(ctx context.Context, cookie string) (BiliRefreshInfo, error) {
	body, _, err := c.authRequest(ctx, http.MethodGet, c.passportURL+"/x/passport-login/web/cookie/info", url.Values{"csrf": {cookieValue(cookie, "bili_jct")}}, cookie)
	if err != nil {
		return BiliRefreshInfo{}, err
	}
	var data BiliRefreshInfo
	if err := decodeBiliEnvelope(body, &data); err != nil {
		return BiliRefreshInfo{}, err
	}
	return data, nil
}

func (c *BilibiliClient) RefreshCookie(ctx context.Context, cookie, refreshToken string, timestamp int64) (BiliRefreshResult, error) {
	path, err := correspondPath(timestamp)
	if err != nil {
		return BiliRefreshResult{}, err
	}
	body, _, err := c.authRequest(ctx, http.MethodGet, c.wwwURL+"/correspond/1/"+path, nil, cookie)
	if err != nil {
		return BiliRefreshResult{}, err
	}
	refreshCSRF, err := extractRefreshCSRF(body)
	if err != nil {
		return BiliRefreshResult{}, err
	}
	form := url.Values{
		"csrf": {cookieValue(cookie, "bili_jct")}, "refresh_csrf": {refreshCSRF},
		"source": {"main_web"}, "refresh_token": {refreshToken},
	}
	body, cookies, err := c.authRequest(ctx, http.MethodPost, c.passportURL+"/x/passport-login/web/cookie/refresh", form, cookie)
	if err != nil {
		return BiliRefreshResult{}, err
	}
	var data struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := decodeBiliEnvelope(body, &data); err != nil {
		return BiliRefreshResult{}, err
	}
	newCookie := mergeCookieHeader(cookie, cookies)
	if data.RefreshToken == "" || cookieValue(newCookie, "SESSDATA") == "" || cookieValue(newCookie, "bili_jct") == "" {
		return BiliRefreshResult{}, errors.New("B 站未返回完整的刷新凭证")
	}
	identity, err := c.ValidateCookie(ctx, newCookie)
	if err != nil {
		return BiliRefreshResult{}, err
	}
	return BiliRefreshResult{Cookie: newCookie, RefreshToken: data.RefreshToken, Identity: identity}, nil
}

func (c *BilibiliClient) ConfirmCookieRefresh(ctx context.Context, newCookie, oldRefreshToken string) error {
	confirm := url.Values{"csrf": {cookieValue(newCookie, "bili_jct")}, "refresh_token": {oldRefreshToken}}
	body, _, err := c.authRequest(ctx, http.MethodPost, c.passportURL+"/x/passport-login/web/confirm/refresh", confirm, newCookie)
	if err != nil {
		return err
	}
	return decodeBiliEnvelope(body, nil)
}

func correspondPath(timestamp int64) (string, error) {
	block, _ := pem.Decode([]byte(refreshPublicKey))
	if block == nil {
		return "", errors.New("无法读取 B 站刷新公钥")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", err
	}
	publicKey, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return "", errors.New("B 站刷新公钥格式不正确")
	}
	encrypted, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, publicKey, []byte(fmt.Sprintf("refresh_%d", timestamp)), nil)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(encrypted), nil
}

func extractRefreshCSRF(body []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	decoder.Strict = false
	decoder.AutoClose = []string{"meta", "link", "br", "img", "input"}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", errors.New("无法解析 B 站刷新页面")
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attribute := range start.Attr {
			if attribute.Name.Local != "id" || attribute.Value != "1-name" {
				continue
			}
			var value string
			if err := decoder.DecodeElement(&value, &start); err == nil && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value), nil
			}
			return "", errors.New("B 站未返回刷新校验信息")
		}
	}
	return "", errors.New("B 站未返回刷新校验信息")
}

func parseCookieHeader(raw string) (map[string]string, []string) {
	values := make(map[string]string)
	order := make([]string, 0)
	for _, part := range strings.Split(raw, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok || name == "" {
			continue
		}
		if _, exists := values[name]; !exists {
			order = append(order, name)
		}
		values[name] = value
	}
	return values, order
}

func serializeCookies(values map[string]string, order []string) string {
	parts := make([]string, 0, len(values))
	seen := make(map[string]bool)
	for _, name := range order {
		if value, ok := values[name]; ok && value != "" {
			parts = append(parts, name+"="+value)
			seen[name] = true
		}
	}
	extra := make([]string, 0)
	for name, value := range values {
		if !seen[name] && value != "" {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		parts = append(parts, name+"="+values[name])
	}
	return strings.Join(parts, "; ")
}

func mergeCookieHeader(raw string, cookies []*http.Cookie) string {
	values, order := parseCookieHeader(raw)
	for _, cookie := range cookies {
		if _, exists := values[cookie.Name]; !exists {
			order = append(order, cookie.Name)
		}
		if cookie.MaxAge < 0 || (!cookie.Expires.IsZero() && cookie.Expires.Before(time.Now())) {
			delete(values, cookie.Name)
		} else {
			values[cookie.Name] = cookie.Value
		}
	}
	return serializeCookies(values, order)
}

func mergeCookieValues(raw string, updates map[string]string) string {
	values, order := parseCookieHeader(raw)
	for name, value := range updates {
		if value == "" {
			continue
		}
		if _, exists := values[name]; !exists {
			order = append(order, name)
		}
		values[name] = value
	}
	return serializeCookies(values, order)
}

func cookieValue(raw, name string) string {
	values, _ := parseCookieHeader(raw)
	return values[name]
}
