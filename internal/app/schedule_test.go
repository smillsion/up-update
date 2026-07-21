package app

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func shanghaiTime(hour, minute int) time.Time {
	return time.Date(2026, time.July, 21, hour, minute, 0, 0, shanghaiLocation)
}

func TestDefaultPollSchedulePeriods(t *testing.T) {
	schedule := defaultPollSchedule(300)
	tests := []struct {
		hour, minute int
		period       string
		interval     int
	}{
		{0, 0, "sleep", 120}, {7, 59, "sleep", 120}, {8, 0, "free", 5},
		{9, 0, "work", 15}, {11, 59, "work", 15}, {12, 0, "free", 5},
		{14, 0, "work", 15}, {17, 59, "work", 15}, {18, 0, "free", 5},
	}
	for _, test := range tests {
		period, interval := periodAt(schedule, shanghaiTime(test.hour, test.minute))
		if period != test.period || interval != test.interval {
			t.Errorf("%02d:%02d = (%s,%d), want (%s,%d)", test.hour, test.minute, period, interval, test.period, test.interval)
		}
	}
}

func TestCrossMidnightWindowAndNextTransition(t *testing.T) {
	schedule := defaultPollSchedule(300)
	schedule.Sleep.Start = "23:00"
	schedule.Sleep.End = "07:00"
	for _, current := range []time.Time{shanghaiTime(23, 0), shanghaiTime(2, 30), shanghaiTime(6, 59)} {
		period, _ := periodAt(schedule, current)
		if period != "sleep" {
			t.Errorf("periodAt(%s)=%s, want sleep", current.Format("15:04"), period)
		}
	}
	status := scheduleStatusAt(schedule, shanghaiTime(22, 0))
	if got := time.Unix(status.NextTransitionAt, 0).In(shanghaiLocation).Format("15:04"); got != "23:00" {
		t.Fatalf("next transition=%s, want 23:00", got)
	}
	status = scheduleStatusAt(schedule, shanghaiTime(1, 0))
	if got := time.Unix(status.NextTransitionAt, 0).In(shanghaiLocation).Format("15:04"); got != "07:00" {
		t.Fatalf("overnight next transition=%s, want 07:00", got)
	}
}

func TestDeliveryDeferredUntilUsesActiveSleepAndQuietWindows(t *testing.T) {
	schedule := defaultPollSchedule(300)
	schedule.Sleep.Start = "23:00"
	schedule.Sleep.End = "08:00"
	settings := barkSettings{QuietEnabled: true, QuietStart: "07:00", QuietEnd: "09:00"}

	until, deferred := deliveryDeferredUntil(schedule, settings, shanghaiTime(7, 30))
	if !deferred || until.In(shanghaiLocation).Format("2006-01-02 15:04") != "2026-07-21 09:00" {
		t.Fatalf("overlap deferred until %v, want 2026-07-21 09:00", until)
	}
	until, deferred = deliveryDeferredUntil(schedule, settings, shanghaiTime(23, 30))
	if !deferred || until.In(shanghaiLocation).Format("2006-01-02 15:04") != "2026-07-22 08:00" {
		t.Fatalf("overnight deferred until %v, want 2026-07-22 08:00", until)
	}
	if _, deferred = deliveryDeferredUntil(schedule, settings, shanghaiTime(9, 0)); deferred {
		t.Fatal("expected delivery to resume at the end boundary")
	}
}

func TestPollScheduleValidationRejectsOverlappingWork(t *testing.T) {
	schedule := defaultPollSchedule(300)
	schedule.Work.Windows = []clockWindow{{Start: "09:00", End: "12:00"}, {Start: "11:30", End: "13:00"}}
	if validatePollSchedule(schedule) == nil {
		t.Fatal("expected overlapping work windows to fail validation")
	}
}

func TestRequeueNormalPollsPreservesFailures(t *testing.T) {
	a := testApp(t)
	schedule := defaultPollSchedule(300)
	encoded, _ := json.Marshal(schedule)
	_, _ = a.db.Exec(`UPDATE app_settings SET value=? WHERE key=?`, string(encoded), pollScheduleKey)
	_, _ = a.db.Exec(`INSERT INTO creators(mid,name,updated_at) VALUES('normal','Normal',1),('failed','Failed',1)`)
	_, _ = a.db.Exec(`INSERT INTO poll_states(creator_mid,last_polled_at,next_poll_at,failure_count,last_error) VALUES('normal',1,9999999999,0,''),('failed',1,9999999999,2,'limited')`)
	now := shanghaiTime(12, 30)
	a.requeueNormalPolls(now)
	var normal, failed int64
	_ = a.db.QueryRow(`SELECT next_poll_at FROM poll_states WHERE creator_mid='normal'`).Scan(&normal)
	_ = a.db.QueryRow(`SELECT next_poll_at FROM poll_states WHERE creator_mid='failed'`).Scan(&failed)
	if normal != now.Unix() {
		t.Fatalf("normal next_poll_at=%d, want %d", normal, now.Unix())
	}
	if failed != 9999999999 {
		t.Fatalf("failed next_poll_at=%d, want unchanged", failed)
	}
}

func TestUpdateScheduleRequeuesDeferredDeliveries(t *testing.T) {
	a := testApp(t)
	userID := adminUserIDForTest(t, a)
	_, _ = a.db.Exec(`INSERT INTO creators(mid,name,updated_at) VALUES('schedule-up','UP',100)`)
	_, _ = a.db.Exec(`INSERT INTO videos(bvid,creator_mid,title,url,published_at,detected_at) VALUES('SCHEDULE-BV','schedule-up','Video','https://example/video',100,100)`)
	_, _ = a.db.Exec(`INSERT INTO deliveries(user_id,bvid,next_attempt_at,deferred_until,created_at) VALUES(?,'SCHEDULE-BV',100,9999999999,100)`, userID)
	body, _ := json.Marshal(map[string]any{"pollSchedule": defaultPollSchedule(300)})
	request := settingsRequest(http.MethodPut, "/api/admin/system", string(body), userID)
	response := httptest.NewRecorder()
	a.updateSystemHandler(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var deferredUntil int64
	if err := a.db.QueryRow(`SELECT deferred_until FROM deliveries WHERE bvid='SCHEDULE-BV'`).Scan(&deferredUntil); err != nil || deferredUntil != 0 {
		t.Fatalf("deferred_until=%d, err=%v; want queue re-evaluation", deferredUntil, err)
	}
}

func TestLegacyPollIntervalMigratesToFreePeriod(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{DataDir: dir, DatabasePath: filepath.Join(dir, "test.db"), EncryptionKey: []byte("01234567890123456789012345678901"), AdminUsername: "admin", AdminPassword: "admin-password", DefaultBarkServer: "https://api.day.app"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a, err := New(cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = a.db.Exec(`DELETE FROM app_settings WHERE key=?`, pollScheduleKey)
	_, _ = a.db.Exec(`UPDATE app_settings SET value='90' WHERE key='poll_interval_seconds'`)
	_ = a.Close()
	a, err = New(cfg, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if got := a.loadPollSchedule().Free.IntervalMinutes; got != 2 {
		t.Fatalf("free interval=%d, want 2 minutes", got)
	}
}
