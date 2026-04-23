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
	SelectedProviderID   string
	SelectedProviderName string
	ProviderTarget       ProviderTarget
}

type ProviderRoute struct {
	ProviderID   string
	ProviderName string
	Target       ProviderTarget
}
