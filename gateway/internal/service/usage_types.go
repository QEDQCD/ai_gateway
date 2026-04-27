package service

type UsageStatus string

const (
	UsageStatusSuccess       UsageStatus = "success"
	UsageStatusFailed        UsageStatus = "failed"
	UsageStatusTimeout       UsageStatus = "timeout"
	UsageStatusRateLimited   UsageStatus = "rate_limited"
	UsageStatusAuthFailed    UsageStatus = "auth_failed"
	UsageStatusUpstreamError UsageStatus = "upstream_error"
)

func AllUsageStatuses() []string {
	return []string{
		string(UsageStatusSuccess),
		string(UsageStatusFailed),
		string(UsageStatusTimeout),
		string(UsageStatusRateLimited),
		string(UsageStatusAuthFailed),
		string(UsageStatusUpstreamError),
	}
}

func (s UsageStatus) Valid() bool {
	for _, valid := range AllUsageStatuses() {
		if string(s) == valid {
			return true
		}
	}
	return false
}

type UsageSource string

const (
	UsageSourceUpstream  UsageSource = "upstream"
	UsageSourceEstimated UsageSource = "estimated"
)

func AllUsageSources() []string {
	return []string{
		string(UsageSourceUpstream),
		string(UsageSourceEstimated),
	}
}

func (s UsageSource) Valid() bool {
	for _, valid := range AllUsageSources() {
		if string(s) == valid {
			return true
		}
	}
	return false
}

type UsageFailureStage string

const (
	UsageFailureStageRequest  UsageFailureStage = "request"
	UsageFailureStageUpstream UsageFailureStage = "upstream"
	UsageFailureStageResponse UsageFailureStage = "response"
	UsageFailureStagePublish  UsageFailureStage = "publish"
	UsageFailureStageInternal UsageFailureStage = "internal"
)

func AllUsageFailureStages() []string {
	return []string{
		string(UsageFailureStageRequest),
		string(UsageFailureStageUpstream),
		string(UsageFailureStageResponse),
		string(UsageFailureStagePublish),
		string(UsageFailureStageInternal),
	}
}

func (s UsageFailureStage) Valid() bool {
	for _, valid := range AllUsageFailureStages() {
		if string(s) == valid {
			return true
		}
	}
	return false
}

type UsageFailureCategory string

const (
	UsageFailureCategoryFailed         UsageFailureCategory = "failed"
	UsageFailureCategoryRateLimited    UsageFailureCategory = "rate_limited"
	UsageFailureCategoryAuthFailed     UsageFailureCategory = "auth_failed"
	UsageFailureCategoryTimeout        UsageFailureCategory = "timeout"
	UsageFailureCategoryUpstreamError  UsageFailureCategory = "upstream_error"
	UsageFailureCategoryPublishFailure UsageFailureCategory = "publish_failure"
	UsageFailureCategoryInternalError  UsageFailureCategory = "internal_error"
)

func AllUsageFailureCategories() []string {
	return []string{
		string(UsageFailureCategoryFailed),
		string(UsageFailureCategoryRateLimited),
		string(UsageFailureCategoryAuthFailed),
		string(UsageFailureCategoryTimeout),
		string(UsageFailureCategoryUpstreamError),
		string(UsageFailureCategoryPublishFailure),
		string(UsageFailureCategoryInternalError),
	}
}

func (s UsageFailureCategory) Valid() bool {
	for _, valid := range AllUsageFailureCategories() {
		if string(s) == valid {
			return true
		}
	}
	return false
}

type UsageFailureInput struct {
	RequestID        string
	RequestLogID     string
	TenantID         string
	UserID           string
	PlatformAPIKeyID string
	FailureStage     string
	ErrorCategory    string
	StatusCode       int
	Retryable        bool
	UserMessage      string
	InternalMessage  string
}
