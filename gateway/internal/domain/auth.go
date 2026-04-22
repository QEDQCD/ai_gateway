package domain

type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

type Tenant struct {
	ID     string
	Name   string
	Status Status
}

type PlatformAPIKey struct {
	ID       string
	TenantID string
	Name     string
	KeyHash  string
	Status   Status
}

type ProviderCredential struct {
	ID              string
	Provider        string
	DisplayName     string
	EncryptedSecret string
	Status          Status
}

type BYOKCredential struct {
	ID              string
	TenantID        string
	Provider        string
	EncryptedSecret string
	Status          Status
}
