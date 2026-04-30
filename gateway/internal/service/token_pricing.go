package service

import (
	"errors"
	"fmt"
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

func NewModelPricingResolver(prices map[string]ModelTokenPrice) (ModelPricingResolver, error) {
	normalized := make(map[string]ModelTokenPrice, len(prices))
	for model, price := range prices {
		key := strings.TrimSpace(model)
		if key == "" {
			continue
		}

		if err := validateModelTokenPrice(key, price); err != nil {
			return ModelPricingResolver{}, err
		}
		normalized[key] = price
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
		InputCostMicroyuan:  roundMicroyuanCost(uncachedInputTokens(usage), price.InputMicroyuanPerMillion),
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

func uncachedInputTokens(usage TokenUsageBreakdown) int64 {
	if usage.InputTokens <= usage.CachedTokens {
		return 0
	}
	return usage.InputTokens - usage.CachedTokens
}

func validateModelTokenPrice(model string, price ModelTokenPrice) error {
	if price.InputMicroyuanPerMillion < 0 {
		return fmt.Errorf("service: model pricing %q input price must be >= 0", model)
	}
	if price.OutputMicroyuanPerMillion < 0 {
		return fmt.Errorf("service: model pricing %q output price must be >= 0", model)
	}
	if price.CachedMicroyuanPerMillion < 0 {
		return fmt.Errorf("service: model pricing %q cached price must be >= 0", model)
	}
	return nil
}
