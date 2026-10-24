package updates

import (
	"regexp"
	"strings"
)

var semver = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)
var numeric = regexp.MustCompile(`^[0-9]+$`)
var stableVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func parseVersion(value string) []string {
	parts := semver.FindStringSubmatch(value)
	if parts == nil {
		return nil
	}
	for _, id := range strings.Split(parts[4], ".") {
		if numeric.MatchString(id) && len(id) > 1 && id[0] == '0' {
			return nil
		}
	}
	return parts
}
func numberCompare(a, b string) int {
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return strings.Compare(a, b)
}

// Compare implements SemVer precedence, including arbitrarily large numeric identifiers.
func Compare(a, b string) (int, bool) {
	x, y := parseVersion(a), parseVersion(b)
	if x == nil || y == nil {
		return 0, false
	}
	for i := 1; i <= 3; i++ {
		if n := numberCompare(x[i], y[i]); n != 0 {
			return n, true
		}
	}
	if x[4] == y[4] {
		return 0, true
	}
	if x[4] == "" {
		return 1, true
	}
	if y[4] == "" {
		return -1, true
	}
	xp, yp := strings.Split(x[4], "."), strings.Split(y[4], ".")
	for i := 0; i < len(xp) && i < len(yp); i++ {
		a, b := xp[i], yp[i]
		an, bn := numeric.MatchString(a), numeric.MatchString(b)
		n := strings.Compare(a, b)
		if an && bn {
			n = numberCompare(a, b)
		} else if an {
			n = -1
		} else if bn {
			n = 1
		}
		if n != 0 {
			return n, true
		}
	}
	if len(xp) < len(yp) {
		return -1, true
	}
	return 1, true
}
func Development(version string) bool {
	if parseVersion(version) == nil {
		return true
	}
	pre := parseVersion(version)[4]
	for _, id := range strings.Split(pre, ".") {
		if id == "local" || id == "dev" || id == "development" {
			return true
		}
	}
	return false
}
