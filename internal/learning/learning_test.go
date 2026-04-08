package learning

import "testing"

func TestPath(t *testing.T) {
	tests := []struct {
		name     string
		learned  map[string]int
		unlocked []bool
		done     []bool
		current  string
		total    int
	}{
		{"fresh start", nil, []bool{true, false, false}, []bool{false, false, false}, "easy", 0},
		{"some easy", map[string]int{"easy": 3}, []bool{true, false, false}, []bool{false, false, false}, "easy", 3},
		{"easy done", map[string]int{"easy": 7, "medium": 1}, []bool{true, true, false}, []bool{true, false, false}, "medium", 6},
		{"hard learned early still counts", map[string]int{"easy": 2, "hard": 4}, []bool{true, false, false}, []bool{false, false, false}, "easy", 6},
		{"medium done but easy not", map[string]int{"easy": 4, "medium": 5}, []bool{true, false, false}, []bool{false, true, false}, "easy", 9},
		{"all done", map[string]int{"easy": 5, "medium": 5, "hard": 9}, []bool{true, true, true}, []bool{true, true, true}, "", 15},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			levels := Path(tc.learned)
			for i, l := range levels {
				if l.Unlocked != tc.unlocked[i] || l.Done != tc.done[i] {
					t.Errorf("level %s: unlocked=%v done=%v", l.Difficulty, l.Unlocked, l.Done)
				}
			}
			cur := Current(levels)
			got := ""
			if cur != nil {
				got = cur.Difficulty
				if cur.Remaining() != PerLevel-cur.Learned {
					t.Error("remaining")
				}
			}
			if got != tc.current {
				t.Errorf("current %q want %q", got, tc.current)
			}
			if done, goal := Total(levels); done != tc.total || goal != 15 {
				t.Errorf("total %d/%d", done, goal)
			}
		})
	}
}
