package publish

import (
	"fmt"
	"strconv"
	"strings"
)

// NormSemver normalises a version string to exactly 3 components.
func NormSemver(v string) string {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	return fmt.Sprintf("%d.%d.%d", semPart(parts, 0), semPart(parts, 1), semPart(parts, 2))
}

func semPart(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	// strip any pre-release suffix (e.g. "3-dev" -> 3)
	s := strings.SplitN(parts[i], "-", 2)[0]
	n, _ := strconv.Atoi(s)
	return n
}

// BumpVersion increments cur by level and returns the new version string.
func BumpVersion(cur string, level BumpLevel) string {
	n := NormSemver(cur)
	parts := strings.SplitN(n, ".", 3)
	a, b, c := semPart(parts, 0), semPart(parts, 1), semPart(parts, 2)
	switch level {
	case BumpMajor:
		return fmt.Sprintf("%d.0.0", a+1)
	case BumpMinor:
		return fmt.Sprintf("%d.%d.0", a, b+1)
	case BumpPatch:
		return fmt.Sprintf("%d.%d.%d", a, b, c+1)
	default:
		return n
	}
}

// SemverLess returns true if a < b (for sorting).
func SemverLess(a, b string) bool {
	na, nb := NormSemver(a), NormSemver(b)
	ap := strings.SplitN(na, ".", 3)
	bp := strings.SplitN(nb, ".", 3)
	for i := 0; i < 3; i++ {
		ai, bi := semPart(ap, i), semPart(bp, i)
		if ai != bi {
			return ai < bi
		}
	}
	return false
}
