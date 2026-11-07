// Package learning models the beginner progression: learn 5 easy recipes,
// then 5 medium, then 5 hard. A level unlocks once the previous one is done;
// recipes learned "early" still count toward their level.
package learning

import "github.com/abdulovia/cookcompass/internal/recipes"

// PerLevel is how many recipes complete a level.
const PerLevel = 5

// Level is one step of the path.
type Level struct {
	Difficulty string
	Learned    int // capped at PerLevel
	Goal       int
	Unlocked   bool
	Done       bool
	Current    bool
}

// Remaining is how many more recipes finish the level.
func (l Level) Remaining() int { return l.Goal - l.Learned }

// Path builds the three levels from learned-recipe counts per difficulty.
func Path(learned map[string]int) []Level {
	levels := make([]Level, len(recipes.Difficulties))
	unlocked, currentSet := true, false
	for i, d := range recipes.Difficulties {
		n := min(learned[d], PerLevel)
		l := Level{Difficulty: d, Learned: n, Goal: PerLevel, Unlocked: unlocked, Done: n >= PerLevel}
		if unlocked && !l.Done && !currentSet {
			l.Current, currentSet = true, true
		}
		levels[i] = l
		unlocked = unlocked && l.Done
	}
	return levels
}

// Current returns the level being worked on, or nil when everything is done.
func Current(levels []Level) *Level {
	for i := range levels {
		if levels[i].Current {
			return &levels[i]
		}
	}
	return nil
}

// Total is the number of path recipes learned (capped per level).
func Total(levels []Level) (done, goal int) {
	for _, l := range levels {
		done += l.Learned
		goal += l.Goal
	}
	return done, goal
}
