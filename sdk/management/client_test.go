package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientGetAuthFiles(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v0/management/auth-files" {
			t.Fatalf("path = %q, want /v0/management/auth-files", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer mgmt-secret" {
			t.Fatalf("authorization = %q, want Bearer mgmt-secret", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files": []map[string]any{
				{
					"id":             "auth-1",
					"auth_index":     "idx-1",
					"name":           "claude-alice.json",
					"provider":       "claude",
					"status":         "ok",
					"status_message": "healthy",
					"email":          "alice@example.com",
					"account_type":   "pro",
					"account":        "alice",
					"last_refresh":   "2026-03-20T12:00:00Z",
					"id_token": map[string]any{
						"plan_type": "plus",
					},
				},
			},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "mgmt-secret")
	got, err := client.GetAuthFiles(context.Background())
	if err != nil {
		t.Fatalf("GetAuthFiles: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(authFiles) = %d, want 1", len(got))
	}
	if got[0].AuthIndex != "idx-1" {
		t.Fatalf("auth_index = %q, want idx-1", got[0].AuthIndex)
	}
	if got[0].Provider != "claude" {
		t.Fatalf("provider = %q, want claude", got[0].Provider)
	}
	if got[0].StatusMessage != "healthy" {
		t.Fatalf("status_message = %q, want healthy", got[0].StatusMessage)
	}
	if got[0].IDToken["plan_type"] != "plus" {
		t.Fatalf("plan_type = %v, want plus", got[0].IDToken["plan_type"])
	}
}

func TestClientGetAuthStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v0/management/get-auth-status" {
			t.Fatalf("path = %q, want /v0/management/get-auth-status", got)
		}
		if got := r.URL.Query().Get("state"); got != "oauth-state" {
			t.Fatalf("state = %q, want oauth-state", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "auth_url",
			"url":    "https://example.test/oauth",
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "")
	got, err := client.GetAuthStatus(context.Background(), "oauth-state")
	if err != nil {
		t.Fatalf("GetAuthStatus: %v", err)
	}
	if got.Status != "auth_url" {
		t.Fatalf("status = %q, want auth_url", got.Status)
	}
	if got.URL != "https://example.test/oauth" {
		t.Fatalf("url = %q, want https://example.test/oauth", got.URL)
	}
}

func TestClientStartAuthFlow(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v0/management/codex-auth-url" {
			t.Fatalf("path = %q, want /v0/management/codex-auth-url", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"url":    "https://auth.openai.com/oauth/authorize?...",
			"state":  "oauth-state",
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "")
	got, err := client.StartAuthFlow(context.Background(), "openai")
	if err != nil {
		t.Fatalf("StartAuthFlow: %v", err)
	}
	if got.Status != "ok" {
		t.Fatalf("status = %q, want ok", got.Status)
	}
	if got.State != "oauth-state" {
		t.Fatalf("state = %q, want oauth-state", got.State)
	}
}

func TestClientDownloadAuthFile(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v0/management/auth-files/download" {
			t.Fatalf("path = %q, want /v0/management/auth-files/download", got)
		}
		if got := r.URL.Query().Get("name"); got != "codex-alice.json" {
			t.Fatalf("name = %q, want codex-alice.json", got)
		}
		_, _ = w.Write([]byte(`{"access_token":"tok","refresh_token":"ref"}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "")
	got, err := client.DownloadAuthFile(context.Background(), "codex-alice.json")
	if err != nil {
		t.Fatalf("DownloadAuthFile: %v", err)
	}
	if string(got) != `{"access_token":"tok","refresh_token":"ref"}` {
		t.Fatalf("downloaded body = %q", string(got))
	}
}

func TestClientGetUsage(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v0/management/usage" {
			t.Fatalf("path = %q, want /v0/management/usage", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"enabled": true,
			"usage": map[string]any{
				"total_requests": 42,
			},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "")
	got, err := client.GetUsage(context.Background())
	if err != nil {
		t.Fatalf("GetUsage: %v", err)
	}
	if enabled, _ := got["enabled"].(bool); !enabled {
		t.Fatalf("enabled = %v, want true", got["enabled"])
	}
	usage, _ := got["usage"].(map[string]any)
	if total, _ := usage["total_requests"].(float64); total != 42 {
		t.Fatalf("total_requests = %v, want 42", usage["total_requests"])
	}
}

func TestClientParsesTimes(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files": []map[string]any{
				{
					"id":           "auth-1",
					"provider":     "codex",
					"last_refresh": ts.Format(time.RFC3339),
				},
			},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "")
	got, err := client.GetAuthFiles(context.Background())
	if err != nil {
		t.Fatalf("GetAuthFiles: %v", err)
	}
	if got[0].LastRefresh == nil || !got[0].LastRefresh.Equal(ts) {
		t.Fatalf("last_refresh = %v, want %v", got[0].LastRefresh, ts)
	}
}

func TestClientGetAuthFilesParsesRateLimitWindows(t *testing.T) {
	t.Parallel()

	resetAt := time.Date(2026, 3, 20, 15, 0, 0, 0, time.UTC)
	rateLimitedUntil := resetAt.Add(15 * time.Minute)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"files": []map[string]any{
				{
					"id":                 "auth-1",
					"auth_index":         "idx-1",
					"provider":           "codex",
					"rate_limited_until": rateLimitedUntil.Format(time.RFC3339),
					"rate_limit_windows": []map[string]any{
						{
							"name":                "primary",
							"used_percent":        82,
							"window_duration_min": 300,
							"resets_at":           resetAt.Format(time.RFC3339),
						},
						{
							"name":                "secondary",
							"used_percent":        40,
							"window_duration_min": 10080,
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "")
	got, err := client.GetAuthFiles(context.Background())
	if err != nil {
		t.Fatalf("GetAuthFiles: %v", err)
	}
	if got[0].RateLimitedUntil == nil || !got[0].RateLimitedUntil.Equal(rateLimitedUntil) {
		t.Fatalf("rate_limited_until = %v, want %v", got[0].RateLimitedUntil, rateLimitedUntil)
	}
	if len(got[0].RateLimitWindows) != 2 {
		t.Fatalf("len(rate_limit_windows) = %d, want 2", len(got[0].RateLimitWindows))
	}
	if got[0].RateLimitWindows[0].Name != "primary" {
		t.Fatalf("first window name = %q, want primary", got[0].RateLimitWindows[0].Name)
	}
	if got[0].RateLimitWindows[0].UsedPercent != 82 {
		t.Fatalf("primary used_percent = %d, want 82", got[0].RateLimitWindows[0].UsedPercent)
	}
	if got[0].RateLimitWindows[0].ResetsAt == nil || !got[0].RateLimitWindows[0].ResetsAt.Equal(resetAt) {
		t.Fatalf("primary resets_at = %v, want %v", got[0].RateLimitWindows[0].ResetsAt, resetAt)
	}
	if got[0].RateLimitWindows[1].WindowDurationMin != 10080 {
		t.Fatalf("secondary window_duration_min = %d, want 10080", got[0].RateLimitWindows[1].WindowDurationMin)
	}
}
