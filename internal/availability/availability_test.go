package availability

import (
	"reflect"
	"testing"

	"github.com/abdulovia/cookcompass/internal/ingredients"
	"github.com/abdulovia/cookcompass/internal/recipes"
)

const (
	flour = iota + 1
	milk
	egg
	salt
	fishSauce
	soySauce
	tahini
	peanutButter
	berries
	gochujang
	sriracha
)

func recipe(lines ...recipes.Ingredient) *recipes.Recipe { return &recipes.Recipe{Ingredients: lines} }
func ess(id int64) recipes.Ingredient {
	return recipes.Ingredient{IngredientID: id, Name: "x", Essential: true}
}
func opt(id int64) recipes.Ingredient { return recipes.Ingredient{IngredientID: id, Name: "x"} }

func checker(have ...int64) Checker {
	h := map[int64]bool{}
	for _, id := range have {
		h[id] = true
	}
	return Checker{
		Have:    h,
		Staples: map[int64]bool{salt: true},
		Subs: map[int64][]ingredients.Substitute{
			fishSauce: {{IngredientID: fishSauce, SubstituteID: soySauce, Ratio: "1:1"}},
			tahini:    {{IngredientID: tahini, SubstituteID: peanutButter, Ratio: "1:1"}},
			gochujang: {{IngredientID: gochujang, SubstituteID: sriracha, Ratio: "½ amount"}},
		},
	}
}

func TestCheck(t *testing.T) {
	tests := []struct {
		name      string
		c         Checker
		r         *recipes.Recipe
		available bool
		lines     []Status
		subs      int
		missing   []int64
	}{
		{"all present", checker(flour, milk, egg), recipe(ess(flour), ess(milk), ess(egg)), true,
			[]Status{Have, Have, Have}, 0, nil},
		{"staple never blocks", checker(flour), recipe(ess(flour), ess(salt)), true,
			[]Status{Have, Staple}, 0, nil},
		{"missing essential", checker(flour), recipe(ess(flour), ess(milk)), false,
			[]Status{Have, Missing}, 0, []int64{milk}},
		{"optional missing is fine", checker(flour), recipe(ess(flour), opt(berries)), true,
			[]Status{Have, OptionalMissing}, 0, nil},
		{"substitute makes it available", checker(soySauce), recipe(ess(fishSauce)), true,
			[]Status{Substituted}, 1, nil},
		{"substitute also missing", checker(), recipe(ess(gochujang)), false,
			[]Status{Missing}, 0, []int64{gochujang}},
		{"optional substituted records no substitution", checker(peanutButter), recipe(opt(tahini)), true,
			[]Status{Substituted}, 0, nil},
		{"unlinked essential is unknown", checker(flour), recipe(ess(flour), recipes.Ingredient{Name: "mystery", Essential: true}), false,
			[]Status{Have, Unknown}, 0, nil},
		{"unlinked optional", checker(), recipe(recipes.Ingredient{Name: "garnish"}), true,
			[]Status{OptionalMissing}, 0, nil},
		{"no ingredients", checker(), recipe(), true, []Status{}, 0, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.c.Check(tc.r)
			if got.Available != tc.available {
				t.Errorf("available = %v want %v (%+v)", got.Available, tc.available, got)
			}
			if !reflect.DeepEqual(got.Lines, tc.lines) {
				t.Errorf("lines = %v want %v", got.Lines, tc.lines)
			}
			if len(got.Substitutions) != tc.subs {
				t.Errorf("subs = %v", got.Substitutions)
			}
			if !reflect.DeepEqual(got.MissingIDs, tc.missing) {
				t.Errorf("missing = %v want %v", got.MissingIDs, tc.missing)
			}
		})
	}
}

func TestThreshold(t *testing.T) {
	r := recipe(ess(flour), ess(milk), ess(egg), ess(berries))
	c := checker(flour, milk, egg)
	if c.Check(r).Available {
		t.Fatal("default threshold is 100%")
	}
	c.Threshold = 0.75
	res := c.Check(r)
	if !res.Available || res.Ratio != 0.75 || res.MissingCount() != 1 {
		t.Fatalf("75%% threshold: %+v", res)
	}
	c.Threshold = 0.8
	if c.Check(r).Available {
		t.Fatal("80% threshold should fail with 3/4")
	}
	c.Threshold = 7 // invalid → treated as 1
	if c.Check(r).Available {
		t.Fatal("invalid threshold should mean 100%")
	}
}

func TestSubstitutionDetails(t *testing.T) {
	res := checker(sriracha).Check(recipe(ess(flour), ess(gochujang)))
	want := []Substitution{{Line: 1, Missing: gochujang, Substitute: sriracha, Ratio: "½ amount"}}
	if !reflect.DeepEqual(res.Substitutions, want) {
		t.Fatalf("substitutions %+v", res.Substitutions)
	}
	// A staple can act as a substitute too.
	c := checker()
	c.Subs[milk] = []ingredients.Substitute{{IngredientID: milk, SubstituteID: salt}}
	if !c.Check(recipe(ess(milk))).Available {
		t.Fatal("staple substitute should count")
	}
}
