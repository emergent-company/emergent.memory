package upgrade

import (
	"strconv"
	"strings"
)

// Normalize strips the connector release prefixes from a tag or version so the
// result is a bare semver string: "connector-v0.2.0" and "v0.2.0" both become
// "0.2.0". It is idempotent for already-bare versions.
func Normalize(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "connector-")
	v = strings.TrimPrefix(v, "v")
	return v
}

// parseVersion parses a (possibly prefixed) version into its numeric core and
// prerelease suffix. ok is false when the core is not one to three numeric
// dot-separated components, which is how "dev"/"unknown"/empty are detected.
func parseVersion(v string) (nums [3]int, pre string, ok bool) {
	norm := Normalize(v)
	if i := strings.IndexByte(norm, '+'); i >= 0 {
		norm = norm[:i] // drop build metadata
	}
	if i := strings.IndexByte(norm, '-'); i >= 0 {
		pre = norm[i+1:]
		norm = norm[:i]
	}
	if norm == "" {
		return nums, "", false
	}
	parts := strings.Split(norm, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return nums, "", false
	}
	for i, p := range parts {
		if p == "" {
			return nums, "", false
		}
		for _, ch := range p {
			if ch < '0' || ch > '9' {
				return nums, "", false
			}
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nums, "", false
		}
		nums[i] = n
	}
	return nums, pre, true
}

// Compare orders two versions returned as -1 (a<b), 0 (equal), or 1 (a>b).
// Prefixes are ignored. A version that cannot be parsed (for example "dev",
// "unknown", or an empty current version) is treated as older than any
// parseable version, so dev builds always see the newest release as an update.
func Compare(a, b string) int {
	an, ap, aok := parseVersion(a)
	bn, bp, bok := parseVersion(b)
	switch {
	case !aok && !bok:
		return 0
	case !aok:
		return -1
	case !bok:
		return 1
	}
	for i := range an {
		switch {
		case an[i] < bn[i]:
			return -1
		case an[i] > bn[i]:
			return 1
		}
	}
	switch {
	case ap == bp:
		return 0
	case ap == "": // release > prerelease
		return 1
	case bp == "":
		return -1
	case ap < bp:
		return -1
	default:
		return 1
	}
}
