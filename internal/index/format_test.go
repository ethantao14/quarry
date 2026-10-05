package index

import (
	"math"
	"testing"
)

func TestRoundScoreUp(t *testing.T) {
	for _, tt := range []struct {
		name string
		x    float64
		want float32
	}{
		{"zero", 0, 0},
		{"exact", 1.5, 1.5},
		{"round down", 1 + math.Ldexp(1, -25), math.Nextafter32(1, 2)},
		{"round up", 1 + 3*math.Ldexp(1, -25), math.Nextafter32(1, 2)},
		{"tiny", math.SmallestNonzeroFloat64, math.SmallestNonzeroFloat32},
		{"largest", math.MaxFloat32, math.MaxFloat32},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := roundScoreUp(tt.x)
			if got != tt.want || float64(got) < tt.x {
				t.Fatalf("roundScoreUp(%g) = %g, want %g >= input", tt.x, got, tt.want)
			}
		})
	}
}
