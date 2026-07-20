package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func (a *App) runPollWorker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	a.pollOne(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.pollOne(ctx)
		}
	}
}

func (a *App) pollOne(ctx context.Context) {
	now := time.Now().Unix()
	var mid string
	err := a.db.QueryRowContext(ctx, `SELECT p.creator_mid FROM poll_states p WHERE p.next_poll_at<=? AND EXISTS(SELECT 1 FROM subscriptions s JOIN users u ON u.id=s.user_id WHERE s.creator_mid=p.creator_mid AND s.enabled=1 AND u.enabled=1) ORDER BY p.next_poll_at LIMIT 1`, now).Scan(&mid)
	if err != nil {
		return
	}
	var userID int64
	var encrypted string
	err = a.db.QueryRowContext(ctx, `SELECT s.user_id,i.bili_cookie_enc FROM subscriptions s JOIN users u ON u.id=s.user_id JOIN integrations i ON i.user_id=s.user_id WHERE s.creator_mid=? AND s.enabled=1 AND u.enabled=1 AND i.bili_status='valid' AND i.bili_cookie_enc<>'' ORDER BY COALESCE(i.bili_last_validated,0) DESC LIMIT 1`, mid).Scan(&userID, &encrypted)
	if err != nil {
		a.recordPollFailure(mid, "没有可用的 B 站 Cookie", false)
		return
	}
	cookie, err := a.vault.Decrypt(encrypted)
	if err != nil {
		a.recordPollFailure(mid, "无法读取加密 Cookie", false)
		return
	}
	videos, err := a.bili.GetLatestVideos(ctx, mid, cookie)
	if err != nil {
		var providerErr *ProviderError
		if errors.As(err, &providerErr) && providerErr.Auth {
			_, _ = a.db.Exec(`UPDATE integrations SET bili_status='invalid',bili_error=?,updated_at=? WHERE user_id=?`, cleanError(err.Error()), now, userID)
		}
		a.recordPollFailure(mid, err.Error(), true)
		return
	}
	if err = a.recordVideos(ctx, mid, videos); err != nil {
		a.logger.Error("record videos failed", "mid", mid, "error", err)
		a.recordPollFailure(mid, "保存投稿数据失败", true)
		return
	}
	interval := a.pollInterval()
	next := time.Now().Add(time.Duration(interval)*time.Second + time.Duration(time.Now().UnixNano()%20)*time.Second).Unix()
	_, _ = a.db.Exec(`UPDATE poll_states SET last_polled_at=?,next_poll_at=?,failure_count=0,last_error='' WHERE creator_mid=?`, now, next, mid)
}

func (a *App) pollInterval() int {
	var value int
	if a.db.QueryRow(`SELECT CAST(value AS INTEGER) FROM app_settings WHERE key='poll_interval_seconds'`).Scan(&value) != nil || value < 60 {
		return 300
	}
	return value
}
func (a *App) recordPollFailure(mid, message string, backoff bool) {
	var failures int
	_ = a.db.QueryRow(`SELECT failure_count FROM poll_states WHERE creator_mid=?`, mid).Scan(&failures)
	failures++
	delays := []time.Duration{5 * time.Minute, 15 * time.Minute, 30 * time.Minute, time.Hour}
	delay := 5 * time.Minute
	if backoff {
		index := failures - 1
		if index >= len(delays) {
			index = len(delays) - 1
		}
		delay = delays[index]
	}
	_, _ = a.db.Exec(`UPDATE poll_states SET last_polled_at=?,next_poll_at=?,failure_count=?,last_error=? WHERE creator_mid=?`, time.Now().Unix(), time.Now().Add(delay).Unix(), failures, cleanError(message), mid)
}

func (a *App) recordVideos(ctx context.Context, mid string, videos []Video) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	for _, video := range videos {
		result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO videos(bvid,creator_mid,title,url,published_at,detected_at) VALUES(?,?,?,?,?,?)`, video.BVID, mid, video.Title, video.URL, video.PublishedAt, now)
		if err != nil {
			return err
		}
		inserted, _ := result.RowsAffected()
		if inserted == 0 {
			continue
		}
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO deliveries(user_id,bvid,status,next_attempt_at,created_at) SELECT s.user_id,?,'pending',?,? FROM subscriptions s JOIN users u ON u.id=s.user_id WHERE s.creator_mid=? AND s.enabled=1 AND u.enabled=1 AND s.subscribed_at<?`, video.BVID, now, now, mid, video.PublishedAt)
		if err != nil {
			return err
		}
	}
	if len(videos) > 0 {
		latest := videos[len(videos)-1]
		_, err = tx.ExecContext(ctx, `UPDATE creators SET latest_bvid=?,latest_title=?,updated_at=? WHERE mid=?`, latest.BVID, latest.Title, now, mid)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (a *App) runDeliveryWorker(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	a.deliverOne(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.deliverOne(ctx)
		}
	}
}

type queuedDelivery struct {
	ID, UserID                                      int64
	Attempts                                        int
	BVID, VideoTitle, VideoURL, CreatorName, Avatar string
}

func (a *App) deliverOne(ctx context.Context) {
	a.deliveryMu.Lock()
	defer a.deliveryMu.Unlock()
	var item queuedDelivery
	err := a.db.QueryRowContext(ctx, `SELECT d.id,d.user_id,d.attempts,v.bvid,v.title,v.url,c.name,c.avatar FROM deliveries d JOIN videos v ON v.bvid=d.bvid JOIN creators c ON c.mid=v.creator_mid JOIN users u ON u.id=d.user_id WHERE d.status='pending' AND d.next_attempt_at<=? AND u.enabled=1 ORDER BY d.next_attempt_at LIMIT 1`, time.Now().Unix()).Scan(&item.ID, &item.UserID, &item.Attempts, &item.BVID, &item.VideoTitle, &item.VideoURL, &item.CreatorName, &item.Avatar)
	if err != nil {
		return
	}
	server, key, level, sound, err := a.loadBark(item.UserID)
	if err == nil {
		err = a.bark.Send(ctx, server, BarkMessage{DeviceKey: key, Title: item.CreatorName + " 发布了新视频", Body: item.VideoTitle, Group: "up-update", URL: item.VideoURL, Icon: item.Avatar, Level: level, Sound: sound})
	}
	if err == nil {
		_, _ = a.db.Exec(`UPDATE deliveries SET status='sent',attempts=attempts+1,last_error='',sent_at=? WHERE id=?`, time.Now().Unix(), item.ID)
		return
	}
	a.recordDeliveryFailure(item, err)
}

func (a *App) recordDeliveryFailure(item queuedDelivery, sendErr error) {
	attempts := item.Attempts + 1
	if attempts >= 6 {
		_, _ = a.db.Exec(`UPDATE deliveries SET status='failed',attempts=?,last_error=? WHERE id=?`, attempts, cleanError(sendErr.Error()), item.ID)
		return
	}
	delays := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 6 * time.Hour}
	_, _ = a.db.Exec(`UPDATE deliveries SET attempts=?,last_error=?,next_attempt_at=? WHERE id=?`, attempts, cleanError(sendErr.Error()), time.Now().Add(delays[attempts-1]).Unix(), item.ID)
}

func (a *App) runMaintenance(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.maintenance(ctx)
		}
	}
}
func (a *App) maintenance(ctx context.Context) {
	_, _ = a.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at<?`, time.Now().Unix())
	a.validateOldestCookie(ctx)
}
func (a *App) validateOldestCookie(ctx context.Context) {
	var userID int64
	var encrypted string
	err := a.db.QueryRowContext(ctx, `SELECT user_id,bili_cookie_enc FROM integrations WHERE bili_cookie_enc<>'' AND (bili_last_validated IS NULL OR bili_last_validated<?) ORDER BY COALESCE(bili_last_validated,0) LIMIT 1`, time.Now().Add(-6*time.Hour).Unix()).Scan(&userID, &encrypted)
	if err != nil {
		return
	}
	cookie, err := a.vault.Decrypt(encrypted)
	if err != nil {
		return
	}
	identity, err := a.bili.ValidateCookie(ctx, cookie)
	now := time.Now().Unix()
	if err != nil {
		status := "valid"
		var providerErr *ProviderError
		if errors.As(err, &providerErr) && providerErr.Auth {
			status = "invalid"
		}
		_, _ = a.db.Exec(`UPDATE integrations SET bili_status=?,bili_error=?,bili_last_validated=?,updated_at=? WHERE user_id=?`, status, cleanError(err.Error()), now, now, userID)
		return
	}
	_, _ = a.db.Exec(`UPDATE integrations SET bili_status='valid',bili_name=?,bili_error='',bili_last_validated=?,updated_at=? WHERE user_id=?`, identity.Name, now, now, userID)
}

func nullInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}
func workerError(prefix string, err error) error { return fmt.Errorf("%s: %w", prefix, err) }
