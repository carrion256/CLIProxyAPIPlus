package executor

import (
	"net/http"
	"testing"
	"time"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestUpdateAuthRateLimitWindowsFromHeadersStoresMetadata(t *testing.T) {
	t.Parallel()

	auth := &cliproxyauth.Auth{Metadata: map[string]any{}}
	headers := make(http.Header)
	headers.Set("X-RateLimit-Limit", "100")
	headers.Set("X-RateLimit-Remaining", "18")
	headers.Set("X-RateLimit-Reset", "1800")
	headers.Set("X-RateLimit-Limit-Tokens", "1000")
	headers.Set("X-RateLimit-Remaining-Tokens", "250")
	headers.Set("X-RateLimit-Reset-Tokens", "7200")

	now := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	updateAuthRateLimitWindowsFromHeaders(auth, headers, now)

	raw, ok := auth.Metadata["rate_limit_windows"].([]map[string]any)
	if !ok {
		t.Fatalf("rate_limit_windows metadata = %#v, want []map[string]any", auth.Metadata["rate_limit_windows"])
	}
	if len(raw) != 2 {
		t.Fatalf("len(rate_limit_windows) = %d, want 2", len(raw))
	}
	if raw[0]["name"] != "primary" {
		t.Fatalf("primary window name = %v, want primary", raw[0]["name"])
	}
	if raw[0]["used_percent"] != 82 {
		t.Fatalf("primary used_percent = %v, want 82", raw[0]["used_percent"])
	}
	if !auth.NextRetryAfter.IsZero() {
		t.Fatalf("expected next_retry_after to remain unset for non-exhausted windows, got %v", auth.NextRetryAfter)
	}
}
