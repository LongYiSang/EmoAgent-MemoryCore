package main

import (
	"context"
	"fmt"
	"io"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func runStartSession(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("start-session", stderr)
	var opts commonOptions
	var id, channel, title, startedAt string
	addCommonFlags(fs, &opts, formatText)
	fs.StringVar(&id, "id", "", "session id")
	fs.StringVar(&channel, "channel", memorycore.ChannelCLI, "session channel")
	fs.StringVar(&title, "title", "", "session title")
	fs.StringVar(&startedAt, "started-at", "", "RFC3339 start time")
	if !parseFlags(fs, args) {
		return 2
	}
	if !requireDB(stderr, fs, opts.DBPath) {
		return 2
	}
	if err := validateFormat(opts.Format, formatText, formatJSON, formatID); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateOneOf("--channel", channel, memorycore.ChannelCLI, memorycore.ChannelAPI, memorycore.ChannelWebUI, memorycore.ChannelTelegram, memorycore.ChannelQQ, memorycore.ChannelImported, memorycore.ChannelOther); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	started, err := parseOptionalTime(startedAt, "--started-at")
	if err != nil {
		return usageError(stderr, fs, err.Error())
	}

	ctx := context.Background()
	svc, err := openService(ctx, opts)
	if err != nil {
		return runtimeError(stderr, "open memorycore: %v", err)
	}
	defer svc.Close()

	session, err := svc.StartSession(ctx, memorycore.StartSessionRequest{
		ID:        id,
		PersonaID: opts.PersonaID,
		Channel:   channel,
		Title:     stringPtr(title),
		StartedAt: started,
	})
	if err != nil {
		return runtimeError(stderr, "start session: %v", err)
	}
	return outputSession(stdout, session, opts)
}

func runEndSession(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("end-session", stderr)
	var opts commonOptions
	var sessionID, summary, summaryFile, endedAt string
	addCommonFlags(fs, &opts, formatText)
	fs.StringVar(&sessionID, "session", "", "session id")
	fs.StringVar(&summary, "summary", "", "session summary")
	fs.StringVar(&summaryFile, "summary-file", "", "session summary file")
	fs.StringVar(&endedAt, "ended-at", "", "RFC3339 end time")
	if !parseFlags(fs, args) {
		return 2
	}
	if !requireDB(stderr, fs, opts.DBPath) {
		return 2
	}
	if sessionID == "" {
		return usageError(stderr, fs, "--session is required")
	}
	if err := validateFormat(opts.Format, formatText, formatJSON, formatID); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	ended, err := parseOptionalTime(endedAt, "--ended-at")
	if err != nil {
		return usageError(stderr, fs, err.Error())
	}
	summaryValue, err := readOptionalTextValue(summary, summaryFile)
	if err != nil {
		return usageError(stderr, fs, "summary input: %v", err)
	}

	ctx := context.Background()
	svc, err := openService(ctx, opts)
	if err != nil {
		return runtimeError(stderr, "open memorycore: %v", err)
	}
	defer svc.Close()

	session, err := svc.EndSession(ctx, memorycore.EndSessionRequest{
		PersonaID: opts.PersonaID,
		SessionID: sessionID,
		EndedAt:   ended,
		Summary:   summaryValue,
	})
	if err != nil {
		return runtimeError(stderr, "end session: %v", err)
	}
	return outputSession(stdout, session, opts)
}

func outputSession(stdout io.Writer, session *memorycore.Session, opts commonOptions) int {
	switch opts.Format {
	case formatID:
		return idOutput(stdout, session.ID)
	case formatJSON:
		return writeJSON(stdout, session, opts.Pretty)
	default:
		fmt.Fprintf(stdout, "session_id=%s\n", session.ID)
		fmt.Fprintf(stdout, "persona_id=%s\n", session.PersonaID)
		fmt.Fprintf(stdout, "channel=%s\n", session.Channel)
		if session.Title != nil {
			fmt.Fprintf(stdout, "title=%s\n", *session.Title)
		}
		if session.Summary != nil {
			fmt.Fprintf(stdout, "summary=%s\n", *session.Summary)
		}
		fmt.Fprintf(stdout, "started_at=%s\n", session.StartedAt.Format("2006-01-02T15:04:05.999999999Z07:00"))
		if session.EndedAt != nil {
			fmt.Fprintf(stdout, "ended_at=%s\n", session.EndedAt.Format("2006-01-02T15:04:05.999999999Z07:00"))
		}
		return 0
	}
}
