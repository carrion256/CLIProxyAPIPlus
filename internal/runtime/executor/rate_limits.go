package executor

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func updateAuthRateLimitWindowsFromHeaders(auth *cliproxyauth.Auth, header http.Header, now time.Time) {
	if auth == nil {
		return
	}
	windows := authRateLimitWindowsFromHeaders(header, now)
	if len(windows) == 0 {
		return
	}
	if auth.Metadata == nil {
		auth.Metadata = map[string]any{}
	}
	auth.Metadata["rate_limit_windows"] = windows

	var nextRetryAfter time.Time
	for _, window := range windows {
		usedPercent, _ := window["used_percent"].(int)
		if usedPercent < 100 {
			continue
		}
		resetAtRaw, _ := window["resets_at"].(string)
		resetAt, ok := parseRateLimitResetAt(resetAtRaw, now)
		if !ok {
			continue
		}
		if nextRetryAfter.IsZero() || resetAt.After(nextRetryAfter) {
			nextRetryAfter = resetAt
		}
	}
	if !nextRetryAfter.IsZero() {
		auth.NextRetryAfter = nextRetryAfter
	}
}

func authRateLimitWindowsFromHeaders(header http.Header, now time.Time) []map[string]any {
	primary := authRateLimitWindowFromHeaders(
		header,
		"primary",
		[]string{"X-RateLimit-Limit", "x-ratelimit-limit", "anthropic-ratelimit-requests-limit"},
		[]string{"X-RateLimit-Remaining", "x-ratelimit-remaining", "anthropic-ratelimit-requests-remaining"},
		[]string{"X-RateLimit-Reset", "x-ratelimit-reset", "anthropic-ratelimit-requests-reset"},
		now,
	)
	secondary := authRateLimitWindowFromHeaders(
		header,
		"secondary",
		[]string{"anthropic-ratelimit-tokens-limit", "x-ratelimit-limit-tokens", "X-RateLimit-Limit-Tokens"},
		[]string{"anthropic-ratelimit-tokens-remaining", "x-ratelimit-remaining-tokens", "X-RateLimit-Remaining-Tokens"},
		[]string{"anthropic-ratelimit-tokens-reset", "x-ratelimit-reset-tokens", "X-RateLimit-Reset-Tokens"},
		now,
	)

	windows := make([]map[string]any, 0, 2)
	if primary != nil {
		windows = append(windows, primary)
	}
	if secondary != nil {
		windows = append(windows, secondary)
	}
	if len(windows) > 0 {
		return windows
	}

	if retryAfter := strings.TrimSpace(header.Get("Retry-After")); retryAfter != "" {
		if seconds := authRetryAfterSeconds(retryAfter, now); seconds > 0 {
			resetAt := now.Add(time.Duration(seconds) * time.Second).UTC()
			return []map[string]any{{
				"name":                "primary",
				"used_percent":        100,
				"window_duration_min": authDurationMinutes(resetAt.Sub(now)),
				"resets_at":           resetAt.Format(time.RFC3339),
				"updated_at":          now.UTC().Format(time.RFC3339),
			}}
		}
	}

	return nil
}

func authRateLimitWindowFromHeaders(header http.Header, name string, limitKeys, remainingKeys, resetKeys []string, now time.Time) map[string]any {
	limit, hasLimit := authFirstPositiveFloatHeader(header, limitKeys)
	remaining, hasRemaining := authFirstNonNegativeFloatHeader(header, remainingKeys)
	if !hasLimit || !hasRemaining || limit <= 0 {
		return nil
	}

	usedPercent := int(((limit - remaining) / limit) * 100)
	if usedPercent < 0 {
		usedPercent = 0
	}
	if usedPercent > 100 {
		usedPercent = 100
	}

	window := map[string]any{
		"name":         name,
		"used_percent": usedPercent,
		"updated_at":   now.UTC().Format(time.RFC3339),
	}
	if resetAt, ok := authFirstResetAtHeader(header, resetKeys, now); ok {
		window["window_duration_min"] = authDurationMinutes(resetAt.Sub(now))
		window["resets_at"] = resetAt.Format(time.RFC3339)
	}
	return window
}

func authFirstPositiveFloatHeader(header http.Header, keys []string) (float64, bool) {
	for _, key := range keys {
		value := strings.TrimSpace(header.Get(key))
		if value == "" {
			continue
		}
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil || parsed <= 0 {
			continue
		}
		return parsed, true
	}
	return 0, false
}

func authFirstNonNegativeFloatHeader(header http.Header, keys []string) (float64, bool) {
	for _, key := range keys {
		value := strings.TrimSpace(header.Get(key))
		if value == "" {
			continue
		}
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil || parsed < 0 {
			continue
		}
		return parsed, true
	}
	return 0, false
}

func authFirstResetAtHeader(header http.Header, keys []string, now time.Time) (time.Time, bool) {
	for _, key := range keys {
		value := strings.TrimSpace(header.Get(key))
		if value == "" {
			continue
		}
		if resetAt, ok := parseRateLimitResetAt(value, now); ok {
			return resetAt, true
		}
	}
	return time.Time{}, false
}

func parseRateLimitResetAt(raw string, now time.Time) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}

	if epoch, err := strconv.ParseInt(raw, 10, 64); err == nil {
		current := now.UTC().Unix()
		if epoch > current-300 {
			return time.Unix(epoch, 0).UTC(), true
		}
		if epoch >= 60 && epoch <= 86400 {
			return now.UTC().Add(time.Duration(epoch) * time.Second), true
		}
	}
	if duration, err := time.ParseDuration(raw); err == nil {
		return now.UTC().Add(duration), true
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed.UTC(), true
	}
	if parsed, err := http.ParseTime(raw); err == nil {
		return parsed.UTC(), true
	}
	return time.Time{}, false
}

func authRetryAfterSeconds(raw string, now time.Time) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
		return seconds
	}
	if parsed, err := http.ParseTime(raw); err == nil {
		if delta := int(parsed.Sub(now).Seconds()); delta > 0 {
			return delta
		}
	}
	return 0
}

func authDurationMinutes(delta time.Duration) int {
	if delta <= 0 {
		return 0
	}
	minutes := int(delta.Minutes())
	if minutes == 0 {
		return 1
	}
	return minutes
}
