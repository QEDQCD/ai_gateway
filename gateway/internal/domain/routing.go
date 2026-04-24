package domain

type ProviderTarget struct {
	CredentialID string
	Provider     string
	BaseURL      string
	APIKey       string
}

type RequestContext struct {
	TenantID             string
	PlatformAPIKeyID     string
	PlatformAPIKeyName   string
	SelectedProviderID   string
	SelectedProviderName string
	RouteID              string
	ProviderTarget       ProviderTarget
}

type ProviderRoute struct {
	RouteID      string
	ProviderID   string
	ProviderName string
	Target       ProviderTarget
}
