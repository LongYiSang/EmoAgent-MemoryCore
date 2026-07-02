package memorycore

const extractionAdmissionPolicyVersion = "phase2d.admission.v1"

type AdmissionAction string

const (
	AdmissionAccept      AdmissionAction = "accept"
	AdmissionReject      AdmissionAction = "reject"
	AdmissionNeedsReview AdmissionAction = "needs_review"
	AdmissionSkip        AdmissionAction = "skip"
	AdmissionRouteOnly   AdmissionAction = "route_only"
	AdmissionNotApplied  AdmissionAction = "not_applied"
)

const (
	ReasonHypotheticalScenario         = "hypothetical_scenario"
	ReasonAssistantSpeculation         = "assistant_speculation_not_user_fact"
	ReasonAssistantSuggestion          = "assistant_suggestion_not_user_fact"
	ReasonToolNoise                    = "tool_noise"
	ReasonWorkLogNoise                 = "work_log_noise"
	ReasonDoNotRemember                = "do_not_remember"
	ReasonDoNotMention                 = "do_not_mention"
	ReasonDeletionIntentOnly           = "deletion_intent_only"
	ReasonCorrectionHintOnly           = "correction_hint_only"
	ReasonManualForgetFactRejected     = "manual_forget_fact_rejected"
	ReasonNoUserOwnedClaim             = "no_user_owned_claim"
	ReasonNoDurableValue               = "no_durable_value"
	ReasonWeakInference                = "weak_inference"
	ReasonSensitiveInference           = "sensitive_inference"
	ReasonEphemeralChitchat            = "ephemeral_chitchat"
	ReasonSourceEpisodeNotUserGrounded = "source_episode_not_user_grounded"
	ReasonNoWriteHint                  = "no_write_hint"
	ReasonModelRejected                = "model_rejected"
	ReasonUserAddressConfigBoundary    = "user_address_config_boundary"
)

type AdmissionDecision struct {
	CandidateID string
	Kind        string
	Action      AdmissionAction
	ReasonCodes []string
	Notes       string
}
