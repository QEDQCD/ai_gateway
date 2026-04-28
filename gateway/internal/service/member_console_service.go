package service

import (
	"context"
)

type memberConsolePrincipalContextKey struct{}

type MemberOverviewPageData struct {
	TenantID      string `json:"tenant_id"`
	TenantName    string `json:"tenant_name"`
	ActiveAPIKeys int    `json:"active_api_keys"`
}

type MemberAPIKeyItem struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Tenant              string   `json:"tenant"`
	Status              string   `json:"status"`
	Scopes              []string `json:"scopes"`
	LastUsedAt          string   `json:"last_used_at"`
	OwnerUserID         string   `json:"owner_user_id"`
	CreatedByUserID     string   `json:"created_by_user_id,omitempty"`
	ExpiresAt           string   `json:"expires_at,omitempty"`
	Revealable          bool     `json:"revealable,omitempty"`
	LegacyUnrecoverable bool     `json:"legacy_unrecoverable,omitempty"`
}

type MemberAPIKeysPageData struct {
	Items []MemberAPIKeyItem `json:"items"`
}

type MemberFailurePageData struct {
	Breakdown    []UsageFailureBucket `json:"breakdown"`
	RecentEvents []string             `json:"recent_events"`
}

type MemberAuditEventItem struct {
	Time       string `json:"time"`
	EventType  string `json:"event_type"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Detail     string `json:"detail"`
}

type MemberAuditPageData struct {
	Items []MemberAuditEventItem `json:"items"`
}

type MemberConsoleService interface {
	Overview(ctx context.Context) (MemberOverviewPageData, error)
	APIKeys(ctx context.Context) (MemberAPIKeysPageData, error)
	CreateAPIKey(ctx context.Context, req CreateAPIKeyRequest) (APIKeyMutationResult, error)
	RotateAPIKey(ctx context.Context, id string, req RotateAPIKeyRequest) (APIKeyMutationResult, error)
	DeactivateAPIKey(ctx context.Context, id string) (APIKeyMutationResult, error)
	RevealAPIKeySecret(ctx context.Context, id string) (APIKeySecretView, error)
	CopyAPIKeySecret(ctx context.Context, id string, ip string, userAgent string) (APIKeySecretView, error)
	UsageOverview(ctx context.Context, query UsageQuery) (UsageOverviewData, error)
	UsageRequests(ctx context.Context, query UsageQuery) (UsageRequestsPageData, error)
	Failures(ctx context.Context, query UsageQuery) (MemberFailurePageData, error)
	AuditEvents(ctx context.Context) (MemberAuditPageData, error)
}

type unavailableMemberConsoleService struct{}

func NewUnavailableMemberConsoleService() MemberConsoleService {
	return unavailableMemberConsoleService{}
}

func ContextWithConsolePrincipal(ctx context.Context, principal ConsolePrincipal) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, memberConsolePrincipalContextKey{}, principal)
}

func ConsolePrincipalFromContext(ctx context.Context) (ConsolePrincipal, bool) {
	if ctx == nil {
		return ConsolePrincipal{}, false
	}
	principal, ok := ctx.Value(memberConsolePrincipalContextKey{}).(ConsolePrincipal)
	if !ok {
		return ConsolePrincipal{}, false
	}
	return principal, true
}

func (unavailableMemberConsoleService) Overview(context.Context) (MemberOverviewPageData, error) {
	return MemberOverviewPageData{}, ErrConsoleServiceUnavailable
}

func (unavailableMemberConsoleService) APIKeys(context.Context) (MemberAPIKeysPageData, error) {
	return MemberAPIKeysPageData{}, ErrConsoleServiceUnavailable
}

func (unavailableMemberConsoleService) CreateAPIKey(context.Context, CreateAPIKeyRequest) (APIKeyMutationResult, error) {
	return APIKeyMutationResult{}, ErrConsoleServiceUnavailable
}

func (unavailableMemberConsoleService) RotateAPIKey(context.Context, string, RotateAPIKeyRequest) (APIKeyMutationResult, error) {
	return APIKeyMutationResult{}, ErrConsoleServiceUnavailable
}

func (unavailableMemberConsoleService) DeactivateAPIKey(context.Context, string) (APIKeyMutationResult, error) {
	return APIKeyMutationResult{}, ErrConsoleServiceUnavailable
}

func (unavailableMemberConsoleService) RevealAPIKeySecret(context.Context, string) (APIKeySecretView, error) {
	return APIKeySecretView{}, ErrConsoleServiceUnavailable
}

func (unavailableMemberConsoleService) CopyAPIKeySecret(context.Context, string, string, string) (APIKeySecretView, error) {
	return APIKeySecretView{}, ErrConsoleServiceUnavailable
}

func (unavailableMemberConsoleService) UsageOverview(context.Context, UsageQuery) (UsageOverviewData, error) {
	return UsageOverviewData{}, ErrConsoleServiceUnavailable
}

func (unavailableMemberConsoleService) UsageRequests(context.Context, UsageQuery) (UsageRequestsPageData, error) {
	return UsageRequestsPageData{}, ErrConsoleServiceUnavailable
}

func (unavailableMemberConsoleService) Failures(context.Context, UsageQuery) (MemberFailurePageData, error) {
	return MemberFailurePageData{}, ErrConsoleServiceUnavailable
}

func (unavailableMemberConsoleService) AuditEvents(context.Context) (MemberAuditPageData, error) {
	return MemberAuditPageData{}, ErrConsoleServiceUnavailable
}
