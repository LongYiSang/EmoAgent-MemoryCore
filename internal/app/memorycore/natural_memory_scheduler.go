package memorycore

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"time"
)

type naturalSchedule struct {
	LocalDate string
	LocalTime string
	Timezone  string
}

func (s *service) RunNaturalMemoryTick(ctx context.Context, req RunNaturalMemoryTickRequest) (*RunNaturalMemoryCycleResult, error) {
	personaID := defaultString(req.PersonaID, s.persona)
	opts := s.naturalOptionsForRequest(req.Options)
	now := req.Now
	if now.IsZero() {
		now = s.now()
	}
	if !opts.Enabled || !opts.SleepCycle.Enabled {
		return skippedNaturalResult(personaID, NaturalMemoryRunSleepCycle, opts, req.DryRun, "sleep cycle disabled"), nil
	}
	schedule := naturalScheduleFor(now, opts, req.LocalDate, req.LocalTime, req.Timezone)
	if due, reason, err := s.naturalSleepCycleDue(ctx, personaID, now, opts, schedule, req.Force, req.Startup); err != nil {
		return nil, err
	} else if !due {
		return skippedNaturalResult(personaID, NaturalMemoryRunSleepCycle, opts, req.DryRun, reason), nil
	}
	result, err := s.RunNaturalMemoryCycle(ctx, RunNaturalMemoryCycleRequest{
		PersonaID: personaID,
		Now:       now,
		DryRun:    req.DryRun,
		Force:     req.Force,
		Explain:   req.Explain,
		RunKind:   NaturalMemoryRunSleepCycle,
		LocalDate: schedule.LocalDate,
		LocalTime: schedule.LocalTime,
		Timezone:  schedule.Timezone,
		Options:   opts,
	})
	if err != nil {
		return nil, err
	}
	if req.Explain && opts.SleepCycle.WarnIfOutsideNightWindow {
		loc, err := naturalLocation(schedule.Timezone)
		if err != nil {
			return nil, err
		}
		if ok, err := naturalWithinNightWindow(now.In(loc), opts); err != nil {
			return nil, err
		} else if !ok {
			result.Explain = append(result.Explain, NaturalMemoryExplainItem{
				ReasonCodes:       []string{"outside_night_window"},
				SafeReasonSummary: "sleep cycle ran outside configured night window",
			})
		}
	}
	return result, nil
}

func (s *service) naturalSleepCycleDue(ctx context.Context, personaID string, now time.Time, opts NaturalMemoryOptions, schedule naturalSchedule, force bool, startup bool) (bool, string, error) {
	hour, minute, err := parseNaturalHHMM(schedule.LocalTime)
	if err != nil {
		return false, "", err
	}
	loc, err := naturalLocation(schedule.Timezone)
	if err != nil {
		return false, "", err
	}
	localNow := now.In(loc)
	dueAt := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, loc)
	dueAt = dueAt.Add(naturalDeterministicJitter(personaID, schedule.LocalDate, opts.SleepCycle.Jitter))
	if !force && localNow.Before(dueAt) {
		return false, "sleep cycle local_time not reached", nil
	}
	if startup && !force && !opts.SleepCycle.RunMissedOnStart && localNow.After(dueAt) {
		return false, "sleep cycle missed on startup", nil
	}
	completed, err := s.natural.SleepCycleCompletedForDate(ctx, personaID, schedule.LocalDate)
	if err != nil {
		return false, "", err
	}
	if completed && !force {
		return false, "sleep cycle already completed", nil
	}
	last, ok, err := s.natural.LastCompletedSleepCycle(ctx, personaID)
	if err != nil {
		return false, "", err
	}
	if ok && !force && now.Sub(last) < opts.SleepCycle.MinInterval {
		return false, "sleep cycle min interval not reached", nil
	}
	return true, "", nil
}

func naturalDeterministicJitter(personaID string, localDate string, max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	if max < time.Minute {
		return max
	}
	minutes := int(max / time.Minute)
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(strings.TrimSpace(personaID) + "|" + strings.TrimSpace(localDate)))
	return time.Duration(1+int(hash.Sum32()%uint32(minutes))) * time.Minute
}

func naturalWithinNightWindow(localNow time.Time, opts NaturalMemoryOptions) (bool, error) {
	startHour, startMinute, err := parseNaturalHHMM(opts.SleepCycle.NightWindowStart)
	if err != nil {
		return false, err
	}
	endHour, endMinute, err := parseNaturalHHMM(opts.SleepCycle.NightWindowEnd)
	if err != nil {
		return false, err
	}
	start := startHour*60 + startMinute
	end := endHour*60 + endMinute
	current := localNow.Hour()*60 + localNow.Minute()
	if start == end {
		return true, nil
	}
	if start < end {
		return current >= start && current <= end, nil
	}
	return current >= start || current <= end, nil
}

func naturalScheduleFor(now time.Time, opts NaturalMemoryOptions, localDate string, localTime string, timezone string) naturalSchedule {
	if strings.TrimSpace(timezone) == "" {
		timezone = opts.SleepCycle.Timezone
	}
	if strings.TrimSpace(timezone) == "" {
		timezone = "Asia/Shanghai"
	}
	loc, err := naturalLocation(timezone)
	if err != nil {
		loc = time.FixedZone("Asia/Shanghai", 8*60*60)
		timezone = "Asia/Shanghai"
	}
	localNow := now.In(loc)
	if strings.TrimSpace(localDate) == "" {
		localDate = localNow.Format("2006-01-02")
	}
	if strings.TrimSpace(localTime) == "" {
		localTime = opts.SleepCycle.LocalTime
	}
	return naturalSchedule{LocalDate: localDate, LocalTime: localTime, Timezone: timezone}
}

func naturalLocation(timezone string) (*time.Location, error) {
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil && timezone == "Asia/Shanghai" {
		return time.FixedZone("Asia/Shanghai", 8*60*60), nil
	}
	return loc, err
}

func parseNaturalHHMM(value string) (int, int, error) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, 0, fmt.Errorf("%w: natural memory local_time must be HH:mm", ErrInvalidRequest)
	}
	return parsed.Hour(), parsed.Minute(), nil
}
