package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/example/ai_gateway/gateway/internal/domain"
	"github.com/example/ai_gateway/gateway/internal/secret"
	"github.com/jackc/pgx/v5"
)

var ErrAuthRecordNotFound = errors.New("auth record not found")
var ErrAuthScopeAmbiguous = errors.New("auth scope ambiguous")

type PlatformAPIKeyRecord struct {
	ID       string
	TenantID string
	UserID   string
	Name     string
	Status   domain.Status
}

type TenantRecord struct {
	ID     string
	Name   string
	Status domain.Status
}

type ProviderCredentialRecord struct {
	ID              string
	Provider        string
	DisplayName     string
	BaseURL         string
	APIKey          string
	Status          domain.Status
	SupportedModels []string
}

type ConsolePrincipalRecord struct {
	UserID   string
	Email    string
	Role     string
	TenantID string
}

type AuthRepository interface {
	FindPlatformAPIKeyByHash(ctx context.Context, keyHash string) (PlatformAPIKeyRecord, error)
	FindTenantByID(ctx context.Context, tenantID string) (TenantRecord, error)
	ListActiveProviderCredentials(ctx context.Context) ([]ProviderCredentialRecord, error)
}

type BootstrapAuthConfig struct {
	RawPlatformAPIKey    string
	PlatformAPIKeyID     string
	PlatformAPIKeyUserID string
	PlatformAPIKeyName   string
	TenantID             string
	TenantName           string
	ProviderCredentialID string
	Provider             string
	ProviderDisplayName  string
	ProviderBaseURL      string
	ProviderAPIKey       string
	SupportedModels      []string
	ConsolePrincipals    []ConsolePrincipalRecord
}

type BootstrapAuthRepository struct {
	platformKeyHash      string
	platformAPIKeyRecord PlatformAPIKeyRecord
	tenantRecord         TenantRecord
	providerCredentials  []ProviderCredentialRecord
	consolePrincipals    map[string]ConsolePrincipalRecord
}

type authQueries interface {
	GetPlatformAPIKeyByHash(ctx context.Context, keyHash string) (GetPlatformAPIKeyByHashRow, error)
	GetTenantByID(ctx context.Context, id string) (GetTenantByIDRow, error)
	ListActiveProviderCredentials(ctx context.Context) ([]ListActiveProviderCredentialsRow, error)
}

type SQLAuthRepository struct {
	queries     authQueries
	secretCodec *secret.Codec
}

func NewAuthRepository(queries authQueries, secretCodec ...*secret.Codec) *SQLAuthRepository {
	repo := &SQLAuthRepository{queries: queries}
	if len(secretCodec) > 0 {
		repo.secretCodec = secretCodec[0]
	}
	return repo
}

func NewBootstrapAuthRepository(cfg BootstrapAuthConfig) *BootstrapAuthRepository {
	repo := &BootstrapAuthRepository{}
	if cfg.RawPlatformAPIKey == "" {
		return repo
	}

	repo.platformKeyHash = hashPlatformAPIKey(cfg.RawPlatformAPIKey)
	repo.platformAPIKeyRecord = PlatformAPIKeyRecord{
		ID:       cfg.PlatformAPIKeyID,
		TenantID: cfg.TenantID,
		UserID:   cfg.PlatformAPIKeyUserID,
		Name:     cfg.PlatformAPIKeyName,
		Status:   domain.StatusActive,
	}
	repo.tenantRecord = TenantRecord{
		ID:     cfg.TenantID,
		Name:   cfg.TenantName,
		Status: domain.StatusActive,
	}
	repo.providerCredentials = []ProviderCredentialRecord{
		{
			ID:              cfg.ProviderCredentialID,
			Provider:        cfg.Provider,
			DisplayName:     cfg.ProviderDisplayName,
			BaseURL:         cfg.ProviderBaseURL,
			APIKey:          cfg.ProviderAPIKey,
			Status:          domain.StatusActive,
			SupportedModels: append([]string(nil), cfg.SupportedModels...),
		},
	}
	if len(cfg.ConsolePrincipals) > 0 {
		repo.consolePrincipals = make(map[string]ConsolePrincipalRecord, len(cfg.ConsolePrincipals))
		for _, principal := range cfg.ConsolePrincipals {
			repo.consolePrincipals[normalizeConsoleSubject(principal.Email)] = principal
		}
	}

	return repo
}

func (r *SQLAuthRepository) FindPlatformAPIKeyByHash(ctx context.Context, keyHash string) (PlatformAPIKeyRecord, error) {
	row, err := r.queries.GetPlatformAPIKeyByHash(ctx, keyHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PlatformAPIKeyRecord{}, ErrAuthRecordNotFound
		}
		return PlatformAPIKeyRecord{}, err
	}

	return PlatformAPIKeyRecord{
		ID:       row.ID,
		TenantID: row.TenantID,
		Name:     row.Name,
		Status:   domain.Status(row.Status),
	}, nil
}

func (r *SQLAuthRepository) FindTenantByID(ctx context.Context, tenantID string) (TenantRecord, error) {
	row, err := r.queries.GetTenantByID(ctx, tenantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TenantRecord{}, ErrAuthRecordNotFound
		}
		return TenantRecord{}, err
	}

	return TenantRecord{
		ID:     row.ID,
		Name:   row.Name,
		Status: domain.Status(row.Status),
	}, nil
}

func (r *SQLAuthRepository) ListActiveProviderCredentials(ctx context.Context) ([]ProviderCredentialRecord, error) {
	rows, err := r.queries.ListActiveProviderCredentials(ctx)
	if err != nil {
		return nil, err
	}

	credentials := make([]ProviderCredentialRecord, 0, len(rows))
	for _, row := range rows {
		apiKey := row.EncryptedSecret
		if r.secretCodec != nil && strings.HasPrefix(row.EncryptedSecret, secret.EncryptedSecretPrefix) {
			decryptedSecret, err := r.secretCodec.Decrypt(row.EncryptedSecret)
			if err != nil {
				return nil, err
			}
			apiKey = decryptedSecret
		}

		credentials = append(credentials, ProviderCredentialRecord{
			ID:              row.ID,
			Provider:        row.Provider,
			DisplayName:     row.DisplayName,
			BaseURL:         row.BaseURL,
			APIKey:          apiKey,
			Status:          domain.Status(row.Status),
			SupportedModels: append([]string(nil), row.SupportedModels...),
		})
	}

	return credentials, nil
}

func (r *BootstrapAuthRepository) FindPlatformAPIKeyByHash(_ context.Context, keyHash string) (PlatformAPIKeyRecord, error) {
	if r.platformKeyHash == "" || keyHash != r.platformKeyHash {
		return PlatformAPIKeyRecord{}, ErrAuthRecordNotFound
	}
	return r.platformAPIKeyRecord, nil
}

func (r *BootstrapAuthRepository) FindTenantByID(_ context.Context, tenantID string) (TenantRecord, error) {
	if r.tenantRecord.ID == "" || tenantID != r.tenantRecord.ID {
		return TenantRecord{}, ErrAuthRecordNotFound
	}
	return r.tenantRecord, nil
}

func (r *BootstrapAuthRepository) ListActiveProviderCredentials(context.Context) ([]ProviderCredentialRecord, error) {
	return append([]ProviderCredentialRecord(nil), r.providerCredentials...), nil
}

func (r *SQLAuthRepository) ResolveConsolePrincipal(ctx context.Context, subject string) (ConsolePrincipalRecord, error) {
	queries, ok := r.queries.(*Queries)
	if !ok {
		return ConsolePrincipalRecord{}, ErrAuthRecordNotFound
	}

	const lookupConsolePrincipal = `
select u.id, u.email, u.role,
  case
    when u.role = 'member' and coalesce(scope.membership_count, 0) = 1 then coalesce(scope.tenant_id, '')
    else ''
  end as tenant_id,
  coalesce(scope.membership_count, 0) as membership_count
from users u
left join lateral (
  select min(tm.tenant_id) as tenant_id, count(*) as membership_count
  from tenant_memberships tm
  where tm.user_id = u.id
    and tm.status = 'active'
) scope on true
where lower(u.email) = $1
  and u.status = 'active'
  and (
    u.role = 'admin'
    or coalesce(scope.membership_count, 0) > 0
  )
limit 1
`

	subject = normalizeConsoleSubject(subject)
	var record ConsolePrincipalRecord
	var membershipCount int64
	err := queries.db.QueryRow(ctx, lookupConsolePrincipal, subject).Scan(
		&record.UserID,
		&record.Email,
		&record.Role,
		&record.TenantID,
		&membershipCount,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ConsolePrincipalRecord{}, ErrAuthRecordNotFound
		}
		return ConsolePrincipalRecord{}, err
	}
	if record.Role == domain.ConsoleRoleMember && membershipCount > 1 {
		return ConsolePrincipalRecord{}, ErrAuthScopeAmbiguous
	}
	return record, nil
}

func (r *BootstrapAuthRepository) ResolveConsolePrincipal(_ context.Context, subject string) (ConsolePrincipalRecord, error) {
	if len(r.consolePrincipals) == 0 {
		return ConsolePrincipalRecord{}, ErrAuthRecordNotFound
	}

	principal, ok := r.consolePrincipals[normalizeConsoleSubject(subject)]
	if !ok {
		return ConsolePrincipalRecord{}, ErrAuthRecordNotFound
	}
	return principal, nil
}

func hashPlatformAPIKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizeConsoleSubject(subject string) string {
	return strings.ToLower(strings.TrimSpace(subject))
}

var _ AuthRepository = (*SQLAuthRepository)(nil)
var _ AuthRepository = (*BootstrapAuthRepository)(nil)
