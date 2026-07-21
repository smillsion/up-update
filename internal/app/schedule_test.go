package app

import (
	"encoding/json"
	"io"
	"log/slog"
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
