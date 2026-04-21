package nutrition

import (
	"context"
	"math"
	"testing"

	"github.com/abdulovia/cookcompass/internal/catalog"
	"github.com/abdulovia/cookcompass/internal/ingredients"
	"github.com/abdulovia/cookcompass/internal/recipes"
	"github.com/abdulovia/cookcompass/internal/seed"
	"github.com/abdulovia/cookcompass/internal/shops"
	"github.com/abdulovia/cookcompass/internal/testutil"
	seedfs "github.com/abdulovia/cookcompass/seed"
)

// Reference values computed by hand from the Mifflin–St Jeor equation.
func TestBMRAndTDEEKnownCases(t *testing.T) {
	tests := []struct {
		name      string
		in        Input
		bmr, tdee float64
	}{
		{"male 80kg 180cm 30y moderate", Input{180, 80, 30, Male, "moderate", Maintain}, 1780, 2759},
		{"female 60kg 165cm 25y sedentary", Input{165, 60, 25, Female, "sedentary", Maintain}, 1345.25, 1614.3},
		{"female 70kg 170cm 40y active", Input{170, 70, 40, Female, "active", Maintain}, 1401.5, 2417.5875},
		{"male 95kg 190cm 55y very active", Input{190, 95, 55, Male, "very_active", Maintain}, 1867.5, 3548.25},
		{"male 60kg 165cm 18y light", Input{165, 60, 18, Male, "light", Maintain}, 1546.25, 2126.09375},
	}
	for _, tc := range tests {
		if got := BMR(tc.in); math.Abs(got-tc.bmr) > 1e-6 {
			t.Errorf("%s: BMR %v want %v", tc.name, got, tc.bmr)
		}
		if got := TDEE(tc.in); math.Abs(got-tc.tdee) > 1e-6 {
			t.Errorf("%s: TDEE %v want %v", tc.name, got, tc.tdee)
		}
	}
}

func TestComputeGoals(t *testing.T) {
	base := Input{HeightCm: 180, WeightKg: 80, Age: 30, Sex: Male, Activity: "moderate"}
	kcal := map[string]float64{LoseWeight: 2260, Maintain: 2760, GainMuscle: 3060, GainWeight: 3260, EatHealthier: 2760}
	for goal, want := range kcal {
		in := base
		in.Goal = goal
		got, err := Compute(in)
		if err != nil {
			t.Fatal(err)
		}
		if got.Calories != want {
			t.Errorf("%s: kcal %v want %v", goal, got.Calories, want)
		}
		energy := got.Protein*4 + got.Fat*9 + got.Carbs*4
		if math.Abs(energy-got.Calories) > 10 {
			t.Errorf("%s: macros sum to %v kcal, target %v", goal, energy, got.Calories)
		}
	}
	// Calorie floor for small, sedentary people on a cut.
	small, _ := Compute(Input{150, 45, 70, Female, "sedentary", LoseWeight})
	if small.Calories != minKcalFemale {
		t.Errorf("female floor: %v", small.Calories)
	}
	smallM, _ := Compute(Input{155, 50, 80, Male, "sedentary", LoseWeight})
	if smallM.Calories != minKcalMale {
		t.Errorf("male floor: %v", smallM.Calories)
	}
	if small.Protein > small.Calories*0.30/4+1 {
		t.Errorf("protein cap: %v", small.Protein)
	}
}

func TestValidate(t *testing.T) {
	bad := Input{HeightCm: 50, WeightKg: 500, Age: 5, Sex: "x", Activity: "couch", Goal: "fly"}
	_, err := Compute(bad)
	ve, ok := err.(ValidationError)
	if !ok || len(ve) != 6 || ve.Error() == "" {
		t.Fatalf("want 6 field errors, got %v", err)
	}
}

// TestTargetsGolden locks down targets for a spread of profiles.
func TestTargetsGolden(t *testing.T) {
	type row struct {
		In  Input   `json:"input"`
		Out Targets `json:"targets"`
	}
	var rows []row
	for _, in := range []Input{
		{180, 80, 30, Male, "moderate", Maintain},
		{165, 60, 25, Female, "sedentary", LoseWeight},
		{175, 72, 22, Male, "active", GainMuscle},
		{160, 50, 19, Female, "light", GainWeight},
		{170, 68, 45, Female, "moderate", EatHealthier},
		{190, 110, 50, Male, "sedentary", LoseWeight},
	} {
		out, err := Compute(in)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row{in, out})
	}
	testutil.Golden(t, "targets", rows)
}

func cand(id int64, slug string, meals []string, kcal, p, f, c float64) Candidate {
	return Candidate{ID: id, Slug: slug, MealTimes: meals, PerServe: recipes.Nutrition{Calories: kcal, Protein: p, Fat: f, Carbs: c}}
}

func TestSuggestSmall(t *testing.T) {
	cands := []Candidate{
		cand(1, "oats", []string{"breakfast"}, 350, 12, 8, 55),
		cand(2, "eggs", []string{"breakfast"}, 300, 20, 22, 2),
		cand(3, "chicken-rice", []string{"lunch", "dinner"}, 600, 45, 15, 70),
		cand(4, "pasta", []string{"lunch", "dinner"}, 650, 25, 20, 90),
		cand(5, "salad", []string{"lunch"}, 300, 10, 20, 15),
		cand(6, "steak", []string{"dinner"}, 700, 55, 40, 20),
		cand(7, "yogurt", []string{"snack"}, 150, 12, 5, 15),
		cand(8, "nuts", []string{"snack"}, 200, 7, 17, 6),
	}
	tg := Targets{Calories: 2200, Protein: 120, Fat: 70, Carbs: 250}
	p := Suggest(cands, tg, 1)
	if n := len(p.Items); n < 3 || n > 5 {
		t.Fatalf("plan has %d items", n)
	}
	if math.Abs(p.Deviation.Calories) > Tolerance {
		t.Fatalf("calories off by %.1f%%: %+v", p.Deviation.Calories*100, p)
	}
	seen := map[string]bool{}
	for i, it := range p.Items {
		if seen[it.Slug] {
			t.Fatalf("recipe repeated: %s", it.Slug)
		}
		seen[it.Slug] = true
		if i < 3 && it.Meal != mainSlots[i] {
			t.Fatalf("slot order %v", p.Items)
		}
		if it.Servings < minServings || it.Servings > maxServings {
			t.Fatalf("servings %v", it.Servings)
		}
	}
	// Deterministic for the same seed.
	if q := Suggest(cands, tg, 1); q.Items[0].Slug != p.Items[0].Slug || q.Total != p.Total {
		t.Fatal("same seed should give the same plan")
	}
	// Empty inputs.
	if e := Suggest(nil, tg, 1); len(e.Items) != 0 {
		t.Fatal("no candidates → empty plan")
	}
	if e := Suggest(cands, Targets{}, 1); len(e.Items) != 0 {
		t.Fatal("no targets → empty plan")
	}
	// Only lunch recipes: every slot falls back to any recipe.
	onlyLunch := []Candidate{cands[2], cands[3], cands[4]}
	if e := Suggest(onlyLunch, tg, 3); len(e.Items) != 3 {
		t.Fatalf("fallback slots: %+v", e)
	}
	if e := Suggest(onlyLunch[:2], tg, 3); len(e.Items) != 0 {
		t.Fatal("two recipes can't fill three distinct slots")
	}
}

// TestSuggestWithSeedRecipes: for every goal the plan built from the real
// seed recipes lands within ±10% on calories, and the full plan is golden.
func TestSuggestWithSeedRecipes(t *testing.T) {
	d := testutil.DB(t)
	ings := ingredients.NewStore(d)
	rs := recipes.NewStore(d, ings, 20)
	if _, err := (&seed.Seeder{FS: seedfs.FS, Ings: ings, Recipes: rs, Catalog: catalog.NewStore(d, ings), Shops: shops.NewStore(d)}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	list, err := rs.List(context.Background(), recipes.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	cands := make([]Candidate, len(list))
	for i, r := range list {
		cands[i] = Candidate{ID: r.ID, Slug: r.Slug, MealTimes: r.MealTimes, PerServe: r.Nutrition}
	}
	type row struct {
		In   Input `json:"input"`
		Plan Plan  `json:"plan"`
	}
	var rows []row
	for _, in := range []Input{
		{180, 80, 30, Male, "moderate", Maintain},
		{165, 60, 25, Female, "sedentary", LoseWeight},
		{175, 72, 22, Male, "active", GainMuscle},
		{160, 50, 19, Female, "light", GainWeight},
		{170, 68, 45, Female, "moderate", EatHealthier},
	} {
		tg, _ := Compute(in)
		p := Suggest(cands, tg, 20261107)
		if math.Abs(p.Deviation.Calories) > Tolerance {
			t.Errorf("%s: calories off by %.1f%%", in.Goal, p.Deviation.Calories*100)
		}
		if math.Abs(p.Deviation.Protein) > 0.25 {
			t.Errorf("%s: protein off by %.1f%%", in.Goal, p.Deviation.Protein*100)
		}
		rows = append(rows, row{in, p})
	}
	testutil.Golden(t, "plans", rows)
}
