package memorycore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/longyisang/emoagent-memorycore/internal/core"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
	"strings"
)

func (s *service) Forget(ctx context.Context, req ForgetRequest) (*ForgetResult, error) {
	personaID := defaultString(req.PersonaID, s.persona)
	if err := validateForgetRequest(req); err != nil {
		return nil, err
	}
	result, err := s.forget.Forget(ctx, memsqlite.ForgetRequest{
		PersonaID:  personaID,
		Actor:      req.Actor,
		ReasonCode: req.ReasonCode,
		Level:      req.Level,
		Target: memsqlite.ForgetTarget{
			ScopeMode: req.Target.ScopeMode,
			NodeType:  core.NodeType(req.Target.NodeType),
			NodeID:    req.Target.NodeID,
		},
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s %s", ErrNotFound, req.Target.NodeType, req.Target.NodeID)
	}
	if err != nil {
		return nil, err
	}
	return forgetResultFromStore(result), nil
}

func validateForgetRequest(req ForgetRequest) error {
	if req.Target.ScopeMode != ForgetScopeExactNode {
		return fmt.Errorf("%w: ScopeMode must be exact_node", ErrInvalidRequest)
	}
	if strings.TrimSpace(req.Target.NodeID) == "" {
		return fmt.Errorf("%w: NodeID is required", ErrInvalidRequest)
	}
	switch req.Actor {
	case ForgetActorUser, ForgetActorSystem, ForgetActorAdmin:
	default:
		return fmt.Errorf("%w: invalid Actor", ErrInvalidRequest)
	}
	switch req.ReasonCode {
	case ForgetReasonUserRequested, ForgetReasonRetentionPolicy, ForgetReasonSafety, ForgetReasonAdminPolicy:
	default:
		return fmt.Errorf("%w: invalid ReasonCode", ErrInvalidRequest)
	}
	switch req.Level {
	case ForgetLevelSoft, ForgetLevelHard:
		if req.Target.NodeType != ForgetNodeFact {
			return fmt.Errorf("%w: %s only supports fact targets", ErrInvalidRequest, req.Level)
		}
	case ForgetLevelSourceRedact:
		if req.Target.NodeType != ForgetNodeEpisode {
			return fmt.Errorf("%w: source_redact only supports episode targets", ErrInvalidRequest)
		}
	case ForgetLevelPurge:
		if req.Target.NodeType != ForgetNodeFact && req.Target.NodeType != ForgetNodeEpisode {
			return fmt.Errorf("%w: purge only supports fact or episode targets", ErrInvalidRequest)
		}
	default:
		return fmt.Errorf("%w: invalid Level", ErrInvalidRequest)
	}
	return nil
}
