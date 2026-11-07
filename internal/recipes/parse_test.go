package recipes

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/abdulovia/cookcompass/internal/testutil"
)

func fp(f float64) *float64 { return &f }

func TestParseIngredientTable(t *testing.T) {
	tests := []struct {
		in   string
		want ParsedIngredient
	}{
		{"2 1/2 cups all-purpose flour, sifted", ParsedIngredient{fp(2.5), "cup", "all-purpose flour", "sifted", false}},
		{"½ tsp salt", ParsedIngredient{fp(0.5), "tsp", "salt", "", false}},
		{"200g butter", ParsedIngredient{fp(200), "g", "butter", "", false}},
		{"3 large eggs", ParsedIngredient{fp(3), "", "large eggs", "", false}},
		{"salt to taste", ParsedIngredient{nil, "to taste", "salt", "to taste", false}},
		{"a pinch of saffron", ParsedIngredient{fp(1), "pinch", "saffron", "", false}},
		{"handful of spinach", ParsedIngredient{nil, "handful", "spinach", "", true}},
		{"mystery ingredient", ParsedIngredient{nil, "", "mystery ingredient", "", true}},
		{"3", ParsedIngredient{fp(3), "", "", "", true}},
		{"1 c", ParsedIngredient{fp(1), "", "c", "", false}},
		{"2 ст. л. сметаны", ParsedIngredient{fp(2), "tbsp", "сметаны", "", false}},
		{"1 1/0 cup x", ParsedIngredient{fp(1), "", "1/0 cup x", "", false}},
	}
	for _, tc := range tests {
		got := ParseIngredient(tc.in)
		if !sameQty(got.Quantity, tc.want.Quantity) || got.Unit != tc.want.Unit || got.Name != tc.want.Name ||
			got.Note != tc.want.Note || got.NeedsReview != tc.want.NeedsReview {
			t.Errorf("ParseIngredient(%q)\n got  %+v qty=%v\n want %+v qty=%v", tc.in, got, deref(got.Quantity), tc.want, deref(tc.want.Quantity))
		}
	}
}

func sameQty(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a-*b < 1e-9 && *b-*a < 1e-9
}

func deref(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

// TestParseIngredientGolden locks down normalization for a corpus of real
// ingredient lines (English and Russian).
func TestParseIngredientGolden(t *testing.T) {
	f, err := os.Open("testdata/ingredient_lines.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	type row struct {
		Line string           `json:"line"`
		Out  ParsedIngredient `json:"parsed"`
	}
	var rows []row
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			rows = append(rows, row{line, ParseIngredient(line)})
		}
	}
	testutil.Golden(t, "ingredients", rows)
}

func TestFormatQuantity(t *testing.T) {
	tests := map[float64]string{0: "", 1: "1", 0.5: "½", 1.5: "1 ½", 2.25: "2 ¼", 0.333: "⅓", 1.2: "1.2", 2.999: "3", 0.125: "⅛"}
	for in, want := range tests {
		if got := FormatQuantity(in); got != want {
			t.Errorf("FormatQuantity(%v) = %q want %q", in, got, want)
		}
	}
}
