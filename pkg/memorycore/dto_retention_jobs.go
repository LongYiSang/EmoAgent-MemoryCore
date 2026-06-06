package memorycore

import "time"

type RetentionJobName string

const (
	RetentionJobDailyTTLExpiry     RetentionJobName = "daily_ttl_expiry"
	RetentionJobMonthlyDeepArchive RetentionJobName = "monthly_deep_archive"
)

type RunRetentionJobsRequest struct {
	PersonaID            string
	Now                  time.Time
	DryRun               bool
	Jobs                 []RetentionJobName
	DeepArchiveAfterDays int
}

type RunRetentionJobsResult struct {
	Jobs      []RetentionJobResult
	Retention RunRetentionResult
}

type RetentionJobResult struct {
	Name    RetentionJobName
	Skipped bool
	Reason  string
}
