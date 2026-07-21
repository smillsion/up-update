package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

const (
	pollScheduleKey = "poll_schedule"
	shanghaiOffset  = 8 * 60 * 60
)

var shanghaiLocation = time.FixedZone("Asia/Shanghai", shanghaiOffset)

type clockWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type sleepPollPeriod struct {
	clockWindow
	IntervalMinutes int `json:"intervalMinutes"`
}

type workPollPeriod struct {
	Windows         []clockWindow `json:"windows"`
	IntervalMinutes int           `json:"intervalMinutes"`
}

type freePollPeriod struct {
	IntervalMinutes int `json:"intervalMinutes"`
}

type pollSchedule struct {
	Timezone string          `json:"timezone"`
	Sleep    sleepPollPeriod `json:"sleep"`
	Work     workPollPeriod  `json:"work"`
	Free     freePollPeriod  `json:"free"`
}

type pollScheduleStatus struct {
	CurrentPeriod          string `json:"currentPeriod"`
	CurrentIntervalMinutes int    `json:"currentIntervalMinutes"`
	NextTransitionAt       int64  `json:"nextTransitionAt"`
}

func defaultPollSchedule(legacyFreeSeconds int) pollSchedule {
	freeMinutes := 5
	if legacyFreeSeconds >= 60 {
		freeMinutes = (legacyFreeSeconds + 59) / 60
	}
	return pollSchedule{
		Timezone: "Asia/Shanghai",
		Sleep:    sleepPollPeriod{clockWindow: clockWindow{Start: "00:00", End: "08:00"}, IntervalMinutes: 120},
		Work:     workPollPeriod{Windows: []clockWindow{{Start: "09:00", End: "12:00"}, {Start: "14:00", End: "18:00"}}, IntervalMinutes: 15},
		Free:     freePollPeriod{IntervalMinutes: freeMinutes},
	}
}

func validatePollSchedule(schedule pollSchedule) error {
	if schedule.Timezone != "Asia/Shanghai" {
		return errors.New("时区必须为 Asia/Shanghai")
	}
	if schedule.Sleep.IntervalMinutes < 1 || schedule.Sleep.IntervalMinutes > 1440 || schedule.Work.IntervalMinutes < 1 || schedule.Work.IntervalMinutes > 1440 || schedule.Free.IntervalMinutes < 1 || schedule.Free.IntervalMinutes > 1440 {
		return errors.New("轮询间隔必须在 1–1440 分钟之间")
	}
	sleepStart, err := parseClock(schedule.Sleep.Start)
	if err != nil {
		return errors.New("睡眠开始时间格式不正确")
	}
	sleepEnd, err := parseClock(schedule.Sleep.End)
	if err != nil {
		return errors.New("睡眠结束时间格式不正确")
	}
	if sleepStart == sleepEnd {
		return errors.New("睡眠开始和结束时间不能相同")
	}
	if len(schedule.Work.Windows) < 1 || len(schedule.Work.Windows) > 4 {
		return errors.New("工作时段需要设置 1–4 组")
	}
	type minuteWindow struct{ start, end int }
	work := make([]minuteWindow, 0, len(schedule.Work.Windows))
	for _, window := range schedule.Work.Windows {
		start, startErr := parseClock(window.Start)
		end, endErr := parseClock(window.End)
		if startErr != nil || endErr != nil || start >= end {
			return errors.New("工作时段必须使用有效的当天开始和结束时间")
		}
		work = append(work, minuteWindow{start: start, end: end})
	}
	sort.Slice(work, func(i, j int) bool { return work[i].start < work[j].start })
	for index := 1; index < len(work); index++ {
		if work[index].start < work[index-1].end {
			return errors.New("工作时段不能重叠")
		}
	}
	return nil
}

func parseClock(value string) (int, error) {
	parsed, err := time.Parse("15:04", value)
	if err != nil || parsed.Format("15:04") != value {
		return 0, errors.New("invalid clock")
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}

func containsClock(window clockWindow, minute int) bool {
	start, startErr := parseClock(window.Start)
	end, endErr := parseClock(window.End)
	if startErr != nil || endErr != nil || start == end {
		return false
	}
	if start < end {
		return minute >= start && minute < end
	}
	return minute >= start || minute < end
}

func periodAt(schedule pollSchedule, now time.Time) (string, int) {
	local := now.In(shanghaiLocation)
	minute := local.Hour()*60 + local.Minute()
	if containsClock(schedule.Sleep.clockWindow, minute) {
		return "sleep", schedule.Sleep.IntervalMinutes
	}
	for _, window := range schedule.Work.Windows {
		if containsClock(window, minute) {
			return "work", schedule.Work.IntervalMinutes
		}
	}
	return "free", schedule.Free.IntervalMinutes
}

func scheduleStatusAt(schedule pollSchedule, now time.Time) pollScheduleStatus {
	period, interval := periodAt(schedule, now)
	return pollScheduleStatus{
		CurrentPeriod: period, CurrentIntervalMinutes: interval,
		NextTransitionAt: nextScheduleTransition(schedule, now).Unix(),
	}
}

func nextScheduleTransition(schedule pollSchedule, now time.Time) time.Time {
	localNow := now.In(shanghaiLocation)
	startOfDay := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, shanghaiLocation)
	var next time.Time
	minutes := make([]int, 0, 2+len(schedule.Work.Windows)*2)
	for _, value := range []string{schedule.Sleep.Start, schedule.Sleep.End} {
		if minute, err := parseClock(value); err == nil {
			minutes = append(minutes, minute)
		}
	}
	for _, window := range schedule.Work.Windows {
		for _, value := range []string{window.Start, window.End} {
			if minute, err := parseClock(value); err == nil {
				minutes = append(minutes, minute)
			}
		}
	}
	for day := 0; day <= 2; day++ {
		for _, minute := range minutes {
			candidate := startOfDay.AddDate(0, 0, day).Add(time.Duration(minute) * time.Minute)
			if !candidate.After(localNow) {
				continue
			}
			before, _ := periodAt(schedule, candidate.Add(-time.Second))
			after, _ := periodAt(schedule, candidate)
			if before != after && (next.IsZero() || candidate.Before(next)) {
				next = candidate
			}
		}
	}
	if !next.IsZero() {
		return next
	}
	return startOfDay.AddDate(0, 0, 1)
}

func (a *App) loadPollSchedule() pollSchedule {
	fallback := defaultPollSchedule(300)
	var encoded string
	if err := a.db.QueryRow(`SELECT value FROM app_settings WHERE key=?`, pollScheduleKey).Scan(&encoded); err != nil {
		return fallback
	}
	var schedule pollSchedule
	if json.Unmarshal([]byte(encoded), &schedule) != nil || validatePollSchedule(schedule) != nil {
		return fallback
	}
	return schedule
}

func (a *App) currentPollIntervalSeconds(now time.Time) int {
	_, minutes := periodAt(a.loadPollSchedule(), now)
	return minutes * 60
}

func (a *App) requeueNormalPolls(now time.Time) {
	interval := int64(a.currentPollIntervalSeconds(now))
	unix := now.Unix()
	_, _ = a.db.Exec(`UPDATE poll_states SET next_poll_at=CASE WHEN last_polled_at IS NULL THEN MIN(next_poll_at,?) ELSE MAX(?,last_polled_at+?) END WHERE failure_count=0 AND last_error=''`, unix, unix, interval)
}

func encodePollSchedule(schedule pollSchedule) (string, error) {
	value, err := json.Marshal(schedule)
	if err != nil {
		return "", fmt.Errorf("encode poll schedule: %w", err)
	}
	return string(value), nil
}
