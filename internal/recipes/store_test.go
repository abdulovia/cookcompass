package recipes

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/abdulovia/cookcompass/internal/ingredients"
	"github.com/abdulovia/cookcompass/internal/testutil"
)

func newTestStore(t *testing.T, keep int) (*Store, *ingredients.Store) {
	t.Helper()
	d := testutil.DB(t)
	ings := ingredients.NewStore(d)
	ctx := context.Background()
	for _, in := range []ingredients.Ingredient{
		{Slug: "flour", Name: "flour", Names: map[string]string{"ru": "мука"}},
		{Slug: "milk", Name: "milk"},
		{Slug: "egg", Name: "egg"},
		{Slug: "bread", Name: "bread"},
		{Slug: "peanut", Name: "peanut", Aliases: []string{"peanuts"}},
	} {
		if _, err := ings.Upsert(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	s := NewStore(d, ings, keep)
	if _, err := s.UpsertCuisine(ctx, "american", "American"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertCuisine(ctx, "thai", "Thai"); err != nil {
		t.Fatal(err)
	}
	return s, ings
}

func pancakesInput() Input {
	return Input{
		Cuisine: "american", MealTimes: []string{"breakfast"}, Difficulty: Easy,
		PrepMinutes: 10, CookMinutes: 15, Servings: 4,
		VideoURL:  "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		Nutrition: Nutrition{Calories: 350, Protein: 10, Fat: 12, Carbs: 50, Fiber: 2},
		Ingredients: []InputIngredient{
			{Text: "2 cups flour", Ingredient: "flour"},
			{Text: "1 1/2 cups milk"},
			{Text: "2 eggs"},
			{Text: "berries, for serving", Essential: new(bool)},
		},
		I18n: map[string]InputText{
			"en": {Name: "Fluffy Pancakes", Steps: []string{"Mix.", "Cook."}},
			"ru": {Name: "Пышные панкейки", Steps: []string{"Смешайте.", "Жарьте."}, Ingredients: []string{"2 стакана муки"}},
		},
	}
}

func TestCRUDRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t, 3)

	// create
	r, err := s.FromInput(ctx, pancakesInput())
	if err != nil {
		t.Fatal(err)
	}
	r.Source = SourceSeed
	if err := s.Create(ctx, r, Change{Note: "seed"}); err != nil {
		t.Fatal(err)
	}
	if r.ID == 0 || r.Slug != "fluffy-pancakes" || r.Status != StatusApproved || r.DedupKey == "" {
		t.Fatalf("create did not populate fields: %+v", r)
	}

	// read
	got, err := s.GetBySlug(ctx, "fluffy-pancakes")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Ingredients) != 4 || got.Ingredients[0].IngredientID == 0 || got.Ingredients[1].IngredientID == 0 ||
		got.Ingredients[2].IngredientID == 0 || got.Ingredients[3].Essential {
		t.Fatalf("ingredients not resolved: %+v", got.Ingredients)
	}
	if *got.Ingredients[1].Quantity != 1.5 || got.Ingredients[1].Unit != "cup" {
		t.Fatalf("quantity parse lost: %+v", got.Ingredients[1])
	}
	if got.Translations["ru"].Name != "Пышные панкейки" || got.Cuisine != "american" || got.Source != SourceSeed {
		t.Fatalf("read mismatch: %+v", got)
	}

	// update (user edit) → revision 2
	got.Name = "Fluffy Buttermilk Pancakes"
	got.Source = SourceUser
	got.Steps = append(got.Steps, "Serve.")
	if err := s.Update(ctx, got, Change{Source: SourceUser, Note: "admin edit"}); err != nil {
		t.Fatal(err)
	}
	if got.Slug != "fluffy-pancakes" {
		t.Fatalf("slug must be stable on rename, got %s", got.Slug)
	}

	// soft delete → hidden from default list, still readable
	if err := s.SoftDelete(ctx, got.ID, Change{Source: SourceUser}); err != nil {
		t.Fatal(err)
	}
	if err := s.SoftDelete(ctx, got.ID, Change{Source: SourceUser}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double delete should be ErrNotFound, got %v", err)
	}
	list, _ := s.List(ctx, Filter{})
	if len(list) != 0 {
		t.Fatalf("deleted recipe listed: %d", len(list))
	}
	deleted, _ := s.List(ctx, Filter{Deleted: DeletedOnly})
	if len(deleted) != 1 || !deleted[0].Deleted() {
		t.Fatalf("deleted list wrong: %+v", deleted)
	}

	// restore
	if err := s.Restore(ctx, got.ID, Change{Source: SourceUser}); err != nil {
		t.Fatal(err)
	}
	if err := s.Restore(ctx, got.ID, Change{Source: SourceUser}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double restore should be ErrNotFound, got %v", err)
	}
	list, _ = s.List(ctx, Filter{})
	if len(list) != 1 || list[0].Name != "Fluffy Buttermilk Pancakes" {
		t.Fatalf("restore failed: %+v", list)
	}

	// revisions: create, edit, delete, restore = 4, pruned to keep=3
	revs, err := s.Revisions(ctx, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 3 || revs[0].Rev != 4 || revs[2].Rev != 2 {
		t.Fatalf("revisions not pruned to 3 newest: %+v", revs)
	}
	if revs[0].Note != "restored" || revs[1].Note != "deleted" || revs[2].Note != "admin edit" {
		t.Fatalf("revision notes %q %q %q", revs[0].Note, revs[1].Note, revs[2].Note)
	}
	if !strings.Contains(revs[1].Snapshot, `"deleted": true`) {
		t.Fatalf("delete snapshot should mark deleted:\n%s", revs[1].Snapshot)
	}
	var snap map[string]any
	if err := json.Unmarshal([]byte(revs[2].Snapshot), &snap); err != nil || snap["name"] != "Fluffy Buttermilk Pancakes" {
		t.Fatalf("snapshot content: %v %v", snap["name"], err)
	}
	if !DiffChanged(Diff(revs[2].Snapshot, revs[1].Snapshot)) {
		t.Fatal("diff between edit and delete should show changes")
	}
}

func TestCreateValidationAndErrors(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t, 20)
	r := &Recipe{Name: "x"}
	if err := s.Create(ctx, r, Change{}); err == nil {
		t.Fatal("invalid recipe accepted")
	}
	r = validRecipe()
	r.Cuisine = "atlantean"
	var ve ValidationError
	if err := s.Create(ctx, r, Change{}); !errors.As(err, &ve) || ve["cuisine"] == "" {
		t.Fatalf("unknown cuisine should fail validation, got %v", err)
	}
	if err := s.Update(ctx, validRecipe(), Change{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update without id: %v", err)
	}
	ghost := validRecipe()
	ghost.ID = 999
	if err := s.Update(ctx, ghost, Change{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing id: %v", err)
	}
	if _, err := s.Get(ctx, 12345); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing: %v", err)
	}
	if _, err := s.GetBySlug(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing slug: %v", err)
	}
	in := pancakesInput()
	in.Ingredients[0].Ingredient = "unobtainium"
	if _, err := s.FromInput(ctx, in); err == nil {
		t.Fatal("unknown ingredient slug accepted")
	}
	if err := s.SetStatus(ctx, 1, "weird", Change{}); err == nil {
		t.Fatal("invalid status accepted")
	}
}

func TestSlugUniqueness(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t, 20)
	a, b := validRecipe(), validRecipe()
	if err := s.Create(ctx, a, Change{}); err != nil {
		t.Fatal(err)
	}
	b.Ingredients = append(b.Ingredients, Ingredient{Name: "butter"})
	if err := s.Create(ctx, b, Change{}); err != nil {
		t.Fatal(err)
	}
	if a.Slug != "toast" || b.Slug != "toast-2" {
		t.Fatalf("slugs %s %s", a.Slug, b.Slug)
	}
}

func TestListFilters(t *testing.T) {
	ctx := context.Background()
	s, ings := newTestStore(t, 20)
	mk := func(name, cuisine, diff string, meals []string, ingr ...string) *Recipe {
		r := &Recipe{Name: name, Cuisine: cuisine, Difficulty: diff, MealTimes: meals, Servings: 2, Steps: []string{"Cook."}}
		for _, i := range ingr {
			r.Ingredients = append(r.Ingredients, Ingredient{Name: i, Essential: true})
		}
		if err := s.Create(ctx, r, Change{}); err != nil {
			t.Fatal(err)
		}
		return r
	}
	mk("Pancakes", "american", Easy, []string{"breakfast"}, "flour", "milk", "egg")
	pad := mk("Pad Thai", "thai", Medium, []string{"dinner", "lunch"}, "peanuts", "egg")
	mk("Toast", "", Easy, []string{"breakfast", "snack"}, "bread")
	pending := mk("Scraped Thing", "thai", Hard, []string{"dinner"}, "flour")
	if err := s.SetStatus(ctx, pending.ID, StatusPending, Change{}); err != nil {
		t.Fatal(err)
	}
	peanut, _ := ings.IDBySlug(ctx, "peanut")

	names := func(list []*Recipe) string {
		var n []string
		for _, r := range list {
			n = append(n, r.Name)
		}
		return strings.Join(n, ",")
	}
	tests := []struct {
		name string
		f    Filter
		want string
	}{
		{"all approved", Filter{}, "Pad Thai,Pancakes,Toast"},
		{"cuisine", Filter{Cuisine: "thai"}, "Pad Thai"},
		{"meal", Filter{MealTime: "breakfast"}, "Pancakes,Toast"},
		{"meal lunch", Filter{MealTime: "lunch"}, "Pad Thai"},
		{"difficulty", Filter{Difficulty: Medium}, "Pad Thai"},
		{"exclude allergen", Filter{ExcludeIngredientIDs: []int64{peanut}}, "Pancakes,Toast"},
		{"query", Filter{Query: "PAD"}, "Pad Thai"},
		{"query paged", Filter{Query: "a", Limit: 1, Offset: 1}, "Pancakes"},
		{"paged", Filter{Limit: 2, Offset: 1}, "Pancakes,Toast"},
		{"ids", Filter{IDs: []int64{pad.ID}}, "Pad Thai"},
		{"pending", Filter{Statuses: []string{StatusPending}}, "Scraped Thing"},
		{"source", Filter{Source: SourceUser}, "Pad Thai,Pancakes,Toast"},
		{"source prefix", Filter{Source: "scraped:"}, ""},
		{"needs review", Filter{NeedsReview: true}, ""},
		{"offset past end", Filter{Query: "a", Offset: 10}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			list, err := s.List(ctx, tc.f)
			if err != nil {
				t.Fatal(err)
			}
			if got := names(list); got != tc.want {
				t.Errorf("got %q want %q", got, tc.want)
			}
		})
	}
	counts, err := s.Counts(ctx)
	if err != nil || counts[StatusApproved] != 3 || counts[StatusPending] != 1 {
		t.Fatalf("counts %v %v", counts, err)
	}
	cs, err := s.Cuisines(ctx)
	if err != nil || len(cs) != 2 || cs[0].Slug != "american" {
		t.Fatalf("cuisines %v %v", cs, err)
	}
}

func TestDedupAndQueue(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t, 20)
	r := validRecipe()
	r.Source = ScrapedSource("example.com")
	r.Status = StatusPending
	r.CanonicalURL = "https://www.example.com/toast/?utm_source=x"
	if err := s.Create(ctx, r, Change{}); err != nil {
		t.Fatal(err)
	}
	if r.CanonicalURL != "https://example.com/toast" {
		t.Fatalf("canonical url not normalized: %s", r.CanonicalURL)
	}
	dup, err := s.FindDuplicate(ctx, "http://example.com/toast", "")
	if err != nil || dup == nil || dup.ID != r.ID {
		t.Fatalf("canonical dedup failed: %v %v", dup, err)
	}
	dup, err = s.FindDuplicate(ctx, "", RecipeContentKey(validRecipeValidated()))
	if err != nil || dup == nil || dup.ID != r.ID {
		t.Fatalf("content dedup failed: %v %v", dup, err)
	}
	dup, err = s.FindDuplicate(ctx, "https://other.com/x", "nothing|0")
	if err != nil || dup != nil {
		t.Fatalf("expected no duplicate, got %v %v", dup, err)
	}

	incoming := validRecipe()
	incoming.Name = "Changed upstream"
	if err := s.RecordWithheld(ctx, r.ID, incoming, Change{Source: r.Source, Note: "withheld"}); err != nil {
		t.Fatal(err)
	}
	revs, _ := s.Revisions(ctx, r.ID)
	if len(revs) != 2 || revs[0].Applied || !strings.Contains(revs[0].Snapshot, "Changed upstream") {
		t.Fatalf("withheld revision wrong: %+v", revs)
	}
	cur, _ := s.Get(ctx, r.ID)
	if cur.Name != "Toast" {
		t.Fatal("withheld change must not alter the recipe")
	}

	n, err := s.ApproveAllPending(ctx, Change{Source: "cli"})
	if err != nil || n != 1 {
		t.Fatalf("approve all %d %v", n, err)
	}
	cur, _ = s.Get(ctx, r.ID)
	if cur.Status != StatusApproved {
		t.Fatal("not approved")
	}

	p := validRecipe()
	p.Name = "Pending one"
	p.Status = StatusPending
	if err := s.Create(ctx, p, Change{}); err != nil {
		t.Fatal(err)
	}
	n, err = s.PurgePending(ctx)
	if err != nil || n != 1 {
		t.Fatalf("purge %d %v", n, err)
	}
	if _, err := s.Get(ctx, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("purged recipe still present")
	}
}

func validRecipeValidated() *Recipe {
	r := validRecipe()
	_ = r.Validate()
	return r
}
