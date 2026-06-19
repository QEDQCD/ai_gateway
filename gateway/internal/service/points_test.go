package service

import "testing"

func TestPointsFromMicroyuanUsesDefaultDivisor(t *testing.T) {
	t.Parallel()

	if got := PointsFromMicroyuan(25_000, 0); got != 2.5 {
		t.Fatalf("expected 2.5 points, got %v", got)
	}
}

func TestFormatPoints(t *testing.T) {
	t.Parallel()

	cases := map[float64]string{
		0:        "0 积分",
		2.5:      "2.50 积分",
		12_345.6: "1.23 万积分",
	}
	for points, want := range cases {
		if got := FormatPoints(points); got != want {
			t.Fatalf("FormatPoints(%v) = %q, want %q", points, got, want)
		}
	}
}
