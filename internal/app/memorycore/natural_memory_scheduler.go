package memorycore

import (
	"context"
	"fmt"
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
		return skippedNaturalResult(personaID, NaturalMemoryRunSleepCycle, opts, false, "sleep cycle disabled"), nil
	}
	schedule := naturalScheduleFor(now, opts, "", "", "")
	if due, reason, err := s.naturalSleepCycleDue(ctx, personaID, now, opts, schedule, req.Force); err != nil {
		return nil, err
	} else if !due {
		return skippedNaturalResult(personaID, NaturalMemoryRunSleepCycle, opts, false, reason), nil
	}
	return s.RunNaturalMemoryCycle(ctx, RunNaturalMemoryCycleRequest{
		PersonaID: personaID,
		Now:       now,
		Force:     req.Force,
		Explain:   req.Explain,
		RunKind:   NaturalMemoryRunSleepCycle,
		LocalDate: schedule.LocalDate,
		LocalTime: schedule.LocalTime,
		Timezone:  schedule.Timezone,
		Options:   opts,
	})
}

func (s *service) naturalSleepCycleDue(ctx context.Context, personaID string, now time.Time, opts NaturalMemoryOptions, schedule naturalSchedule, force bool) (bool, string, error) {
	hour, minute, err := parseNaturalHHMM(opts.SleepCycle.LocalTime)
	if err != nil {
		return false, "", err
	}
	loc, err := naturalLocation(schedule.Timezone)
	if err != nil {
		return false, "", err
	}
	localNow := now.In(loc)
	dueAt := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, loc)
	if !force && localNow.Before(dueAt) {
		return false, "sleep cycle local_time not reached", nil
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
