package units

import (
	"math"
	"testing"
)

func TestMicrojoulesToWatts(t *testing.T) {
	tests := []struct {
		name    string
		deltaUJ float64
		seconds float64
		want    float64
		wantOK  bool
	}{
		{name: "one joule per second", deltaUJ: 1_000_000, seconds: 1, want: 1, wantOK: true},
		{name: "45.5 watt package", deltaUJ: 45_500_000, seconds: 1, want: 45.5, wantOK: true},
		{name: "two second window", deltaUJ: 91_000_000, seconds: 2, want: 45.5, wantOK: true},
		{name: "zero delta", deltaUJ: 0, seconds: 1, want: 0, wantOK: true},
		{name: "negative delta", deltaUJ: -1, seconds: 1, wantOK: false},
		{name: "zero seconds", deltaUJ: 1, seconds: 0, wantOK: false},
		{name: "nan seconds", deltaUJ: 1, seconds: math.NaN(), wantOK: false},
		{name: "infinite delta", deltaUJ: math.Inf(1), seconds: 1, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := MicrojoulesToWatts(tt.deltaUJ, tt.seconds)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Fatalf("watts = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMicrowattsToWatts(t *testing.T) {
	tests := []struct {
		name   string
		uw     float64
		want   float64
		wantOK bool
	}{
		{name: "85 watt gpu", uw: 85_000_000, want: 85, wantOK: true},
		{name: "zero", uw: 0, want: 0, wantOK: true},
		{name: "negative", uw: -1, wantOK: false},
		{name: "nan", uw: math.NaN(), wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := MicrowattsToWatts(tt.uw)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Fatalf("watts = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPercentBounds(t *testing.T) {
	tests := []struct {
		draw, max float64
		want      int
	}{
		{45.5, 150, 30},
		{-1, 100, 0},
		{200, 100, 100},
		{1, 0, 0},
		{1, math.NaN(), 0},
	}
	for _, tt := range tests {
		if got := Percent(tt.draw, tt.max); got != tt.want {
			t.Errorf("Percent(%v, %v) = %d, want %d", tt.draw, tt.max, got, tt.want)
		}
	}
}

func TestRound(t *testing.T) {
	tests := []struct {
		in    float64
		place int
		want  float64
	}{
		{1.23456, 2, 1.23},
		{1.235, 2, 1.24},
		{149.999, 2, 150},
		{-0.001, 2, 0},
	}
	for _, tt := range tests {
		if got := Round(tt.in, tt.place); got != tt.want {
			t.Errorf("Round(%v, %d) = %v, want %v", tt.in, tt.place, got, tt.want)
		}
	}
}
