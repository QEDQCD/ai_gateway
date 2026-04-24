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
