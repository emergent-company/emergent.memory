package market

import (
	"strconv"
	"strings"
)

type semverCore struct {
	major, minor, patch int
	prerelease          string
}

func parseSemverCore(raw string) (semverCore, bool) {
	v := strings.TrimSpace(raw)
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	pre := ""
	if i := strings.IndexByte(v, '-'); i >= 0 {
		pre = v[i+1:]
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return semverCore{}, false
	}
	var nums [3]int
	for i, p := range parts {
		if p == "" {
			return semverCore{}, false
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return semverCore{}, false
		}
		nums[i] = n
	}
	return semverCore{major: nums[0], minor: nums[1], patch: nums[2], prerelease: pre}, true
}

func parseConstraintVersion(raw string) (semverCore, bool) {
	v := strings.TrimSpace(strings.TrimPrefix(raw, "v"))
	parts := strings.Split(v, ".")
	if len(parts) > 3 {
		// Keep only the numeric core / prerelease.
		v = strings.Join(parts[:3], ".")
	}
	for len(parts) < 3 {
		v += ".0"
		parts = strings.Split(v, ".")
	}
	return parseSemverCore(v)
}

func compareSemver(a, b semverCore) int {
	if a.major != b.major {
		return sign(a.major - b.major)
	}
	if a.minor != b.minor {
		return sign(a.minor - b.minor)
	}
	if a.patch != b.patch {
		return sign(a.patch - b.patch)
	}
	switch {
	case a.prerelease == b.prerelease:
		return 0
	case a.prerelease == "":
		return 1
	case b.prerelease == "":
		return -1
	case a.prerelease > b.prerelease:
		return 1
	default:
		return -1
	}
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}

// Compatible reports whether version satisfies the listing's compat.memory
// range. An empty or "*" range is compatible. A version that is not a semver
// (for example a dev build) is treated as compatible: the range exists to stop
// a real release mismatch, not to make development builds uninstallable. An
// unparseable constraint fails closed (incompatible).
func Compatible(version, constraint string) bool {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" || constraint == "*" {
		return true
	}
	current, ok := parseSemverCore(version)
	if !ok {
		return true
	}
	tokens := strings.FieldsFunc(constraint, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t' || r == '\n'
	})
	if len(tokens) == 0 {
		return true
	}
	for _, token := range tokens {
		if !matchesConstraint(current, token) {
			return false
		}
	}
	return true
}

func matchesConstraint(current semverCore, token string) bool {
	op := "="
	rest := token
	for _, candidate := range []string{">=", "<=", "==", ">", "<", "^", "~", "="} {
		if strings.HasPrefix(token, candidate) {
			op = candidate
			rest = strings.TrimSpace(strings.TrimPrefix(token, candidate))
			break
		}
	}
	want, ok := parseConstraintVersion(rest)
	if !ok {
		return false
	}
	cmp := compareSemver(current, want)
	switch op {
	case ">":
		return cmp > 0
	case ">=":
		return cmp >= 0
	case "<":
		return cmp < 0
	case "<=":
		return cmp <= 0
	case "=", "==":
		return cmp == 0
	case "^":
		if cmp < 0 {
			return false
		}
		if want.major > 0 {
			return current.major == want.major
		}
		if want.minor > 0 {
			return current.major == 0 && current.minor == want.minor
		}
		return current.major == 0 && current.minor == 0 && current.patch == want.patch
	case "~":
		if cmp < 0 {
			return false
		}
		if current.major != want.major {
			return false
		}
		return current.minor == want.minor
	default:
		return false
	}
}
