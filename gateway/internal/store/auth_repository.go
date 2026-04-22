package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/liwenjian/ai_gateway/gateway/internal/domain"
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
	ID          string
	Provider    string
	DisplayName string
	Status      domain.Status
}

type AuthRepository interface {
	FindPlatformAPIKeyByHash(ctx context.Context, keyHash string) (PlatformAPIKeyRecord, error)
	FindTenantByID(ctx context.Context, tenantID string) (TenantRecord, error)
	ListActiveProviderCredentials(ctx context.Context) ([]ProviderCredentialRecord, error)
}

type authQueries interface {
	GetPlatformAPIKeyByHash(ctx context.Context, keyHash string) (GetPlatformAPIKeyByHashRow, error)
	GetTenantByID(ctx context.Context, id string) (GetTenantByIDRow, error)
	ListActiveProviderCredentials(ctx context.Context) ([]ListActiveProviderCredentialsRow, error)
}

type SQLAuthRepository struct {
	queries authQueries
}

func NewAuthRepository(queries authQueries) *SQLAuthRepository {
	return &SQLAuthRepository{queries: queries}
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
		credentials = append(credentials, ProviderCredentialRecord{
			ID:          row.ID,
			Provider:    row.Provider,
			DisplayName: row.DisplayName,
			Status:      domain.Status(row.Status),
		})
	}

	return credentials, nil
}

var _ AuthRepository = (*SQLAuthRepository)(nil)
