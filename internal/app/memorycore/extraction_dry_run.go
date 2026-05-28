package memorycore

func DryRun(req ExtractionRequest, resp ExtractionResponse, gate ExtractionGateResult) ExtractionDryRunResult {
	pinnedTargets := pinTargets(gate, resp)
	result := ExtractionDryRunResult{
		RequestID:              req.RequestID,
		PersonaID:              req.PersonaID,
		GateResult:             gate,
		EntityPreview:          []EntityApplyPreview{},
		FactPreview:            []FactApplyPreview{},
		RoutedDeletionIntents:  []DeletionIntentRoute{},
		RoutedPinIntents:       []PinIntentRoute{},
		NotAppliedLinks:        []LinkCandidatePreview{},
		NotAppliedAffectEvents: []AffectEventPreview{},
		Summary: DryRunSummary{
			AcceptedFacts: gate.Summary.AcceptedFactCount,
			NeedsReview:   gate.Summary.NeedsReviewCount,
			Rejected:      gate.Summary.RejectedCount,
			Routed:        gate.Summary.RoutedCount,
			NotApplied:    gate.Summary.NotAppliedCount,
		},
	}
	for _, d := range gate.EntityDecisions {
		result.EntityPreview = append(result.EntityPreview, EntityApplyPreview{
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
		result.FactPreview = append(result.FactPreview, FactApplyPreview{
			CandidateID:   fact.CandidateID,
			Predicate:     fact.Predicate,
			Decision:      d.Decision,
			ReasonCodes:   append([]string(nil), d.ReasonCodes...),
			Pinned:        fact.Pinned || pinned,
			UserRequested: fact.UserRequested || pinned,
		})
	}
	for _, d := range gate.DeletionIntentDecisions {
		result.RoutedDeletionIntents = append(result.RoutedDeletionIntents, DeletionIntentRoute{
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
		result.RoutedPinIntents = append(result.RoutedPinIntents, PinIntentRoute{
			CandidateID:       d.CandidateID,
			TargetCandidateID: target,
			Decision:          d.Decision,
		})
	}
	for _, link := range resp.Links {
		if d, ok := decisionByID(gate.LinkDecisions, link.CandidateID); ok {
			result.NotAppliedLinks = append(result.NotAppliedLinks, LinkCandidatePreview{
				CandidateID: link.CandidateID,
				LinkType:    link.LinkType,
				Decision:    d.Decision,
			})
		}
	}
	for _, event := range resp.AffectEvents {
		if d, ok := decisionByID(gate.AffectEventDecisions, event.CandidateID); ok {
			result.NotAppliedAffectEvents = append(result.NotAppliedAffectEvents, AffectEventPreview{
				CandidateID: event.CandidateID,
				Scope:       event.Scope,
				Decision:    d.Decision,
			})
		}
	}
	return result
}

func pinTargets(gate ExtractionGateResult, resp ExtractionResponse) map[string]struct{} {
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
