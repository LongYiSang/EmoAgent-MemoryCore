package extractionruntime

import (
	"context"
	"database/sql"

	appcore "github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

type (
	AuditStore                      = appcore.AuditStore
	BuildRequestOptions             = appcore.BuildRequestOptions
	MockLLM                         = appcore.MockLLM
	OpenAICompatibleLLM             = appcore.OpenAICompatibleLLM
	OpenAICompatibleOptions         = appcore.OpenAICompatibleOptions
	OpenAICompatibleThinkingOptions = appcore.OpenAICompatibleThinkingOptions
	PromptVersions                  = appcore.PromptVersions
	Runner                          = appcore.Runner
	RunnerOptions                   = appcore.RunnerOptions
	SQLiteAuditStore                = appcore.SQLiteAuditStore
)

func BuildRequest(ctx context.Context, db *sql.DB, opts BuildRequestOptions) (memorycore.ExtractionRequest, error) {
	return appcore.BuildRequest(ctx, db, opts)
}

func NewRunner(opts RunnerOptions) *Runner {
	return appcore.NewRunner(opts)
}

func NewSQLiteAuditStore(db *sql.DB) *SQLiteAuditStore {
	return appcore.NewSQLiteAuditStore(db)
}

func NewDeterministicMockLLM() *MockLLM {
	return appcore.NewDeterministicMockLLM()
}

func NewOpenAICompatibleLLM(opts OpenAICompatibleOptions) *OpenAICompatibleLLM {
	return appcore.NewOpenAICompatibleLLM(opts)
}
