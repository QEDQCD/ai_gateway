package service_test

import (
	"strings"
	"testing"

	"github.com/example/ai_gateway/gateway/internal/service"
)

func TestResolveModelPricingFallsBackToDefault(t *testing.T) {
	resolver, err := service.NewModelPricingResolver(map[string]service.ModelTokenPrice{
		"default": {
			InputMicroyuanPerMillion:  2_000_000,
			OutputMicroyuanPerMillion: 20_000_000,
			CachedMicroyuanPerMillion: 500_000,
		},
		"qwen-flash": {
			InputMicroyuanPerMillion:  3_000_000,
			OutputMicroyuanPerMillion: 30_000_000,
			CachedMicroyuanPerMillion: 700_000,
		},
	})
	if err != nil {
		t.Fatalf("NewModelPricingResolver failed: %v", err)
	}

	got, err := resolver.Resolve("unknown-model")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if got.InputMicroyuanPerMillion != 2_000_000 {
		t.Fatalf("expected default input price, got %d", got.InputMicroyuanPerMillion)
	}
	if got.OutputMicroyuanPerMillion != 20_000_000 {
		t.Fatalf("expected default output price, got %d", got.OutputMicroyuanPerMillion)
	}
	if got.CachedMicroyuanPerMillion != 500_000 {
		t.Fatalf("expected default cached price, got %d", got.CachedMicroyuanPerMillion)
	}
}

func TestResolveModelPricingUsesExactModelMatch(t *testing.T) {
	resolver, err := service.NewModelPricingResolver(map[string]service.ModelTokenPrice{
		"default": {
			InputMicroyuanPerMillion:  2_000_000,
			OutputMicroyuanPerMillion: 20_000_000,
			CachedMicroyuanPerMillion: 500_000,
		},
		"qwen-flash": {
			InputMicroyuanPerMillion:  3_000_000,
			OutputMicroyuanPerMillion: 30_000_000,
			CachedMicroyuanPerMillion: 700_000,
		},
	})
	if err != nil {
		t.Fatalf("NewModelPricingResolver failed: %v", err)
	}

	got, err := resolver.Resolve("qwen-flash")
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if got != (service.ModelTokenPrice{
		InputMicroyuanPerMillion:  3_000_000,
		OutputMicroyuanPerMillion: 30_000_000,
		CachedMicroyuanPerMillion: 700_000,
	}) {
		t.Fatalf("expected exact model price, got %#v", got)
	}
}

func TestNewModelPricingResolverRequiresDefault(t *testing.T) {
	_, err := service.NewModelPricingResolver(map[string]service.ModelTokenPrice{
		"qwen-flash": {
			InputMicroyuanPerMillion:  3_000_000,
			OutputMicroyuanPerMillion: 30_000_000,
			CachedMicroyuanPerMillion: 700_000,
		},
	})
	if err == nil {
		t.Fatal("expected missing default pricing to fail")
	}
}

func TestNewModelPricingResolverRejectsNegativePrices(t *testing.T) {
	_, err := service.NewModelPricingResolver(map[string]service.ModelTokenPrice{
		"default": {
			InputMicroyuanPerMillion:  -1,
			OutputMicroyuanPerMillion: 20_000_000,
			CachedMicroyuanPerMillion: 500_000,
		},
	})
	if err == nil {
		t.Fatal("expected negative model pricing to fail")
	}
	if !strings.Contains(err.Error(), "default") {
		t.Fatalf("expected error to mention default entry, got %v", err)
	}
}

func TestComputeUsageCostsRoundsHalfUp(t *testing.T) {
	costs, err := service.ComputeUsageCosts(service.ModelTokenPrice{
		InputMicroyuanPerMillion: 500_000,
	}, service.TokenUsageBreakdown{
		InputTokens: 1,
	})
	if err != nil {
		t.Fatalf("ComputeUsageCosts failed: %v", err)
	}

	if costs.InputCostMicroyuan != 1 {
		t.Fatalf("expected rounded input cost 1 microyuan, got %d", costs.InputCostMicroyuan)
	}
	if costs.TotalCostMicroyuan != 1 {
		t.Fatalf("expected rounded total cost 1 microyuan, got %d", costs.TotalCostMicroyuan)
	}
}

func TestComputeUsageCostsTreatsCachedTokensAsSubsetOfInput(t *testing.T) {
	costs, err := service.ComputeUsageCosts(service.ModelTokenPrice{
		InputMicroyuanPerMillion:  2_000_000,
		OutputMicroyuanPerMillion: 20_000_000,
		CachedMicroyuanPerMillion: 500_000,
	}, service.TokenUsageBreakdown{
		InputTokens:  11,
		OutputTokens: 7,
		CachedTokens: 5,
	})
	if err != nil {
		t.Fatalf("ComputeUsageCosts failed: %v", err)
	}

	if costs != (service.UsageCosts{
		InputCostMicroyuan:  12,
		OutputCostMicroyuan: 140,
		CachedCostMicroyuan: 3,
		TotalCostMicroyuan:  155,
	}) {
		t.Fatalf("expected full cost breakdown, got %#v", costs)
	}
}

func TestComputeUsageCostsRejectsNegativePrices(t *testing.T) {
	_, err := service.ComputeUsageCosts(service.ModelTokenPrice{
		InputMicroyuanPerMillion: -1,
	}, service.TokenUsageBreakdown{
		InputTokens: 1,
	})
	if err == nil {
		t.Fatal("expected negative price to fail")
	}
	if !strings.Contains(err.Error(), "input price") {
		t.Fatalf("expected input price error, got %v", err)
	}
}
