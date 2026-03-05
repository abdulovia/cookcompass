package server

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

var planItemRe = regexp.MustCompile(`class="plan-item"`)

func TestGoalToSuggestionFlow(t *testing.T) {
	h := newHarness(t)

	// Before a goal is set: home invites the user, goals page asks for the form.
	if _, body := h.get("/"); !strings.Contains(body, "Get a daily plan") {
		t.Fatal("home goal CTA missing")
	}
	if _, body := h.get("/goals"); !strings.Contains(body, "Fill in the form") {
		t.Fatal("goals page without goal")
	}

	// Invalid input re-renders with errors.
	bad := url.Values{"system": {"metric"}, "height_cm": {"abc"}, "weight_kg": {"5"}, "age": {"30"}, "sex": {"x"}, "activity": {"moderate"}, "goal": {"maintain"}}
	resp, body := h.post("/goals", bad)
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "Please enter a number") ||
		!strings.Contains(body, "out of range") || !strings.Contains(body, "Please pick one") {
		t.Fatalf("validation: %d", resp.StatusCode)
	}

	// Valid goal → redirect → targets and a 3–5 item plan.
	form := url.Values{"system": {"metric"}, "height_cm": {"180"}, "weight_kg": {"80"}, "age": {"30"}, "sex": {"male"},
		"activity": {"moderate"}, "goal": {"maintain"}}
	resp, _ = h.post("/goals", form)
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(resp.Header.Get("Location"), "goal_saved") {
		t.Fatalf("save goal: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp, body = h.get("/goals")
	if resp.StatusCode != http.StatusOK {
		t.Fatal(resp.StatusCode)
	}
	for _, want := range []string{"2760", "1780", "All targets within ±10%", "Breakfast", "Dinner"} {
		if !strings.Contains(body, want) {
			t.Errorf("goals page missing %q", want)
		}
	}
	if n := len(planItemRe.FindAllString(body, -1)); n < 3 || n > 5 {
		t.Fatalf("plan has %d items", n)
	}
	if _, body := h.get("/goals?shuffle=1"); !strings.Contains(body, "shuffle=2") {
		t.Fatal("shuffle link")
	}
	if resp, _ := h.get("/goals?shuffle=-5"); resp.StatusCode != http.StatusOK {
		t.Fatal("negative shuffle")
	}
	if _, body := h.get("/"); !strings.Contains(body, "Today&#39;s plan") || len(planItemRe.FindAllString(body, -1)) < 3 {
		t.Fatal("home plan missing")
	}

	// Imperial input converts to metric storage.
	prof, _ := h.app.Users.Profile(context.Background(), h.app.DefaultUserID)
	prof.Units = "imperial"
	if err := h.app.Users.SaveSettings(context.Background(), prof.ID, prof.Settings); err != nil {
		t.Fatal(err)
	}
	if _, body := h.get("/goals"); !strings.Contains(body, `name="height_ft" inputmode="numeric" value="5"`) ||
		!strings.Contains(body, `value="176"`) {
		t.Fatal("imperial form not prefilled from metric values")
	}
	imp := url.Values{"system": {"imperial"}, "height_ft": {"5"}, "height_in": {"6"}, "weight_lb": {"150"}, "age": {"40"},
		"sex": {"female"}, "activity": {"light"}, "goal": {"lose_weight"}}
	if resp, _ := h.post("/goals", imp); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("imperial save: %d", resp.StatusCode)
	}
	prof, _ = h.app.Users.Profile(context.Background(), h.app.DefaultUserID)
	if prof.HeightCm != 167.6 || prof.WeightKg != 68 || prof.Goal != "lose_weight" {
		t.Fatalf("imperial conversion: %+v", prof.Settings)
	}
	if resp, _ := h.post("/goals", url.Values{"system": {"imperial"}, "height_ft": {"x"}}); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatal("bad imperial input accepted")
	}

	// A user allergic to everything in the plan pool gets a graceful empty plan.
	if _, err := h.app.DB.Exec(`UPDATE recipes SET status = 'rejected'`); err != nil {
		t.Fatal(err)
	}
	if _, body := h.get("/goals"); !strings.Contains(body, "Not enough recipes") {
		t.Fatal("empty plan message")
	}
}
