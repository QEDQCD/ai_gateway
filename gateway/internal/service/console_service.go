package service

import (
	"context"
	"errors"
	"time"
)

var ErrConsoleServiceUnavailable = errors.New("console service unavailable")

type requestAuditMetadataContextKey struct{}

type RequestAuditMetadata struct {
	IPAddress string
	UserAgent string
}

type KeyMetric struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type TableRow struct {
	Columns []string `json:"columns"`
}

type OverviewPageData struct {
	Stats         []KeyMetric        `json:"stats"`
	RouteHealth   []TableRow         `json:"route_health"`
	TopModels     []TableRow         `json:"top_models"`
	RecentAlerts  []TableRow         `json:"recent_alerts"`
	AuditSnapshot []TableRow         `json:"audit_snapshot"`
	QuotaSummary  TenantQuotaSummary `json:"quota_summary"`
}

type ConsoleSystemStatus struct {
	ConsoleStage     string   `json:"console_stage"`
	RunMode          string   `json:"run_mode"`
	GatewayHealth    string   `json:"gateway_health"`
	QuotaProtection  string   `json:"quota_protection"`
	ConsoleEntry     string   `json:"console_entry"`
	GatewayAdminAPI  string   `json:"gateway_admin_api"`
	InternalServices []string `json:"internal_services"`
	HiddenModules    []string `json:"hidden_modules"`
}

type APIKeyItem struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Tenant              string   `json:"tenant"`
	Status              string   `json:"status"`
	Scopes              []string `json:"scopes"`
	LastUsedAt          string   `json:"last_used_at"`
	CreatedByUserID     string   `json:"created_by_user_id,omitempty"`
	ExpiresAt           string   `json:"expires_at,omitempty"`
	Revealable          bool     `json:"revealable,omitempty"`
	LegacyUnrecoverable bool     `json:"legacy_unrecoverable,omitempty"`
}

type APIKeysPageData struct {
	Items          []APIKeyItem `json:"items"`
	CredentialMode string       `json:"credential_mode"`
}

type CreateAPIKeyRequest struct {
	TenantID string   `json:"tenant_id"`
	Name     string   `json:"name"`
	Scopes   []string `json:"scopes"`
}

type RotateAPIKeyRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

type APIKeyMutationResult struct {
	Item   APIKeyItem `json:"item"`
	RawKey string     `json:"raw_key,omitempty"`
}

type APIKeySecretView struct {
	APIKeyID            string `json:"api_key_id"`
	MaskedKey           string `json:"masked_key"`
	FullKey             string `json:"full_key,omitempty"`
	Revealable          bool   `json:"revealable"`
	LegacyUnrecoverable bool   `json:"legacy_unrecoverable"`
	ExpiresAt           string `json:"expires_at,omitempty"`
}

type ApplicationItem struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	Name        string `json:"name"`
	CompanyName string `json:"company_name"`
	UseCase     string `json:"use_case"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
}

type ApplicationsPageData struct {
	Items []ApplicationItem `json:"items"`
}

type CreateApplicationRequest struct {
	Email            string `json:"email"`
	Name             string `json:"name"`
	CompanyName      string `json:"company_name"`
	UseCase          string `json:"use_case"`
	Password         string `json:"password"`
	CaptchaPassToken string `json:"captcha_pass_token"`
}

type ApproveApplicationRequest struct {
	ActorID  string `json:"actor_id"`
	Comment  string `json:"comment"`
	TenantID string `json:"tenant_id"`
}

type CaptchaChallenge struct {
	CaptchaID string `json:"captcha_id"`
	ImageData string `json:"image_data"`
	ExpiresAt string `json:"expires_at"`
}

type VerifyCaptchaRequest struct {
	CaptchaID   string `json:"captcha_id"`
	CaptchaCode string `json:"captcha_code"`
}

type CaptchaPassResult struct {
	CaptchaPassToken string `json:"captcha_pass_token"`
	ExpiresAt        string `json:"expires_at"`
}

type RejectApplicationRequest struct {
	ActorID string `json:"actor_id"`
	Comment string `json:"comment"`
}

type ApplicationMutationResult struct {
	Item ApplicationItem `json:"item"`
}

type RouteMetric struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type RouteItem struct {
	RequestedModel string `json:"requested_model"`
	RouteLabel     string `json:"route_label"`
	Credential     string `json:"credential"`
	Latency        string `json:"latency"`
	Status         string `json:"status"`
}

type RoutesPageData struct {
	Stats         []RouteMetric `json:"stats"`
	Items         []RouteItem   `json:"items"`
	PolicySummary []string      `json:"policy_summary"`
}

type PlaygroundPageData struct {
	AvailableModels []string               `json:"available_models"`
	LastRun         *PlaygroundRunResponse `json:"last_run,omitempty"`
}

type PlaygroundRunRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type PlaygroundRunResponse struct {
	RouteLabel  string `json:"route_label"`
	Endpoint    string `json:"endpoint"`
	Latency     string `json:"latency"`
	Status      string `json:"status"`
	Response    string `json:"response"`
	PlatformKey string `json:"platform_key"`
}

type AuditSummary struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

type AuditMetric struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type AuditEvent struct {
	Time   string `json:"time"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type AuditItem struct {
	Time          string `json:"time"`
	Tenant        string `json:"tenant"`
	Endpoint      string `json:"endpoint"`
	RequestModel  string `json:"request_model"`
	UpstreamModel string `json:"upstream_model"`
	Status        string `json:"status"`
	RouteLabel    string `json:"route_label"`
	Latency       string `json:"latency"`
	UsageSource   string `json:"usage_source"`
}

type AuditPageData struct {
	Metrics   []AuditMetric  `json:"metrics"`
	Events    []AuditEvent   `json:"events"`
	Items     []AuditItem    `json:"items"`
	Summaries []AuditSummary `json:"summaries"`
}

type UsageQuery struct {
	From             time.Time `json:"from"`
	To               time.Time `json:"to"`
	Window           string    `json:"window"`
	TenantID         string    `json:"tenant_id"`
	PlatformAPIKeyID string    `json:"platform_api_key_id"`
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	RouteID          string    `json:"route_id"`
	RequestPath      string    `json:"request_path"`
	Status           string    `json:"status"`
	ErrorCategory    string    `json:"error_category"`
	UsageSource      string    `json:"usage_source"`
	Limit            int       `json:"limit"`
	Offset           int       `json:"offset"`
}

type UsageOverviewData struct {
	TotalRequests  int64  `json:"total_requests"`
	SuccessRate    string `json:"success_rate"`
	TotalTokens    string `json:"total_tokens"`
	AverageLatency string `json:"average_latency"`
	EstimatedShare string `json:"estimated_share"`
}

type UsageTrendPoint struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type UsageTrendData struct {
	Requests []UsageTrendPoint `json:"requests"`
	Tokens   []UsageTrendPoint `json:"tokens"`
	Success  []UsageTrendPoint `json:"success"`
}

type UsageLatencyCell struct {
	BucketLabel string `json:"bucket_label"`
	Latency     string `json:"latency"`
	Status      string `json:"status"`
	Requests    string `json:"requests"`
}

type UsageLatencyLane struct {
	Model          string             `json:"model"`
	RouteLabel     string             `json:"route_label"`
	SuccessRate    string             `json:"success_rate"`
	AverageLatency string             `json:"average_latency"`
	Cells          []UsageLatencyCell `json:"cells"`
}

type UsageLatencyWallData struct {
	WindowLabel string             `json:"window_label"`
	Buckets     []string           `json:"buckets"`
	Lanes       []UsageLatencyLane `json:"lanes"`
}

type UsageFailureBucket struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type UsageFailureData struct {
	Breakdown    []UsageFailureBucket `json:"breakdown"`
	RecentEvents []string             `json:"recent_events"`
}

type UsageRequestItem struct {
	RequestID   string `json:"request_id"`
	Tenant      string `json:"tenant"`
	Endpoint    string `json:"endpoint"`
	Model       string `json:"model"`
	Status      string `json:"status"`
	TotalTokens string `json:"total_tokens"`
	Latency     string `json:"latency"`
	UsageSource string `json:"usage_source"`
}

type UsageRequestsPageData struct {
	Items  []UsageRequestItem `json:"items"`
	Total  int64              `json:"total"`
	Limit  int                `json:"limit"`
	Offset int                `json:"offset"`
}

type ConsoleService interface {
	Overview(ctx context.Context) (OverviewPageData, error)
	SystemStatus(ctx context.Context) (ConsoleSystemStatus, error)
	Applications(ctx context.Context) (ApplicationsPageData, error)
	CreateApplication(ctx context.Context, req CreateApplicationRequest) (ApplicationMutationResult, error)
	ApproveApplication(ctx context.Context, id string, req ApproveApplicationRequest) (ApplicationMutationResult, error)
	RejectApplication(ctx context.Context, id string, req RejectApplicationRequest) (ApplicationMutationResult, error)
	APIKeys(ctx context.Context) (APIKeysPageData, error)
	CreateAPIKey(ctx context.Context, req CreateAPIKeyRequest) (APIKeyMutationResult, error)
	RotateAPIKey(ctx context.Context, id string, req RotateAPIKeyRequest) (APIKeyMutationResult, error)
	DeactivateAPIKey(ctx context.Context, id string) (APIKeyMutationResult, error)
	DeleteAPIKey(ctx context.Context, id string) (APIKeyMutationResult, error)
	RevealAPIKeySecret(ctx context.Context, id string) (APIKeySecretView, error)
	CopyAPIKeySecret(ctx context.Context, id string, ip string, userAgent string) (APIKeySecretView, error)
	Routes(ctx context.Context) (RoutesPageData, error)
	Playground(ctx context.Context) (PlaygroundPageData, error)
	RunPlayground(ctx context.Context, req PlaygroundRunRequest) (PlaygroundRunResponse, error)
	Audit(ctx context.Context) (AuditPageData, error)
	UsageOverview(ctx context.Context, query UsageQuery) (UsageOverviewData, error)
	UsageTrends(ctx context.Context, query UsageQuery) (UsageTrendData, error)
	UsageLatencyWall(ctx context.Context, query UsageQuery) (UsageLatencyWallData, error)
	UsageFailures(ctx context.Context, query UsageQuery) (UsageFailureData, error)
	UsageRequests(ctx context.Context, query UsageQuery) (UsageRequestsPageData, error)
}

type unavailableConsoleService struct{}

func NewUnavailableConsoleService() ConsoleService {
	return unavailableConsoleService{}
}

func ContextWithRequestAuditMetadata(ctx context.Context, ip string, userAgent string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, requestAuditMetadataContextKey{}, RequestAuditMetadata{
		IPAddress: ip,
		UserAgent: userAgent,
	})
}

func RequestAuditMetadataFromContext(ctx context.Context) RequestAuditMetadata {
	if ctx == nil {
		return RequestAuditMetadata{}
	}
	metadata, ok := ctx.Value(requestAuditMetadataContextKey{}).(RequestAuditMetadata)
	if !ok {
		return RequestAuditMetadata{}
	}
	return metadata
}

func (unavailableConsoleService) Overview(context.Context) (OverviewPageData, error) {
	return OverviewPageData{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) SystemStatus(context.Context) (ConsoleSystemStatus, error) {
	return ConsoleSystemStatus{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) Applications(context.Context) (ApplicationsPageData, error) {
	return ApplicationsPageData{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) CreateApplication(context.Context, CreateApplicationRequest) (ApplicationMutationResult, error) {
	return ApplicationMutationResult{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) ApproveApplication(context.Context, string, ApproveApplicationRequest) (ApplicationMutationResult, error) {
	return ApplicationMutationResult{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) RejectApplication(context.Context, string, RejectApplicationRequest) (ApplicationMutationResult, error) {
	return ApplicationMutationResult{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) APIKeys(context.Context) (APIKeysPageData, error) {
	return APIKeysPageData{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) CreateAPIKey(context.Context, CreateAPIKeyRequest) (APIKeyMutationResult, error) {
	return APIKeyMutationResult{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) RotateAPIKey(context.Context, string, RotateAPIKeyRequest) (APIKeyMutationResult, error) {
	return APIKeyMutationResult{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) DeactivateAPIKey(context.Context, string) (APIKeyMutationResult, error) {
	return APIKeyMutationResult{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) DeleteAPIKey(context.Context, string) (APIKeyMutationResult, error) {
	return APIKeyMutationResult{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) RevealAPIKeySecret(context.Context, string) (APIKeySecretView, error) {
	return APIKeySecretView{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) CopyAPIKeySecret(context.Context, string, string, string) (APIKeySecretView, error) {
	return APIKeySecretView{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) Routes(context.Context) (RoutesPageData, error) {
	return RoutesPageData{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) Playground(context.Context) (PlaygroundPageData, error) {
	return PlaygroundPageData{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) RunPlayground(context.Context, PlaygroundRunRequest) (PlaygroundRunResponse, error) {
	return PlaygroundRunResponse{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) Audit(context.Context) (AuditPageData, error) {
	return AuditPageData{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) UsageOverview(context.Context, UsageQuery) (UsageOverviewData, error) {
	return UsageOverviewData{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) UsageTrends(context.Context, UsageQuery) (UsageTrendData, error) {
	return UsageTrendData{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) UsageLatencyWall(context.Context, UsageQuery) (UsageLatencyWallData, error) {
	return UsageLatencyWallData{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) UsageFailures(context.Context, UsageQuery) (UsageFailureData, error) {
	return UsageFailureData{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) UsageRequests(context.Context, UsageQuery) (UsageRequestsPageData, error) {
	return UsageRequestsPageData{}, ErrConsoleServiceUnavailable
}
