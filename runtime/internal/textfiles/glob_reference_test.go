package textfiles

import (
	"path"
	"strings"
)

// Pre-optimization matcher, kept as a behavior oracle for recursive glob semantics.
func legacyGlobMatch(pattern, name string) bool {
	a, b := strings.Split(pattern, "/"), strings.Split(name, "/")
	memo := map[[2]int]bool{}
	seen := map[[2]int]bool{}
	var match func(int, int) bool
	match = func(i, j int) (result bool) {
		key := [2]int{i, j}
		if seen[key] {
			return memo[key]
		}
		defer func() { seen[key] = true; memo[key] = result }()
		if i == len(a) {
			return j == len(b)
		}
		if a[i] == "**" {
			if match(i+1, j) {
				return true
			}
			return j < len(b) && match(i, j+1)
		}
		if j == len(b) {
			return false
		}
		ok, _ := path.Match(a[i], b[j])
		return ok && match(i+1, j+1)
	}
	return match(0, 0)
}
