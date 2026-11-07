// Package nutrition computes daily energy and macro targets (Mifflin–St
// Jeor + activity factor + goal adjustment) and composes a day of real
// recipes that hits them.
package nutrition

import (
	"errors"
	"math"
)

// Sexes.
const (
	Male   = "male"
	Female = "female"
)

// Activity levels and their TDEE multipliers.
var ActivityFactors = map[string]float64{
	"sedentary":   1.2,   // desk job, little exercise
	"light":       1.375, // light exercise 1–3 days/week
	"moderate":    1.55,  // moderate exercise 3–5 days/week
	"active":      1.725, // hard exercise 6–7 days/week
	"very_active": 1.9,   // physical job or twice-daily training
}

// Activities lists activity levels in increasing order.
var Activities = []string{"sedentary", "light", "moderate", "active", "very_active"}

// Goals.
const (
	LoseWeight   = "lose_weight"
	Maintain     = "maintain"
	GainMuscle   = "gain_muscle"
	GainWeight   = "gain_weight"
	EatHealthier = "eat_healthier"
)

// Goals lists the supported goals in display order.
var Goals = []string{LoseWeight, Maintain, GainMuscle, GainWeight, EatHealthier}

type goalRule struct {
	kcalDelta   float64 // added to TDEE
	proteinPerK float64 // g protein per kg body weight
	fatShare    float64 // share of kcal from fat
}

var goalRules = map[string]goalRule{
	LoseWeight:   {-500, 1.6, 0.30},
	Maintain:     {0, 1.2, 0.30},
	GainMuscle:   {300, 2.0, 0.25},
	GainWeight:   {500, 1.5, 0.30},
	EatHealthier: {0, 1.2, 0.30},
}

// Input describes the person.
type Input struct {
	HeightCm float64 `json:"height_cm"`
	WeightKg float64 `json:"weight_kg"`
	Age      int     `json:"age"`
	Sex      string  `json:"sex"`
	Activity string  `json:"activity"`
	Goal     string  `json:"goal"`
}

// ValidationError maps field → i18n message id.
type ValidationError map[string]string

func (v ValidationError) Error() string { return "invalid nutrition input" }

// Validate checks plausible adult ranges.
func (in Input) Validate() error {
	errs := ValidationError{}
	if in.HeightCm < 100 || in.HeightCm > 250 {
		errs["height"] = "validation.out_of_range"
	}
	if in.WeightKg < 30 || in.WeightKg > 300 {
		errs["weight"] = "validation.out_of_range"
	}
	if in.Age < 14 || in.Age > 100 {
		errs["age"] = "validation.out_of_range"
	}
	if in.Sex != Male && in.Sex != Female {
		errs["sex"] = "validation.invalid_choice"
	}
	if _, ok := ActivityFactors[in.Activity]; !ok {
		errs["activity"] = "validation.invalid_choice"
	}
	if _, ok := goalRules[in.Goal]; !ok {
		errs["goal"] = "validation.invalid_choice"
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

// BMR is the Mifflin–St Jeor basal metabolic rate in kcal/day.
func BMR(in Input) float64 {
	b := 10*in.WeightKg + 6.25*in.HeightCm - 5*float64(in.Age)
	if in.Sex == Female {
		return b - 161
	}
	return b + 5
}

// TDEE is total daily energy expenditure: BMR × activity factor.
func TDEE(in Input) float64 { return BMR(in) * ActivityFactors[in.Activity] }

// Targets are daily goals.
type Targets struct {
	BMR      float64 `json:"bmr"`
	TDEE     float64 `json:"tdee"`
	Calories float64 `json:"calories"`
	Protein  float64 `json:"protein_g"`
	Fat      float64 `json:"fat_g"`
	Carbs    float64 `json:"carbs_g"`
	Fiber    float64 `json:"fiber_g"`
}

// Minimum daily intake we will ever suggest without medical supervision.
const (
	minKcalFemale = 1200
	minKcalMale   = 1500
)

// ErrInvalid wraps validation failures.
var ErrInvalid = errors.New("invalid input")

// Compute derives daily targets for in.
func Compute(in Input) (Targets, error) {
	if err := in.Validate(); err != nil {
		return Targets{}, err
	}
	rule := goalRules[in.Goal]
	t := Targets{BMR: math.Round(BMR(in)), TDEE: math.Round(TDEE(in))}
	kcal := TDEE(in) + rule.kcalDelta
	floor := float64(minKcalMale)
	if in.Sex == Female {
		floor = minKcalFemale
	}
	kcal = math.Max(kcal, floor)
	t.Calories = math.Round(kcal/10) * 10
	t.Protein = math.Round(in.WeightKg * rule.proteinPerK)
	// Protein never exceeds 30% of energy, even for very light people on a cut.
	t.Protein = math.Min(t.Protein, math.Round(t.Calories*0.30/4))
	t.Fat = math.Round(t.Calories * rule.fatShare / 9)
	t.Carbs = math.Max(0, math.Round((t.Calories-t.Protein*4-t.Fat*9)/4))
	t.Fiber = math.Max(25, math.Round(t.Calories/1000*14))
	return t, nil
}
