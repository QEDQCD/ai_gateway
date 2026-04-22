package domain

type RequestContext struct {
	TenantID             string
	PlatformAPIKeyID     string
	SelectedProviderID   string
	SelectedProviderName string
}

type ProviderRoute struct {
	ProviderID   string
	ProviderName string
}
