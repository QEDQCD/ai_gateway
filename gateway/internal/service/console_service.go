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
	Stats           []KeyMetric        `json:"stats"`
	PlatformMetrics []KeyMetric        `json:"platform_metrics"`
	TenantPosture   []TableRow         `json:"tenant_posture"`
	RouteHealth     []TableRow         `json:"route_health"`
	TopModels       []TableRow         `json:"top_models"`
	RecentAlerts    []TableRow         `json:"recent_alerts"`
	AuditSnapshot   []TableRow         `json:"audit_snapshot"`
	QuotaSummary    TenantQuotaSummary `json:"quota_summary"`
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
	ActorID            string   `json:"actor_id"`
	Comment            string   `json:"comment"`
	TenantID           string   `json:"tenant_id"`
	TokenLimit         int64    `json:"token_limit"`
	CostLimitMicroyuan int64    `json:"cost_limit_microyuan"`
	AllowedModels      []string `json:"allowed_models"`
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

type AccountDeletionApplicationItem struct {
	ID              string `json:"id"`
	UserID          string `json:"user_id"`
	TenantID        string `json:"tenant_id"`
	UserEmail       string `json:"user_email"`
	UserName        string `json:"user_name"`
	Reason          string `json:"reason"`
	Status          string `json:"status"`
	DisabledAPIKeys int    `json:"disabled_api_keys"`
	CreatedAt       string `json:"created_at"`
	ReviewedAt      string `json:"reviewed_at,omitempty"`
}

type AccountDeletionApplicationsPageData struct {
	Items []AccountDeletionApplicationItem `json:"items"`
}

type CreateAccountDeletionApplicationRequest struct {
	Reason string `json:"reason"`
}

type ReviewAccountDeletionApplicationRequest struct {
	ActorID string `json:"actor_id"`
	Comment string `json:"comment"`
}

type AccountDeletionApplicationMutationResult struct {
	Item AccountDeletionApplicationItem `json:"item"`
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
	ProviderGroup  string `json:"provider_group"`
}

type RoutesPageData struct {
	Stats         []RouteMetric `json:"stats"`
	Items         []RouteItem   `json:"items"`
	PolicySummary []string      `json:"policy_summary"`
}

type ProviderItem struct {
	ID              string   `json:"id"`
	Provider        string   `json:"provider"`
	DisplayName     string   `json:"display_name"`
	SupportedModels []string `json:"supported_models,omitempty"`
	BaseURL         string   `json:"base_url,omitempty"`
	CredentialMode  string   `json:"credential_mode"`
	SecretRef       string   `json:"secret_ref"`
	Status          string   `json:"status"`
}

type CreateProviderRequest struct {
	Provider       string `json:"provider"`
	DisplayName    string `json:"display_name"`
	BaseURL        string `json:"base_url"`
	CredentialMode string `json:"credential_mode"`
	SecretRef      string `json:"secret_ref"`
	APIKey         string `json:"api_key"`
}

type ProviderMutationResult struct {
	Item ProviderItem `json:"item"`
}

type ProviderModelItem struct {
	ID                   string `json:"id,omitempty"`
	RequestedModel       string `json:"requested_model"`
	Provider             string `json:"provider"`
	ProviderCredentialID string `json:"provider_credential_id"`
	RouteLabel           string `json:"route_label"`
	HealthStatus         string `json:"health_status"`
	LatencyMS            int64  `json:"latency_ms"`
	RequestMode          string `json:"request_mode"`
}

type ProviderModelsPageData struct {
	Providers []ProviderItem      `json:"providers"`
	Models    []ProviderModelItem `json:"models"`
}

type CreateProviderModelRequest struct {
	RequestedModel       string `json:"requested_model"`
	ProviderCredentialID string `json:"provider_credential_id"`
	RequestMode          string `json:"request_mode"`
	HealthcheckEnabled   bool   `json:"healthcheck_enabled"`
}

type ProviderModelMutationResult struct {
	Item ProviderModelItem `json:"item"`
}

type ModelHealthItem struct {
	ID                   string `json:"id"`
	RequestedModel       string `json:"requested_model"`
	ProviderCredentialID string `json:"provider_credential_id"`
	RouteLabel           string `json:"route_label"`
	HealthStatus         string `json:"health_status"`
	LastHealthError      string `json:"last_health_error"`
	RequestMode          string `json:"request_mode"`
	LatencyMS            int64  `json:"latency_ms"`
	FirstTokenLatencyMS  int64  `json:"first_token_latency_ms"`
	LastHealthCheckedAt  string `json:"last_health_checked_at"`
}

type ModelHealthWallCell struct {
	BucketLabel string `json:"bucket_label"`
	Status      string `json:"status"`
	Latency     string `json:"latency"`
	Requests    string `json:"requests"`
}

type ModelHealthWallLane struct {
	Model          string                `json:"model"`
	Provider       string                `json:"provider"`
	RouteLabel     string                `json:"route_label"`
	SuccessRate    string                `json:"success_rate"`
	AverageLatency string                `json:"average_latency"`
	Cells          []ModelHealthWallCell `json:"cells"`
}

type ModelHealthWall struct {
	Window      string                `json:"window"`
	WindowLabel string                `json:"window_label"`
	Buckets     []string              `json:"buckets"`
	Lanes       []ModelHealthWallLane `json:"lanes"`
}

type ModelHealthPageData struct {
	Items []ModelHealthItem `json:"items"`
	Wall  ModelHealthWall   `json:"wall"`
}

type PlaygroundPageData struct {
	AvailableModels []string               `json:"available_models"`
	LastRun         *PlaygroundRunResponse `json:"last_run,omitempty"`
}

type PlaygroundRunRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream,omitempty"`
}

type PlaygroundRunResponse struct {
	RouteLabel  string `json:"route_label"`
	Endpoint    string `json:"endpoint"`
	Latency     string `json:"latency"`
	Status      string `json:"status"`
	Response    string `json:"response"`
	PlatformKey string `json:"platform_key"`
}

type PlaygroundStreamSession struct {
	ContentType string
	StatusCode  int
	Run         func(emit func([]byte) error) (PlaygroundRunResponse, error)
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
	Time                string `json:"time"`
	Tenant              string `json:"tenant"`
	Endpoint            string `json:"endpoint"`
	RequestModel        string `json:"request_model"`
	UpstreamModel       string `json:"upstream_model"`
	ResolvedModel       string `json:"resolved_model"`
	TaskClass           string `json:"task_class"`
	RoutingReason       string `json:"routing_reason"`
	TargetModelTier     string `json:"target_model_tier"`
	Status              string `json:"status"`
	RouteLabel          string `json:"route_label"`
	Latency             string `json:"latency"`
	FirstTokenLatencyMS int64  `json:"first_token_latency_ms"`
	UsageSource         string `json:"usage_source"`
	TotalCost           string `json:"total_cost"`
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
	ResolvedModel    string    `json:"resolved_model"`
	RouteID          string    `json:"route_id"`
	RequestPath      string    `json:"request_path"`
	Status           string    `json:"status"`
	ErrorCategory    string    `json:"error_category"`
	UsageSource      string    `json:"usage_source"`
	Limit            int       `json:"limit"`
	Offset           int       `json:"offset"`
}

type TenantBillingQuery struct {
	TenantID string `json:"tenant_id"`
	Month    string `json:"month"`
}

type TenantBillingSummary struct {
	TenantID     string `json:"tenant_id"`
	Month        string `json:"month"`
	RequestCount int64  `json:"request_count"`
	SuccessCount int64  `json:"success_count"`
	FailureCount int64  `json:"failure_count"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	CachedTokens int64  `json:"cached_tokens"`
	TotalTokens  int64  `json:"total_tokens"`
	InputCost    string `json:"input_cost"`
	OutputCost   string `json:"output_cost"`
	CachedCost   string `json:"cached_cost"`
	TotalCost    string `json:"total_cost"`
}

type TenantBillingProviderItem struct {
	ProviderCredentialID string `json:"provider_credential_id"`
	Provider             string `json:"provider"`
	DisplayName          string `json:"display_name"`
	RequestCount         int64  `json:"request_count"`
	SuccessCount         int64  `json:"success_count"`
	FailureCount         int64  `json:"failure_count"`
	TotalTokens          int64  `json:"total_tokens"`
	TotalCost            string `json:"total_cost"`
}

type TenantBillingModelItem struct {
	Model                string `json:"model"`
	ProviderCredentialID string `json:"provider_credential_id"`
	ProviderDisplayName  string `json:"provider_display_name"`
	RequestCount         int64  `json:"request_count"`
	SuccessCount         int64  `json:"success_count"`
	FailureCount         int64  `json:"failure_count"`
	TotalTokens          int64  `json:"total_tokens"`
	TotalCost            string `json:"total_cost"`
}

type TenantBillingAPIKeyItem struct {
	PlatformAPIKeyID string `json:"platform_api_key_id"`
	Name             string `json:"name"`
	RequestCount     int64  `json:"request_count"`
	SuccessCount     int64  `json:"success_count"`
	FailureCount     int64  `json:"failure_count"`
	TotalTokens      int64  `json:"total_tokens"`
	TotalCost        string `json:"total_cost"`
}

type TenantBillingPageData struct {
	Summary   TenantBillingSummary        `json:"summary"`
	Providers []TenantBillingProviderItem `json:"providers"`
	Models    []TenantBillingModelItem    `json:"models"`
	APIKeys   []TenantBillingAPIKeyItem   `json:"api_keys"`
}

type UsageOverviewData struct {
	TotalRequests  int64              `json:"total_requests"`
	SuccessRate    string             `json:"success_rate"`
	TotalTokens    string             `json:"total_tokens"`
	InputTokens    string             `json:"input_tokens"`
	OutputTokens   string             `json:"output_tokens"`
	CachedTokens   string             `json:"cached_tokens"`
	AverageLatency string             `json:"average_latency"`
	EstimatedShare string             `json:"estimated_share"`
	InputCost      string             `json:"input_cost"`
	OutputCost     string             `json:"output_cost"`
	CachedCost     string             `json:"cached_cost"`
	TotalCost      string             `json:"total_cost"`
	PricingModels  []PricingModelItem `json:"pricing_models"`
}

type PricingModelItem struct {
	Model       string `json:"model"`
	InputPrice  string `json:"input_price"`
	OutputPrice string `json:"output_price"`
	CachedPrice string `json:"cached_price"`
}

type UsageTrendPoint struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type UsageTrendData struct {
	Requests []UsageTrendPoint `json:"requests"`
	Tokens   []UsageTrendPoint `json:"tokens"`
	Success  []UsageTrendPoint `json:"success"`
	Costs    []UsageTrendPoint `json:"costs"`
}

type UsageLatencyCell struct {
	BucketLabel string `json:"bucket_label"`
	Latency     string `json:"latency"`
	Status      string `json:"status"`
	Requests    string `json:"requests"`
}

type UsageLatencyLane struct {
	Model          string             `json:"model"`
	Provider       string             `json:"provider"`
	Source         string             `json:"source"`
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
	Breakdown        []UsageFailureBucket    `json:"breakdown"`
	RecentEvents     []string                `json:"recent_events"`
	RecentEventItems []UsageFailureEventItem `json:"recent_event_items"`
}

type UsageFailureEventItem struct {
	Time          string `json:"time"`
	TenantID      string `json:"tenant_id"`
	TenantName    string `json:"tenant_name"`
	RequestModel  string `json:"request_model"`
	ResolvedModel string `json:"resolved_model"`
	Provider      string `json:"provider"`
	StatusCode    int    `json:"status_code"`
	Category      string `json:"category"`
	Reason        string `json:"reason"`
}

type UsageRequestItem struct {
	RequestID           string `json:"request_id"`
	Tenant              string `json:"tenant"`
	TenantID            string `json:"tenant_id"`
	TenantName          string `json:"tenant_name"`
	Endpoint            string `json:"endpoint"`
	Model               string `json:"model"`
	ResolvedModel       string `json:"resolved_model"`
	TaskClass           string `json:"task_class"`
	RoutingReason       string `json:"routing_reason"`
	TargetModelTier     string `json:"target_model_tier"`
	Status              string `json:"status"`
	TotalTokens         string `json:"total_tokens"`
	InputTokens         string `json:"input_tokens"`
	OutputTokens        string `json:"output_tokens"`
	CachedTokens        string `json:"cached_tokens"`
	Latency             string `json:"latency"`
	FirstTokenLatencyMS int64  `json:"first_token_latency_ms"`
	UsageSource         string `json:"usage_source"`
	InputCost           string `json:"input_cost"`
	OutputCost          string `json:"output_cost"`
	CachedCost          string `json:"cached_cost"`
	TotalCost           string `json:"total_cost"`
	InputPrice          string `json:"input_price"`
	OutputPrice         string `json:"output_price"`
	CachedPrice         string `json:"cached_price"`
}

type UsageRequestDetail struct {
	RequestID           string                  `json:"request_id"`
	TenantID            string                  `json:"tenant_id"`
	TenantName          string                  `json:"tenant_name"`
	Endpoint            string                  `json:"endpoint"`
	Model               string                  `json:"model"`
	ResolvedModel       string                  `json:"resolved_model"`
	TaskClass           string                  `json:"task_class"`
	RoutingReason       string                  `json:"routing_reason"`
	TargetModelTier     string                  `json:"target_model_tier"`
	Status              string                  `json:"status"`
	TotalTokens         string                  `json:"total_tokens"`
	InputTokens         string                  `json:"input_tokens"`
	OutputTokens        string                  `json:"output_tokens"`
	CachedTokens        string                  `json:"cached_tokens"`
	Latency             string                  `json:"latency"`
	FirstTokenLatencyMS int64                   `json:"first_token_latency_ms"`
	UsageSource         string                  `json:"usage_source"`
	InputCost           string                  `json:"input_cost"`
	OutputCost          string                  `json:"output_cost"`
	CachedCost          string                  `json:"cached_cost"`
	TotalCost           string                  `json:"total_cost"`
	InputPrice          string                  `json:"input_price"`
	OutputPrice         string                  `json:"output_price"`
	CachedPrice         string                  `json:"cached_price"`
	PromptExcerpt       string                  `json:"prompt_excerpt"`
	ResponseExcerpt     string                  `json:"response_excerpt"`
	ErrorCode           string                  `json:"error_code"`
	ErrorMessage        string                  `json:"error_message"`
	FailureEvents       []UsageFailureEventItem `json:"failure_events"`
}

type UsageRequestsPageData struct {
	Items                []UsageRequestItem `json:"items"`
	ResolvedModelOptions []string           `json:"resolved_model_options"`
	Total                int64              `json:"total"`
	Limit                int                `json:"limit"`
	Offset               int                `json:"offset"`
}

type ConsoleService interface {
	Overview(ctx context.Context) (OverviewPageData, error)
	SystemStatus(ctx context.Context) (ConsoleSystemStatus, error)
	IssueCaptcha(ctx context.Context, ip string, userAgent string) (CaptchaChallenge, error)
	VerifyCaptcha(ctx context.Context, req VerifyCaptchaRequest) (CaptchaPassResult, error)
	Applications(ctx context.Context) (ApplicationsPageData, error)
	CreateApplication(ctx context.Context, req CreateApplicationRequest) (ApplicationMutationResult, error)
	ApproveApplication(ctx context.Context, id string, req ApproveApplicationRequest) (ApplicationMutationResult, error)
	RejectApplication(ctx context.Context, id string, req RejectApplicationRequest) (ApplicationMutationResult, error)
	AccountDeletionApplications(ctx context.Context) (AccountDeletionApplicationsPageData, error)
	ApproveAccountDeletionApplication(ctx context.Context, id string, req ReviewAccountDeletionApplicationRequest) (AccountDeletionApplicationMutationResult, error)
	RejectAccountDeletionApplication(ctx context.Context, id string, req ReviewAccountDeletionApplicationRequest) (AccountDeletionApplicationMutationResult, error)
	APIKeys(ctx context.Context) (APIKeysPageData, error)
	CreateAPIKey(ctx context.Context, req CreateAPIKeyRequest) (APIKeyMutationResult, error)
	RotateAPIKey(ctx context.Context, id string, req RotateAPIKeyRequest) (APIKeyMutationResult, error)
	DeactivateAPIKey(ctx context.Context, id string) (APIKeyMutationResult, error)
	DeleteAPIKey(ctx context.Context, id string) (APIKeyMutationResult, error)
	RevealAPIKeySecret(ctx context.Context, id string) (APIKeySecretView, error)
	CopyAPIKeySecret(ctx context.Context, id string, ip string, userAgent string) (APIKeySecretView, error)
	ProviderModels(ctx context.Context) (ProviderModelsPageData, error)
	CreateProvider(ctx context.Context, req CreateProviderRequest) (ProviderMutationResult, error)
	CreateProviderModel(ctx context.Context, req CreateProviderModelRequest) (ProviderModelMutationResult, error)
	RunProviderModelHealthcheck(ctx context.Context, id string) (ProviderModelMutationResult, error)
	ModelHealth(ctx context.Context, window string) (ModelHealthPageData, error)
	Routes(ctx context.Context) (RoutesPageData, error)
	Playground(ctx context.Context) (PlaygroundPageData, error)
	RunPlayground(ctx context.Context, req PlaygroundRunRequest) (PlaygroundRunResponse, error)
	StreamPlayground(ctx context.Context, req PlaygroundRunRequest) (PlaygroundStreamSession, error)
	Audit(ctx context.Context) (AuditPageData, error)
	TenantBilling(ctx context.Context, query TenantBillingQuery) (TenantBillingPageData, error)
	UsageOverview(ctx context.Context, query UsageQuery) (UsageOverviewData, error)
	UsageTrends(ctx context.Context, query UsageQuery) (UsageTrendData, error)
	UsageLatencyWall(ctx context.Context, query UsageQuery) (UsageLatencyWallData, error)
	UsageFailures(ctx context.Context, query UsageQuery) (UsageFailureData, error)
	UsageRequests(ctx context.Context, query UsageQuery) (UsageRequestsPageData, error)
	UsageRequestDetail(ctx context.Context, requestID string) (UsageRequestDetail, error)
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

func (unavailableConsoleService) IssueCaptcha(context.Context, string, string) (CaptchaChallenge, error) {
	return CaptchaChallenge{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) VerifyCaptcha(context.Context, VerifyCaptchaRequest) (CaptchaPassResult, error) {
	return CaptchaPassResult{}, ErrConsoleServiceUnavailable
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

func (unavailableConsoleService) AccountDeletionApplications(context.Context) (AccountDeletionApplicationsPageData, error) {
	return AccountDeletionApplicationsPageData{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) ApproveAccountDeletionApplication(context.Context, string, ReviewAccountDeletionApplicationRequest) (AccountDeletionApplicationMutationResult, error) {
	return AccountDeletionApplicationMutationResult{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) RejectAccountDeletionApplication(context.Context, string, ReviewAccountDeletionApplicationRequest) (AccountDeletionApplicationMutationResult, error) {
	return AccountDeletionApplicationMutationResult{}, ErrConsoleServiceUnavailable
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

func (unavailableConsoleService) ProviderModels(context.Context) (ProviderModelsPageData, error) {
	return ProviderModelsPageData{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) CreateProvider(context.Context, CreateProviderRequest) (ProviderMutationResult, error) {
	return ProviderMutationResult{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) CreateProviderModel(context.Context, CreateProviderModelRequest) (ProviderModelMutationResult, error) {
	return ProviderModelMutationResult{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) RunProviderModelHealthcheck(context.Context, string) (ProviderModelMutationResult, error) {
	return ProviderModelMutationResult{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) ModelHealth(context.Context, string) (ModelHealthPageData, error) {
	return ModelHealthPageData{}, ErrConsoleServiceUnavailable
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

func (unavailableConsoleService) StreamPlayground(context.Context, PlaygroundRunRequest) (PlaygroundStreamSession, error) {
	return PlaygroundStreamSession{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) Audit(context.Context) (AuditPageData, error) {
	return AuditPageData{}, ErrConsoleServiceUnavailable
}

func (unavailableConsoleService) TenantBilling(context.Context, TenantBillingQuery) (TenantBillingPageData, error) {
	return TenantBillingPageData{}, ErrConsoleServiceUnavailable
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

func (unavailableConsoleService) UsageRequestDetail(context.Context, string) (UsageRequestDetail, error) {
	return UsageRequestDetail{}, ErrConsoleServiceUnavailable
}
