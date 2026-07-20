package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type BarkClient struct{ http *http.Client }

func NewBarkClient() *BarkClient { return &BarkClient{http: &http.Client{Timeout: 15 * time.Second}} }

type BarkMessage struct {
	DeviceKey string `json:"device_key"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Group     string `json:"group,omitempty"`
	URL       string `json:"url,omitempty"`
	Icon      string `json:"icon,omitempty"`
	Level     string `json:"level,omitempty"`
	Sound     string `json:"sound,omitempty"`
}

func (b *BarkClient) Send(ctx context.Context, server string, message BarkMessage) error {
	server = normalizeServerURL(server)
	parsed, err := url.Parse(server)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("Bark Server 地址不正确")
	}
	payload, _ := json.Marshal(message)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server+"/push", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := b.http.Do(req)
	if err != nil {
		return fmt.Errorf("连接 Bark 失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Bark 返回 HTTP %d", resp.StatusCode)
	}
	var result struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &result) == nil && result.Code != 0 && result.Code != http.StatusOK {
		if result.Message == "" {
			result.Message = "推送被拒绝"
		}
		return errors.New(result.Message)
	}
	return nil
}
