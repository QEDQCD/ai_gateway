package service

import (
	"errors"
	"strings"

	"github.com/example/ai_gateway/gateway/internal/config"
)

type ModelTokenPrice = config.ModelTokenPrice

type TokenUsageBreakdown struct {
	InputTokens  int64
	OutputTokens int64
	CachedTokens int64
}

type UsageCosts struct {
	InputCostMicroyuan  int64
	OutputCostMicroyuan int64
	CachedCostMicroyuan int64
	TotalCostMicroyuan  int64
}

type ModelPricingResolver struct {
	prices map[string]ModelTokenPrice
}

func NewModelPricingResolver(prices map[string]config.ModelTokenPrice) (ModelPricingResolver, error) {
	normalized := make(map[string]ModelTokenPrice, len(prices))
	for model, price := range prices {
		key := strings.TrimSpace(model)
		if key == "" {
			continue
		}
		normalized[key] = ModelTokenPrice{
			InputMicroyuanPerMillion:  nonNegativeInt64(price.InputMicroyuanPerMillion),
			OutputMicroyuanPerMillion: nonNegativeInt64(price.OutputMicroyuanPerMillion),
			CachedMicroyuanPerMillion: nonNegativeInt64(price.CachedMicroyuanPerMillion),
		}
	}

	if _, ok := normalized["default"]; !ok {
		return ModelPricingResolver{}, errors.New("service: model pricing requires default entry")
	}

	return ModelPricingResolver{prices: normalized}, nil
}

func (r ModelPricingResolver) Resolve(model string) (ModelTokenPrice, error) {
	key := strings.TrimSpace(model)
	if price, ok := r.prices[key]; ok {
		return price, nil
	}
	if price, ok := r.prices["default"]; ok {
		return price, nil
	}
	return ModelTokenPrice{}, errors.New("service: default model pricing not configured")
}

func ComputeUsageCosts(price ModelTokenPrice, usage TokenUsageBreakdown) UsageCosts {
	costs := UsageCosts{
		InputCostMicroyuan:  roundMicroyuanCost(usage.InputTokens, price.InputMicroyuanPerMillion),
		OutputCostMicroyuan: roundMicroyuanCost(usage.OutputTokens, price.OutputMicroyuanPerMillion),
		CachedCostMicroyuan: roundMicroyuanCost(usage.CachedTokens, price.CachedMicroyuanPerMillion),
	}
	costs.TotalCostMicroyuan = costs.InputCostMicroyuan + costs.OutputCostMicroyuan + costs.CachedCostMicroyuan
	return costs
}

func roundMicroyuanCost(tokens int64, price int64) int64 {
	if tokens <= 0 || price <= 0 {
		return 0
	}
	return (tokens*price + 500_000) / 1_000_000
}

func nonNegativeInt64(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}
