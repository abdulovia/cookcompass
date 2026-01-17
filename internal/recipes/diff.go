package recipes

import "strings"

// DiffOp is the kind of a diff line.
type DiffOp int

// Diff operations.
const (
	DiffSame DiffOp = iota
	DiffAdd
	DiffDel
)

// DiffLine is one line of a line-based diff.
type DiffLine struct {
	Op   DiffOp
	Text string
}

// Class returns a CSS-friendly name for the operation.
func (d DiffLine) Class() string {
	switch d.Op {
	case DiffAdd:
		return "add"
	case DiffDel:
		return "del"
	}
	return "same"
}

// Prefix returns the unified-diff marker for the line.
func (d DiffLine) Prefix() string {
	switch d.Op {
	case DiffAdd:
		return "+"
	case DiffDel:
		return "-"
	}
	return " "
}

// Diff computes a minimal line diff between a and b using an LCS table.
// Snapshots are small (tens to hundreds of lines), so O(n·m) is fine.
func Diff(a, b string) []DiffLine {
	x := splitLines(a)
	y := splitLines(b)
	n, m := len(x), len(y)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	out := make([]DiffLine, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case x[i] == y[j]:
			out = append(out, DiffLine{DiffSame, x[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, DiffLine{DiffDel, x[i]})
			i++
		default:
			out = append(out, DiffLine{DiffAdd, y[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, DiffLine{DiffDel, x[i]})
	}
	for ; j < m; j++ {
		out = append(out, DiffLine{DiffAdd, y[j]})
	}
	return out
}

// DiffChanged reports whether any line differs.
func DiffChanged(lines []DiffLine) bool {
	for _, l := range lines {
		if l.Op != DiffSame {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}
