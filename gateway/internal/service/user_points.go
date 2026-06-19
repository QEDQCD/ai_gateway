package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type UserPointsSummaryItem struct {
	UserID       string `json:"user_id"`
	UserName     string `json:"user_name"`
	UserEmail    string `json:"user_email"`
	TenantID     string `json:"tenant_id"`
	TenantName   string `json:"tenant_name"`
	RequestCount int64  `json:"request_count"`
	TotalTokens  string `json:"total_tokens"`
	TotalCost    string `json:"total_cost"`
	TotalPoints  string `json:"total_points"`
}

type UserPointsModelItem struct {
	Model        string `json:"model"`
	RequestCount int64  `json:"request_count"`
	TotalTokens  string `json:"total_tokens"`
	TotalCost    string `json:"total_cost"`
	TotalPoints  string `json:"total_points"`
}

type UserPointsOverviewData struct {
	PointsDivisor int64                   `json:"points_divisor"`
	TotalPoints   string                  `json:"total_points"`
	TotalCost     string                  `json:"total_cost"`
	TotalRequests int64                   `json:"total_requests"`
	TotalTokens   string                  `json:"total_tokens"`
	Items         []UserPointsSummaryItem `json:"items"`
}

type MemberPointsOverviewData struct {
	UserID        string                `json:"user_id"`
	UserName      string                `json:"user_name"`
	PointsDivisor int64                 `json:"points_divisor"`
	TotalPoints   string                `json:"total_points"`
	TotalCost     string                `json:"total_cost"`
	TotalRequests int64                 `json:"total_requests"`
	TotalTokens   string                `json:"total_tokens"`
	ByModel       []UserPointsModelItem `json:"by_model"`
}

func (s postgresConsoleService) UserPointsOverview(ctx context.Context, query UsageQuery) (UserPointsOverviewData, error) {
	query, err := normalizeUsageQuery(query, time.Now().UTC())
	if err != nil {
		return UserPointsOverviewData{}, err
	}

	divisor := s.pointsDivisor()
	whereClause, args := buildUserPointsLedgerWhere(query)
	rows, err := s.db.Query(ctx, `
		select
			u.id,
			u.name,
			u.email,
			t.id,
			t.name,
			coalesce(sum(l.request_count), 0),
			coalesce(sum(l.total_tokens), 0),
			coalesce(sum(l.total_cost_microyuan), 0)
		from user_usage_ledger l
		join users u on u.id = l.user_id
		join tenants t on t.id = l.tenant_id
		where `+whereClause+`
		group by u.id, u.name, u.email, t.id, t.name
		order by coalesce(sum(l.total_cost_microyuan), 0) desc, u.name asc;
	`, args...)
	if err != nil {
		return UserPointsOverviewData{}, err
	}
	defer rows.Close()

	items := make([]UserPointsSummaryItem, 0)
	var totalRequests int64
	var totalTokens int64
	var totalCostMicroyuan int64
	for rows.Next() {
		var item UserPointsSummaryItem
		var tokens int64
		var costMicroyuan int64
		if err := rows.Scan(
			&item.UserID,
			&item.UserName,
			&item.UserEmail,
			&item.TenantID,
			&item.TenantName,
			&item.RequestCount,
			&tokens,
			&costMicroyuan,
		); err != nil {
			return UserPointsOverviewData{}, err
		}
		item.TotalTokens = formatLargeNumber(int(tokens))
		item.TotalCost = formatMicroyuanAmount(costMicroyuan)
		item.TotalPoints = FormatPoints(PointsFromMicroyuan(costMicroyuan, divisor))
		items = append(items, item)
		totalRequests += item.RequestCount
		totalTokens += tokens
		totalCostMicroyuan += costMicroyuan
	}
	if err := rows.Err(); err != nil {
		return UserPointsOverviewData{}, err
	}

	return UserPointsOverviewData{
		PointsDivisor: divisor,
		TotalPoints:   FormatPoints(PointsFromMicroyuan(totalCostMicroyuan, divisor)),
		TotalCost:     formatMicroyuanAmount(totalCostMicroyuan),
		TotalRequests: totalRequests,
		TotalTokens:   formatLargeNumber(int(totalTokens)),
		Items:         items,
	}, nil
}

func (s postgresConsoleService) MemberPointsOverview(ctx context.Context, query UsageQuery) (MemberPointsOverviewData, error) {
	query, err := normalizeUsageQuery(query, time.Now().UTC())
	if err != nil {
		return MemberPointsOverviewData{}, err
	}
	userID := strings.TrimSpace(query.UserID)
	if userID == "" {
		return MemberPointsOverviewData{}, StatusError{
			Code:    400,
			Message: "user_id is required",
		}
	}

	divisor := s.pointsDivisor()
	summaryWhere, summaryArgs := buildUserPointsLedgerWhere(query)
	var userName string
	var totalRequests int64
	var totalTokens int64
	var totalCostMicroyuan int64
	if err := s.db.QueryRow(ctx, `
		select
			u.name,
			coalesce(sum(l.request_count), 0),
			coalesce(sum(l.total_tokens), 0),
			coalesce(sum(l.total_cost_microyuan), 0)
		from user_usage_ledger l
		join users u on u.id = l.user_id
		where `+summaryWhere+`
		group by u.name;
	`, summaryArgs...).Scan(&userName, &totalRequests, &totalTokens, &totalCostMicroyuan); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MemberPointsOverviewData{
				UserID:        userID,
				PointsDivisor: divisor,
				TotalPoints:   FormatPoints(0),
				TotalCost:     formatMicroyuanAmount(0),
				TotalTokens:   formatLargeNumber(0),
				ByModel:       []UserPointsModelItem{},
			}, nil
		}
		return MemberPointsOverviewData{}, err
	}

	logWhere, logArgs := buildUsageLogWhere(query, "l.request_started_at", true)
	logWhere += " and l.user_id = $" + fmt.Sprint(len(logArgs)+1)
	logArgs = append(logArgs, userID)
	modelRows, err := s.db.Query(ctx, `
		select
			coalesce(nullif(l.upstream_model, ''), l.request_model) as model_name,
			count(*),
			coalesce(sum(l.total_tokens), 0),
			coalesce(sum(l.total_cost_microyuan), 0)
		from llm_request_logs l
		where `+logWhere+`
		group by model_name
		order by coalesce(sum(l.total_cost_microyuan), 0) desc, model_name asc;
	`, logArgs...)
	if err != nil {
		return MemberPointsOverviewData{}, err
	}
	defer modelRows.Close()

	byModel := make([]UserPointsModelItem, 0)
	for modelRows.Next() {
		var item UserPointsModelItem
		var tokens int64
		var costMicroyuan int64
		if err := modelRows.Scan(&item.Model, &item.RequestCount, &tokens, &costMicroyuan); err != nil {
			return MemberPointsOverviewData{}, err
		}
		item.TotalTokens = formatLargeNumber(int(tokens))
		item.TotalCost = formatMicroyuanAmount(costMicroyuan)
		item.TotalPoints = FormatPoints(PointsFromMicroyuan(costMicroyuan, divisor))
		byModel = append(byModel, item)
	}
	if err := modelRows.Err(); err != nil {
		return MemberPointsOverviewData{}, err
	}

	return MemberPointsOverviewData{
		UserID:        userID,
		UserName:      userName,
		PointsDivisor: divisor,
		TotalPoints:   FormatPoints(PointsFromMicroyuan(totalCostMicroyuan, divisor)),
		TotalCost:     formatMicroyuanAmount(totalCostMicroyuan),
		TotalRequests: totalRequests,
		TotalTokens:   formatLargeNumber(int(totalTokens)),
		ByModel:       byModel,
	}, nil
}

func buildUserPointsLedgerWhere(query UsageQuery) (string, []any) {
	builder := newUsageWhereBuilderWithTime(query.From, query.To, "l.bucket_start")
	builder.addIfNotEmpty("l.tenant_id = $%d", query.TenantID)
	builder.addIfNotEmpty("l.user_id = $%d", query.UserID)
	return builder.whereClause(), builder.args
}

func (s postgresConsoleService) pointsDivisor() int64 {
	if s.pointsDivisorValue <= 0 {
		return DefaultPointsDivisor
	}
	return s.pointsDivisorValue
}
