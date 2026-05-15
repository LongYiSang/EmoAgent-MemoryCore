package extraction

import "github.com/longyisang/emoagent-memorycore/internal/app/memorycore"

func DryRun(req memorycore.ExtractionRequest, resp memorycore.ExtractionResponse, gate memorycore.ExtractionGateResult) memorycore.ExtractionDryRunResult {
	pinnedTargets := pinTargets(gate, resp)
	result := memorycore.ExtractionDryRunResult{
		RequestID:              req.RequestID,
		PersonaID:              req.PersonaID,
		GateResult:             gate,
		EntityPreview:          []memorycore.EntityApplyPreview{},
		FactPreview:            []memorycore.FactApplyPreview{},
		RoutedDeletionIntents:  []memorycore.DeletionIntentRoute{},
		RoutedPinIntents:       []memorycore.PinIntentRoute{},
		NotAppliedLinks:        []memorycore.LinkCandidatePreview{},
		NotAppliedAffectEvents: []memorycore.AffectEventPreview{},
		Summary: memorycore.DryRunSummary{
			AcceptedFacts: gate.Summary.AcceptedFactCount,
			NeedsReview:   gate.Summary.NeedsReviewCount,
			Rejected:      gate.Summary.RejectedCount,
			Routed:        gate.Summary.RoutedCount,
			NotApplied:    gate.Summary.NotAppliedCount,
		},
	}
	for _, d := range gate.EntityDecisions {
		result.EntityPreview = append(result.EntityPreview, memorycore.EntityApplyPreview{
			CandidateID: d.CandidateID,
			Action:      "resolve_or_ensure",
			Decision:    d.Decision,
		})
	}
	for _, fact := range resp.Facts {
		d, ok := decisionByID(gate.FactDecisions, fact.CandidateID)
		if !ok {
			continue
		}
		_, pinned := pinnedTargets[fact.CandidateID]
		result.FactPreview = append(result.FactPreview, memorycore.FactApplyPreview{
			CandidateID:   fact.CandidateID,
			Predicate:     fact.Predicate,
			Decision:      d.Decision,
			ReasonCodes:   append([]string(nil), d.ReasonCodes...),
			Pinned:        fact.Pinned || pinned,
			UserRequested: fact.UserRequested || pinned,
		})
	}
	for _, d := range gate.DeletionIntentDecisions {
		result.RoutedDeletionIntents = append(result.RoutedDeletionIntents, memorycore.DeletionIntentRoute{
			CandidateID: d.CandidateID,
			RouteTo:     "forget_manager",
			Decision:    d.Decision,
		})
	}
	for _, d := range gate.PinIntentDecisions {
		var target *string
		for _, intent := range resp.PinIntents {
			if intent.CandidateID == d.CandidateID {
				target = intent.TargetCandidateID
				break
			}
		}
		result.RoutedPinIntents = append(result.RoutedPinIntents, memorycore.PinIntentRoute{
			CandidateID:       d.CandidateID,
			TargetCandidateID: target,
			Decision:          d.Decision,
		})
	}
	for _, link := range resp.Links {
		if d, ok := decisionByID(gate.LinkDecisions, link.CandidateID); ok {
			result.NotAppliedLinks = append(result.NotAppliedLinks, memorycore.LinkCandidatePreview{
				CandidateID: link.CandidateID,
				LinkType:    link.LinkType,
				Decision:    d.Decision,
			})
		}
	}
	for _, event := range resp.AffectEvents {
		if d, ok := decisionByID(gate.AffectEventDecisions, event.CandidateID); ok {
			result.NotAppliedAffectEvents = append(result.NotAppliedAffectEvents, memorycore.AffectEventPreview{
				CandidateID: event.CandidateID,
				Scope:       event.Scope,
				Decision:    d.Decision,
			})
		}
	}
	return result
}

func pinTargets(gate memorycore.ExtractionGateResult, resp memorycore.ExtractionResponse) map[string]struct{} {
	targets := map[string]struct{}{}
	for _, intent := range resp.PinIntents {
		if intent.TargetCandidateID == nil {
			continue
		}
		d, ok := decisionByID(gate.PinIntentDecisions, intent.CandidateID)
		if ok && d.Decision == decisionRouteOnly {
			targets[*intent.TargetCandidateID] = struct{}{}
		}
	}
	return targets
}
