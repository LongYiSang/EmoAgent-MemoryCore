package memorycore

import appcore "github.com/longyisang/emoagent-memorycore/internal/app/memorycore"

type (
	ClassifyForgetConfirmationRequest      = appcore.ClassifyForgetConfirmationRequest
	ClassifyForgetConfirmationResult       = appcore.ClassifyForgetConfirmationResult
	ExecuteManualForgetOperationRequest    = appcore.ExecuteManualForgetOperationRequest
	ExecuteManualForgetOperationResult     = appcore.ExecuteManualForgetOperationResult
	ForgetCandidate                        = appcore.ForgetCandidate
	GetPendingManualForgetOperationRequest = appcore.GetPendingManualForgetOperationRequest
	ManualForgetDirectiveRequest           = appcore.ManualForgetDirectiveRequest
	ManualForgetDirectiveResult            = appcore.ManualForgetDirectiveResult
	ManualRuleHint                         = appcore.ManualRuleHint
	MemoryOperationAssistantGuidance       = appcore.MemoryOperationAssistantGuidance
	MemoryOperationLLMContext              = appcore.MemoryOperationLLMContext
	MemoryOperationSafeCandidate           = appcore.MemoryOperationSafeCandidate
	MemoryOperationVerifyContext           = appcore.MemoryOperationVerifyContext
	PendingManualForgetOperation           = appcore.PendingManualForgetOperation
	PlanManualForgetRequest                = appcore.PlanManualForgetRequest
	PlanManualForgetResult                 = appcore.PlanManualForgetResult
	RecentPromptMemoryRef                  = appcore.RecentPromptMemoryRef
)
