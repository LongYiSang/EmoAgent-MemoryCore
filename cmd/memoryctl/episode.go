package main

import (
	"context"
	"fmt"
	"io"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func runAppendEpisode(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("append-episode", stderr)
	var opts commonOptions
	var id, sessionID, role, content, contentFile, occurredAt, sourceType, sourceRef, visibility, sensitivity string
	var searchable bool
	addCommonFlags(fs, &opts, formatText)
	fs.StringVar(&id, "id", "", "episode id")
	fs.StringVar(&sessionID, "session", "", "session id")
	fs.StringVar(&role, "role", memorycore.RoleUser, "episode role")
	fs.StringVar(&content, "content", "", "episode content")
	fs.StringVar(&contentFile, "content-file", "", "episode content file")
	fs.StringVar(&occurredAt, "occurred-at", "", "RFC3339 occurrence time")
	fs.StringVar(&sourceType, "source-type", memorycore.SourceTypeChat, "source type")
	fs.StringVar(&sourceRef, "source-ref", "", "source reference")
	fs.StringVar(&visibility, "visibility", memorycore.VisibilityVisible, "visibility status")
	fs.StringVar(&sensitivity, "sensitivity", memorycore.SensitivityNormal, "sensitivity level")
	fs.BoolVar(&searchable, "searchable", true, "searchable")
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
	if err := validateOneOf("--role", role, memorycore.RoleUser, memorycore.RoleAssistant, memorycore.RoleSystem, memorycore.RoleToolSummary, memorycore.RoleWorkReport); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateOneOf("--source-type", sourceType, memorycore.SourceTypeChat, memorycore.SourceTypeWorkCandidate, memorycore.SourceTypePlugin, memorycore.SourceTypeSystem, memorycore.SourceTypeImported); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateOneOf("--visibility", visibility, memorycore.VisibilityVisible, memorycore.VisibilityHidden, memorycore.VisibilityRedacted, memorycore.VisibilityPurged); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateOneOf("--sensitivity", sensitivity, memorycore.SensitivityNormal, memorycore.SensitivitySensitive, memorycore.SensitivityHighlySensitive); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	body, err := readTextValue(content, contentFile)
	if err != nil {
		return usageError(stderr, fs, "content input: %v", err)
	}
	occurred, err := parseOptionalTime(occurredAt, "--occurred-at")
	if err != nil {
		return usageError(stderr, fs, err.Error())
	}

	ctx := context.Background()
	svc, err := openService(ctx, opts)
	if err != nil {
		return runtimeError(stderr, "open memorycore: %v", err)
	}
	defer svc.Close()

	episode, err := svc.AppendEpisode(ctx, memorycore.AppendEpisodeRequest{
		ID:               id,
		PersonaID:        opts.PersonaID,
		SessionID:        sessionID,
		Role:             role,
		Content:          body,
		OccurredAt:       occurred,
		SourceType:       sourceType,
		SourceRef:        stringPtr(sourceRef),
		VisibilityStatus: visibility,
		SensitivityLevel: sensitivity,
		Searchable:       boolPtr(searchable),
	})
	if err != nil {
		return runtimeError(stderr, "append episode: %v", err)
	}
	switch opts.Format {
	case formatID:
		return idOutput(stdout, episode.ID)
	case formatJSON:
		return writeJSON(stdout, episode, opts.Pretty)
	default:
		fmt.Fprintf(stdout, "episode_id=%s\n", episode.ID)
		fmt.Fprintf(stdout, "session_id=%s\n", episode.SessionID)
		fmt.Fprintf(stdout, "role=%s\n", episode.Role)
		fmt.Fprintf(stdout, "visibility_status=%s\n", episode.VisibilityStatus)
		fmt.Fprintf(stdout, "searchable=%s\n", boolText(episode.Searchable))
		return 0
	}
}
