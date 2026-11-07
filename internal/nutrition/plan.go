package nutrition

import (
	"math"
	"sort"

	"github.com/abdulovia/cookcompass/internal/recipes"
)

// Tolerance is the allowed relative deviation from each target.
const Tolerance = 0.10

// Candidate is a recipe that may be suggested.
type Candidate struct {
	ID        int64             `json:"id"`
	Slug      string            `json:"slug"`
	MealTimes []string          `json:"meal_times"`
	PerServe  recipes.Nutrition `json:"per_serving"`
}

func (c Candidate) has(meal string) bool {
	for _, m := range c.MealTimes {
		if m == meal {
			return true
		}
	}
	return false
}

// PlanItem is one meal of the day.
type PlanItem struct {
	Slug     string            `json:"slug"`
	Meal     string            `json:"meal"`
	Servings float64           `json:"servings"`
	Totals   recipes.Nutrition `json:"totals"`
	ID       int64             `json:"-"`
}

// Deviation is the relative error per macro (+0.05 = 5% over target).
type Deviation struct {
	Calories float64 `json:"calories"`
	Protein  float64 `json:"protein"`
	Fat      float64 `json:"fat"`
	Carbs    float64 `json:"carbs"`
}

// Within reports whether every macro is within tolerance.
func (d Deviation) Within() bool { return d.outside() == 0 }

func (d Deviation) outside() int {
	n := 0
	for _, v := range []float64{d.Calories, d.Protein, d.Fat, d.Carbs} {
		if math.Abs(v) > Tolerance+1e-9 {
			n++
		}
	}
	return n
}

func (d Deviation) score() float64 {
	return 3*d.Calories*d.Calories + 2*d.Protein*d.Protein + d.Fat*d.Fat + d.Carbs*d.Carbs
}

// Plan is a suggested day of eating.
type Plan struct {
	Items     []PlanItem        `json:"items"`
	Total     recipes.Nutrition `json:"total"`
	Targets   Targets           `json:"targets"`
	Deviation Deviation         `json:"deviation"`
	Within    bool              `json:"within_tolerance"`
}

// Slots are the meals of a day; snacks are optional extras.
var mainSlots = []string{recipes.Breakfast, recipes.Lunch, recipes.Dinner}

const (
	maxSnacks    = 2
	minServings  = 0.5
	maxServings  = 2.5
	servingStep  = 0.25
	refineTop    = 40
	varietyPool  = 6
	maxPerSlot   = 25
	searchBudget = 400000
)

type combo struct {
	ids   []int // indexes into candidates
	meals []string
	serve []float64
	dev   Deviation
	total recipes.Nutrition
	// penalty discourages unusual portions (far from one serving).
	penalty float64
}

// servingPenalty weighs portion oddness against macro accuracy: two
// servings of a dish costs about as much as being 6% off on calories.
const servingPenalty = 0.01

func (c *combo) score() float64 { return c.dev.score() + c.penalty }

// Suggest picks 3–5 recipes (breakfast, lunch, dinner and up to two snacks)
// whose combined nutrition is as close as possible to t, scaling servings in
// quarter steps. seed rotates between equally good plans so the suggestion
// changes from day to day. With no candidates it returns an empty plan.
func Suggest(cands []Candidate, t Targets, seed int64) Plan {
	plan := Plan{Targets: t}
	if len(cands) == 0 || t.Calories <= 0 {
		return plan
	}
	slot := func(meal string) []int {
		var idx []int
		for i, c := range cands {
			if c.has(meal) && c.PerServe.Calories > 0 {
				idx = append(idx, i)
			}
		}
		if len(idx) == 0 { // no recipe tagged for this meal: anything goes
			for i, c := range cands {
				if c.PerServe.Calories > 0 {
					idx = append(idx, i)
				}
			}
		}
		if len(idx) > maxPerSlot {
			idx = idx[:maxPerSlot]
		}
		return idx
	}
	b, l, d := slot(recipes.Breakfast), slot(recipes.Lunch), slot(recipes.Dinner)
	snacks := slot(recipes.Snack)
	snackSets := [][]int{{}}
	for i := range snacks {
		snackSets = append(snackSets, []int{snacks[i]})
		for j := i + 1; j < len(snacks) && maxSnacks >= 2; j++ {
			snackSets = append(snackSets, []int{snacks[i], snacks[j]})
		}
	}

	var all []combo
	budget := searchBudget
	for _, bi := range b {
		for _, li := range l {
			for _, di := range d {
				if bi == li || bi == di || li == di {
					continue
				}
				for _, ss := range snackSets {
					if budget--; budget < 0 {
						break
					}
					ids := append([]int{bi, li, di}, ss...)
					if !distinct(ids) {
						continue
					}
					meals := append(append([]string{}, mainSlots...), repeat(recipes.Snack, len(ss))...)
					c := combo{ids: ids, meals: meals}
					c.uniformScale(cands, t)
					all = append(all, c)
				}
			}
		}
	}
	if len(all) == 0 {
		return plan
	}
	rank := func(cs []combo) {
		sort.SliceStable(cs, func(i, j int) bool {
			oi, oj := cs[i].dev.outside(), cs[j].dev.outside()
			if oi != oj {
				return oi < oj
			}
			return cs[i].score() < cs[j].score()
		})
	}
	rank(all)
	top := all[:min(refineTop, len(all))]
	for i := range top {
		top[i].refine(cands, t)
	}
	rank(top)
	// Variety: choose among the best plans that share the best tolerance count.
	best := top[0].dev.outside()
	n := 0
	for n < len(top) && n < varietyPool && top[n].dev.outside() == best {
		n++
	}
	pick := top[int(uint64(seed)%uint64(n))]
	for k, i := range pick.ids {
		c := cands[i]
		plan.Items = append(plan.Items, PlanItem{ID: c.ID, Slug: c.Slug, Meal: pick.meals[k], Servings: pick.serve[k],
			Totals: roundN(c.PerServe.Scale(pick.serve[k]))})
	}
	plan.Total = roundN(pick.total)
	plan.Deviation = roundDev(pick.dev)
	plan.Within = pick.dev.Within()
	return plan
}

func (c *combo) evaluate(cands []Candidate, t Targets) {
	var total recipes.Nutrition
	for k, i := range c.ids {
		total = total.Add(cands[i].PerServe.Scale(c.serve[k]))
	}
	c.total = total
	c.penalty = 0
	for _, s := range c.serve {
		c.penalty += servingPenalty * (s - 1) * (s - 1)
	}
	c.dev = Deviation{
		Calories: rel(total.Calories, t.Calories),
		Protein:  rel(total.Protein, t.Protein),
		Fat:      rel(total.Fat, t.Fat),
		Carbs:    rel(total.Carbs, t.Carbs),
	}
}

// uniformScale gives every item the same serving count that best matches
// the calorie target.
func (c *combo) uniformScale(cands []Candidate, t Targets) {
	kcal := 0.0
	for _, i := range c.ids {
		kcal += cands[i].PerServe.Calories
	}
	s := clampServing(t.Calories / kcal)
	c.serve = make([]float64, len(c.ids))
	for k := range c.serve {
		c.serve[k] = s
	}
	c.evaluate(cands, t)
}

// refine adjusts individual servings by quarter steps while that improves
// the plan (coordinate descent; a handful of passes is enough).
func (c *combo) refine(cands []Candidate, t Targets) {
	better := func(a Deviation, ap float64, b Deviation, bp float64) bool {
		if a.outside() != b.outside() {
			return a.outside() < b.outside()
		}
		return a.score()+ap < b.score()+bp-1e-12
	}
	for pass := 0; pass < 6; pass++ {
		improved := false
		for k := range c.serve {
			for _, delta := range []float64{servingStep, -servingStep} {
				old := c.serve[k]
				next := clampServing(old + delta)
				if next == old {
					continue
				}
				prev, prevTotal, prevPen := c.dev, c.total, c.penalty
				c.serve[k] = next
				c.evaluate(cands, t)
				if better(c.dev, c.penalty, prev, prevPen) {
					improved = true
					continue
				}
				c.serve[k] = old
				c.dev, c.total, c.penalty = prev, prevTotal, prevPen
			}
		}
		if !improved {
			return
		}
	}
}

func clampServing(s float64) float64 {
	s = math.Round(s/servingStep) * servingStep
	return math.Min(maxServings, math.Max(minServings, s))
}

func rel(got, want float64) float64 {
	if want <= 0 {
		return 0
	}
	return (got - want) / want
}

func distinct(ids []int) bool {
	seen := map[int]bool{}
	for _, i := range ids {
		if seen[i] {
			return false
		}
		seen[i] = true
	}
	return true
}

func repeat(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

func roundN(n recipes.Nutrition) recipes.Nutrition {
	r := func(v float64) float64 { return math.Round(v) }
	return recipes.Nutrition{Calories: r(n.Calories), Protein: r(n.Protein), Fat: r(n.Fat), Carbs: r(n.Carbs), Fiber: r(n.Fiber),
		Sugar: r(n.Sugar), Sodium: r(n.Sodium), SaturatedFat: r(n.SaturatedFat)}
}

func roundDev(d Deviation) Deviation {
	r := func(v float64) float64 { return math.Round(v*1000) / 1000 }
	return Deviation{r(d.Calories), r(d.Protein), r(d.Fat), r(d.Carbs)}
}
