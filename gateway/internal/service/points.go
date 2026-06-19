package service

import (
	"fmt"
	"math"
)

const DefaultPointsDivisor int64 = 10_000

func NormalizePointsDivisor(divisor int64) int64 {
	if divisor <= 0 {
		return DefaultPointsDivisor
	}
	return divisor
}

func PointsFromMicroyuan(totalCostMicroyuan int64, divisor int64) float64 {
	divisor = NormalizePointsDivisor(divisor)
	if totalCostMicroyuan <= 0 {
		return 0
	}
	return float64(totalCostMicroyuan) / float64(divisor)
}

func FormatPoints(points float64) string {
	if points <= 0 {
		return "0 积分"
	}
	absPoints := math.Abs(points)
	switch {
	case absPoints >= 10_000:
		return fmt.Sprintf("%.2f 万积分", points/10_000)
	case absPoints >= 1:
		return fmt.Sprintf("%.2f 积分", points)
	default:
		return fmt.Sprintf("%.4f 积分", points)
	}
}

func FormatPointsRate(pointsPerSecond float64) string {
	if pointsPerSecond <= 0 {
		return "0 积分/秒"
	}
	return fmt.Sprintf("%.2f 积分/秒", pointsPerSecond)
}
