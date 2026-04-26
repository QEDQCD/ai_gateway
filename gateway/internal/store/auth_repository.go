package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/liwenjian/ai_gateway/gateway/internal/domain"
	"github.com/liwenjian/ai_gateway/gateway/internal/secret"
)

var ErrAuthRecordNotFound = errors.New("auth record not found")

type PlatformAPIKeyRecord struct {
	ID       string
	TenantID string
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

type AuthRepository interface {
	FindPlatformAPIKeyByHash(ctx context.Context, keyHash string) (PlatformAPIKeyRecord, error)
	FindTenantByID(ctx context.Context, tenantID string) (TenantRecord, error)
	ListActiveProviderCredentials(ctx context.Context) ([]ProviderCredentialRecord, error)
}

type BootstrapAuthConfig struct {
	RawPlatformAPIKey    string
	PlatformAPIKeyID     string
	PlatformAPIKeyName   string
	TenantID             string
	TenantName           string
	ProviderCredentialID string
	Provider             string
	ProviderDisplayName  string
	ProviderBaseURL      string
	ProviderAPIKey       string
	SupportedModels      []string
}

type BootstrapAuthRepository struct {
	platformKeyHash      string
	platformAPIKeyRecord PlatformAPIKeyRecord
	tenantRecord         TenantRecord
	providerCredentials  []ProviderCredentialRecord
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

func hashPlatformAPIKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return "sha256:" + hex.EncodeToString(sum[:])
}

var _ AuthRepository = (*SQLAuthRepository)(nil)
var _ AuthRepository = (*BootstrapAuthRepository)(nil)
