package main

import (
	"context"
	"fmt"
	"io"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func runEnsureEntity(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("ensure-entity", stderr)
	var opts commonOptions
	var id, name, entityType, description, visibility, sensitivity string
	var searchable bool
	var aliases stringList
	addCommonFlags(fs, &opts, formatText)
	fs.StringVar(&id, "id", "", "entity id")
	fs.StringVar(&name, "name", "", "canonical name")
	fs.StringVar(&entityType, "type", memorycore.EntityTypeConcept, "entity type")
	fs.StringVar(&description, "description", "", "description")
	fs.Var(&aliases, "alias", "entity alias; repeatable")
	fs.StringVar(&visibility, "visibility", memorycore.VisibilityVisible, "visibility status")
	fs.StringVar(&sensitivity, "sensitivity", memorycore.SensitivityNormal, "sensitivity level")
	fs.BoolVar(&searchable, "searchable", true, "searchable")
	if !parseFlags(fs, args) {
		return 2
	}
	if !requireDB(stderr, fs, opts.DBPath) {
		return 2
	}
	if name == "" {
		return usageError(stderr, fs, "--name is required")
	}
	if err := validateFormat(opts.Format, formatText, formatJSON, formatID); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateOneOf("--type", entityType, memorycore.EntityTypeUser, memorycore.EntityTypeAgent, memorycore.EntityTypePerson, memorycore.EntityTypePlace, memorycore.EntityTypeOrg, memorycore.EntityTypeConcept, memorycore.EntityTypeObject, memorycore.EntityTypeEventTopic); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateOneOf("--visibility", visibility, memorycore.VisibilityVisible, memorycore.VisibilityHidden, memorycore.VisibilityForgotten, memorycore.VisibilityPurged); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateOneOf("--sensitivity", sensitivity, memorycore.SensitivityNormal, memorycore.SensitivitySensitive, memorycore.SensitivityHighlySensitive); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	inputAliases := make([]memorycore.EntityAliasInput, 0, len(aliases))
	for _, alias := range aliases {
		inputAliases = append(inputAliases, memorycore.EntityAliasInput{
			Alias:      alias,
			AliasType:  memorycore.AliasTypeSurface,
			Confidence: 1,
		})
	}

	ctx := context.Background()
	svc, err := openService(ctx, opts)
	if err != nil {
		return runtimeError(stderr, "open memorycore: %v", err)
	}
	defer svc.Close()

	entity, err := svc.EnsureEntity(ctx, memorycore.EnsureEntityRequest{
		ID:               id,
		PersonaID:        opts.PersonaID,
		CanonicalName:    name,
		EntityType:       entityType,
		Description:      stringPtr(description),
		VisibilityStatus: visibility,
		SensitivityLevel: sensitivity,
		Searchable:       boolPtr(searchable),
		Aliases:          inputAliases,
	})
	if err != nil {
		return runtimeError(stderr, "ensure entity: %v", err)
	}
	return outputEntity(stdout, entity, opts)
}

func runAddAlias(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("add-alias", stderr)
	var opts commonOptions
	var id, entityID, alias, aliasType, sourceEpisode string
	var confidence float64
	addCommonFlags(fs, &opts, formatText)
	fs.StringVar(&id, "id", "", "alias id")
	fs.StringVar(&entityID, "entity", "", "entity id")
	fs.StringVar(&alias, "alias", "", "alias")
	fs.StringVar(&aliasType, "alias-type", memorycore.AliasTypeSurface, "alias type")
	fs.Float64Var(&confidence, "confidence", 1, "confidence")
	fs.StringVar(&sourceEpisode, "source-episode", "", "source episode id")
	if !parseFlags(fs, args) {
		return 2
	}
	if !requireDB(stderr, fs, opts.DBPath) {
		return 2
	}
	if entityID == "" {
		return usageError(stderr, fs, "--entity is required")
	}
	if alias == "" {
		return usageError(stderr, fs, "--alias is required")
	}
	if err := validateFormat(opts.Format, formatText, formatJSON, formatID); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateOneOf("--alias-type", aliasType, memorycore.AliasTypeSurface, memorycore.AliasTypeNickname, memorycore.AliasTypeTranslation, memorycore.AliasTypeAbbreviation); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateFloatRange("--confidence", confidence, 0, 1); err != nil {
		return usageError(stderr, fs, err.Error())
	}

	ctx := context.Background()
	svc, err := openService(ctx, opts)
	if err != nil {
		return runtimeError(stderr, "open memorycore: %v", err)
	}
	defer svc.Close()

	created, err := svc.AddEntityAlias(ctx, memorycore.AddEntityAliasRequest{
		ID:              id,
		PersonaID:       opts.PersonaID,
		EntityID:        entityID,
		Alias:           alias,
		AliasType:       aliasType,
		Confidence:      confidence,
		SourceEpisodeID: stringPtr(sourceEpisode),
	})
	if err != nil {
		return runtimeError(stderr, "add alias: %v", err)
	}
	switch opts.Format {
	case formatID:
		return idOutput(stdout, created.ID)
	case formatJSON:
		return writeJSON(stdout, created, opts.Pretty)
	default:
		fmt.Fprintf(stdout, "alias_id=%s\n", created.ID)
		fmt.Fprintf(stdout, "entity_id=%s\n", created.EntityID)
		fmt.Fprintf(stdout, "alias=%s\n", created.Alias)
		return 0
	}
}

func outputEntity(stdout io.Writer, entity *memorycore.Entity, opts commonOptions) int {
	switch opts.Format {
	case formatID:
		return idOutput(stdout, entity.ID)
	case formatJSON:
		return writeJSON(stdout, entity, opts.Pretty)
	default:
		fmt.Fprintf(stdout, "entity_id=%s\n", entity.ID)
		fmt.Fprintf(stdout, "canonical_name=%s\n", entity.CanonicalName)
		fmt.Fprintf(stdout, "entity_type=%s\n", entity.EntityType)
		fmt.Fprintf(stdout, "visibility_status=%s\n", entity.VisibilityStatus)
		fmt.Fprintf(stdout, "searchable=%s\n", boolText(entity.Searchable))
		return 0
	}
}
