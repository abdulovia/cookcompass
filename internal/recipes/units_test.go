package recipes

import "testing"

func TestConvertUnits(t *testing.T) {
	tests := []struct {
		q      float64
		unit   string
		system string
		wantQ  float64
		wantU  string
	}{
		{1, "cup", Metric, 240, "ml"},
		{1.5, "cup", Metric, 350, "ml"},
		{8, "cup", Metric, 1.9, "l"},
		{1, "lb", Metric, 450, "g"},
		{3, "lb", Metric, 1.35, "kg"},
		{4, "oz", Metric, 110, "g"},
		{0.1, "oz", Metric, 3, "g"},
		{2, "fl oz", Metric, 60, "ml"},
		{2, "tbsp", Metric, 2, "tbsp"},
		{500, "g", Imperial, 1, "lb"},
		{200, "g", Imperial, 7, "oz"},
		{1.2, "kg", Imperial, 2.75, "lb"},
		{250, "ml", Imperial, 1, "cup"},
		{30, "ml", Imperial, 2, "tbsp"},
		{5, "ml", Imperial, 1, "tsp"},
		{1, "l", Imperial, 4.25, "cup"},
		{3, "clove", Imperial, 3, "clove"},
		{1, "cup", Imperial, 1, "cup"},
		{1, "cup", "other", 1, "cup"},
		{0.01, "g", Imperial, 0.25, "oz"},
	}
	for _, tc := range tests {
		q, u := ConvertUnits(tc.q, tc.unit, tc.system)
		if q != tc.wantQ || u != tc.wantU {
			t.Errorf("ConvertUnits(%v %s → %s) = %v %s, want %v %s", tc.q, tc.unit, tc.system, q, u, tc.wantQ, tc.wantU)
		}
	}
}
