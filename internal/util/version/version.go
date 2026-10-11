// Package version compares 3x-ui release versions ("v3.8.0", "3.8.0", or this fork's "3.9.0-k8").
package version

import (
	"strconv"
	"strings"
)

// Compare returns -1, 0 or 1 as a is older than, equal to or newer than b, and
// false when either side is not MAJOR.MINOR.PATCH with an optional fork "-kN" revision.
func Compare(a, b string) (int, bool) {
	aParts, okA := parse(a)
	bParts, okB := parse(b)
	if !okA || !okB {
		return 0, false
	}
	for i := range len(aParts) {
		if aParts[i] > bParts[i] {
			return 1, true
		}
		if aParts[i] < bParts[i] {
			return -1, true
		}
	}
	return 0, true
}

// Normalize strips surrounding space and a leading "v" from a version tag.
func Normalize(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// parse returns MAJOR, MINOR, PATCH and the fork revision; a fork build "3.9.0-k8" sits after
// upstream 3.9.0 and before 3.9.1, so a missing revision counts as 0.
func parse(v string) ([4]int, bool) {
	var result [4]int
	base, rev, forked := strings.Cut(Normalize(v), "-k")
	parts := strings.Split(base, ".")
	if len(parts) != 3 {
		return result, false
	}
	if forked {
		parts = append(parts, rev)
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || strings.HasPrefix(part, "+") {
			return result, false
		}
		result[i] = n
	}
	return result, true
}
