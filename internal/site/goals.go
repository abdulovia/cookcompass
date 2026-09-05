package site

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/abdulovia/cookcompass/internal/nutrition"
	"github.com/abdulovia/cookcompass/internal/pool"
	"github.com/abdulovia/cookcompass/internal/recipes"
	"github.com/abdulovia/cookcompass/internal/users"
	"github.com/abdulovia/cookcompass/internal/web"
)

// PlanView is a plan with the recipes it refers to, ready for templates.
type PlanView struct {
	Plan    nutrition.Plan
	Targets nutrition.Targets
	Items   []PlanItemView
	Shuffle int
}

// PlanItemView pairs a plan item with its recipe.
type PlanItemView struct {
	nutrition.PlanItem
	Recipe *recipes.Recipe
}

// goalForm keeps raw input for redisplay.
type goalForm struct {
	HeightCm, WeightKg, HeightFt, HeightIn, WeightLb, Age string
	Sex, Activity, Goal                                   string
}

func inputFromProfile(p *users.Profile) nutrition.Input {
	return nutrition.Input{HeightCm: p.HeightCm, WeightKg: p.WeightKg, Age: p.Age, Sex: p.Sex, Activity: p.Activity, Goal: p.Goal}
}

func formFromProfile(p *users.Profile) goalForm {
	f := goalForm{Sex: p.Sex, Activity: p.Activity, Goal: p.Goal}
	if p.HeightCm > 0 {
		f.HeightCm = strconv.FormatFloat(p.HeightCm, 'f', -1, 64)
		inches := p.HeightCm / 2.54
		f.HeightFt = strconv.Itoa(int(inches / 12))
		f.HeightIn = strconv.Itoa(int(math.Round(math.Mod(inches, 12))))
	}
	if p.WeightKg > 0 {
		f.WeightKg = strconv.FormatFloat(p.WeightKg, 'f', -1, 64)
		f.WeightLb = strconv.Itoa(int(math.Round(p.WeightKg / 0.453592)))
	}
	if p.Age > 0 {
		f.Age = strconv.Itoa(p.Age)
	}
	return f
}

// daySeed changes once per day (and per user) so the suggestion rotates.
func daySeed(userID int64, shuffle int) int64 {
	y, m, d := time.Now().Date()
	return int64(y*10000+int(m)*100+d) + userID*7919 + int64(shuffle)
}

// planFor builds today's plan for the current viewer, or nil when the
// goal isn't set yet.
func (h *Handler) planFor(r *http.Request, shuffle int) (*PlanView, error) {
	prof := web.Profile(r.Context())
	if prof == nil || !prof.HasGoal() {
		return nil, nil
	}
	targets, err := nutrition.Compute(inputFromProfile(prof))
	if err != nil {
		return nil, nil // stored values became invalid; the form will show errors
	}
	items, err := h.App.Pool.Visible(r.Context(), h.viewer(r))
	if err != nil {
		return nil, err
	}
	cands := make([]nutrition.Candidate, len(items))
	bySlug := make(map[string]pool.Item, len(items))
	for i, it := range items {
		cands[i] = nutrition.Candidate{ID: it.Base.ID, Slug: it.Base.Slug, MealTimes: it.Base.MealTimes, PerServe: it.Base.Nutrition}
		bySlug[it.Base.Slug] = it
	}
	plan := nutrition.Suggest(cands, targets, daySeed(prof.ID, shuffle))
	pv := &PlanView{Plan: plan, Targets: targets, Shuffle: shuffle}
	for _, pi := range plan.Items {
		pv.Items = append(pv.Items, PlanItemView{PlanItem: pi, Recipe: bySlug[pi.Slug].Recipe})
	}
	return pv, nil
}

func (h *Handler) goals(w http.ResponseWriter, r *http.Request) {
	prof := web.Profile(r.Context())
	shuffle, _ := strconv.Atoi(r.URL.Query().Get("shuffle"))
	if shuffle < 0 || shuffle > 1000 {
		shuffle = 0
	}
	plan, err := h.planFor(r, shuffle)
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	h.renderGoals(w, r, http.StatusOK, formFromProfile(prof), nil, plan)
}

func (h *Handler) renderGoals(w http.ResponseWriter, r *http.Request, status int, f goalForm, errs map[string]string, plan *PlanView) {
	p := web.NewPage(r, "goals.title")
	p.Errors = errs
	p.Data = map[string]any{
		"Form": f, "Plan": plan,
		"Imperial": h.units(r) == recipes.Imperial,
	}
	h.R.Page(w, status, "pages/goals", p)
}

func (h *Handler) saveGoals(w http.ResponseWriter, r *http.Request) {
	prof := h.requireUser(w, r)
	if prof == nil {
		return
	}
	f := goalForm{
		HeightCm: r.FormValue("height_cm"), WeightKg: r.FormValue("weight_kg"),
		HeightFt: r.FormValue("height_ft"), HeightIn: r.FormValue("height_in"), WeightLb: r.FormValue("weight_lb"),
		Age: r.FormValue("age"), Sex: r.FormValue("sex"), Activity: r.FormValue("activity"), Goal: r.FormValue("goal"),
	}
	errs := map[string]string{}
	num := func(field, s string) float64 {
		v, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(s), ",", "."), 64)
		if err != nil {
			errs[field] = "validation.number"
		}
		return v
	}
	in := nutrition.Input{Sex: f.Sex, Activity: f.Activity, Goal: f.Goal}
	if r.FormValue("system") == recipes.Imperial {
		ft, inch := num("height", f.HeightFt), 0.0
		if strings.TrimSpace(f.HeightIn) != "" {
			inch = num("height", f.HeightIn)
		}
		in.HeightCm = math.Round((ft*12+inch)*2.54*10) / 10
		in.WeightKg = math.Round(num("weight", f.WeightLb)*0.453592*10) / 10
	} else {
		in.HeightCm = num("height", f.HeightCm)
		in.WeightKg = num("weight", f.WeightKg)
	}
	in.Age = int(num("age", f.Age))
	if err := in.Validate(); err != nil {
		var ve nutrition.ValidationError
		if errors.As(err, &ve) {
			for k, v := range ve {
				if _, seen := errs[k]; !seen {
					errs[k] = v
				}
			}
		}
	}
	if len(errs) > 0 {
		h.renderGoals(w, r, http.StatusUnprocessableEntity, f, errs, nil)
		return
	}
	st := prof.Settings
	st.HeightCm, st.WeightKg, st.Age, st.Sex, st.Activity, st.Goal = in.HeightCm, in.WeightKg, in.Age, in.Sex, in.Activity, in.Goal
	if err := h.App.Users.SaveSettings(r.Context(), prof.ID, st); err != nil {
		h.Fail(w, r, err)
		return
	}
	web.Redirect(w, r, "/goals#plan", "goal_saved")
}
