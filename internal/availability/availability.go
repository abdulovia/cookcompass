// Package availability decides whether a recipe can be cooked with a given
// set of obtainable ingredients (a country's shops, or a user's pantry),
// allowing declared substitutes. It is pure logic with no I/O.
package availability

import (
	"github.com/abdulovia/cookcompass/internal/ingredients"
	"github.com/abdulovia/cookcompass/internal/recipes"
)

// Status of one ingredient line.
type Status string

// Line statuses.
const (
	Have            Status = "have"             // obtainable as written
	Staple          Status = "staple"           // assumed in every kitchen
	Substituted     Status = "substituted"      // replaced by an obtainable substitute
	Missing         Status = "missing"          // essential and not obtainable
	OptionalMissing Status = "optional_missing" // non-essential and not obtainable
	Unknown         Status = "unknown"          // not linked to a canonical ingredient
)

// Substitution records a replacement used to make a recipe feasible.
type Substitution struct {
	Line       int
	Missing    int64
	Substitute int64
	Ratio      string
}

// Result is the outcome for one recipe.
type Result struct {
	Available     bool
	Essential     int     // essential lines considered
	Satisfied     int     // essential lines obtainable (directly or substituted)
	Ratio         float64 // Satisfied / Essential (1 when Essential == 0)
	Lines         []Status
	Substitutions []Substitution
	MissingIDs    []int64 // essential ingredient ids that can't be obtained
}

// MissingCount is the number of essential lines that can't be satisfied.
func (r Result) MissingCount() int { return r.Essential - r.Satisfied }

// Checker evaluates recipes against an ingredient set.
type Checker struct {
	Have      map[int64]bool
	Staples   map[int64]bool
	Subs      map[int64][]ingredients.Substitute
	Threshold float64 // fraction of essential lines required, (0,1]; 0 means 1
}

// Check evaluates r.
func (c Checker) Check(r *recipes.Recipe) Result {
	threshold := c.Threshold
	if threshold <= 0 || threshold > 1 {
		threshold = 1
	}
	res := Result{Lines: make([]Status, len(r.Ingredients))}
	for i, in := range r.Ingredients {
		st := c.line(i, in, &res)
		res.Lines[i] = st
		if !in.Essential || st == Staple {
			continue
		}
		res.Essential++
		switch st {
		case Have, Substituted:
			res.Satisfied++
		default:
			if in.IngredientID != 0 {
				res.MissingIDs = append(res.MissingIDs, in.IngredientID)
			}
		}
	}
	res.Ratio = 1
	if res.Essential > 0 {
		res.Ratio = float64(res.Satisfied) / float64(res.Essential)
	}
	res.Available = res.Ratio >= threshold-1e-9
	return res
}

func (c Checker) line(i int, in recipes.Ingredient, res *Result) Status {
	id := in.IngredientID
	switch {
	case id == 0:
		if in.Essential {
			return Unknown
		}
		return OptionalMissing
	case c.Staples[id]:
		return Staple
	case c.Have[id]:
		return Have
	}
	for _, s := range c.Subs[id] {
		if c.Have[s.SubstituteID] || c.Staples[s.SubstituteID] {
			if in.Essential {
				res.Substitutions = append(res.Substitutions, Substitution{Line: i, Missing: id, Substitute: s.SubstituteID, Ratio: s.Ratio})
			}
			return Substituted
		}
	}
	if !in.Essential {
		return OptionalMissing
	}
	return Missing
}
