package toolnames

import (
	"strconv"
	"strings"
)

const maxNameLen = 64

func Normalize(name string) string {
	return baseCandidate(name, 0)
}

func BuildShortNameMap(names []string) map[string]string {
	used := map[string]struct{}{}
	m := map[string]string{}

	for _, n := range names {
		candidate := baseCandidate(n, 0)
		if _, ok := used[candidate]; !ok {
			used[candidate] = struct{}{}
			m[n] = candidate
			continue
		}
		for i := 1; ; i++ {
			suffix := "_" + strconv.Itoa(i)
			candidate = baseCandidate(n, len(suffix)) + suffix
			if _, ok := used[candidate]; ok {
				continue
			}
			used[candidate] = struct{}{}
			m[n] = candidate
			break
		}
	}

	return m
}

func baseCandidate(name string, reservedSuffix int) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "tool"
	}

	if len(name) > maxNameLen-reservedSuffix {
		if strings.HasPrefix(name, "mcp__") {
			if idx := strings.LastIndex(name, "__"); idx > 0 {
				name = "mcp__" + name[idx+2:]
			}
		}
	}

	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}

	sanitized := strings.Trim(b.String(), "_")
	if sanitized == "" {
		sanitized = "tool"
	}

	limit := maxNameLen - reservedSuffix
	if limit < 1 {
		limit = 1
	}
	if len(sanitized) > limit {
		sanitized = sanitized[:limit]
	}
	return sanitized
}
