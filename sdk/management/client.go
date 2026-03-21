package management

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a lightweight HTTP client for CLIProxy's management API.
type Client struct {
	baseURL   string
	secretKey string
	http      *http.Client
}

// AuthFile is the typed auth-files entry returned by the management API.
type AuthFile struct {
	ID               string            `json:"id"`
	AuthIndex        string            `json:"auth_index"`
	Name             string            `json:"name"`
	Type             string            `json:"type"`
	Provider         string            `json:"provider"`
	Label            string            `json:"label"`
	Status           string            `json:"status"`
	StatusMessage    string            `json:"status_message"`
	Disabled         bool              `json:"disabled"`
	Unavailable      bool              `json:"unavailable"`
	RuntimeOnly      bool              `json:"runtime_only"`
	Source           string            `json:"source"`
	Email            string            `json:"email"`
	AccountType      string            `json:"account_type"`
	Account          string            `json:"account"`
	LastRefresh      *time.Time        `json:"last_refresh,omitempty"`
	NextRetryAfter   *time.Time        `json:"next_retry_after,omitempty"`
	RateLimitedUntil *time.Time        `json:"rate_limited_until,omitempty"`
	RateLimitWindows []RateLimitWindow `json:"rate_limit_windows,omitempty"`
	IDToken          map[string]any    `json:"id_token,omitempty"`
}

// RateLimitWindow describes one provider-reported quota/session window for an auth file.
type RateLimitWindow struct {
	Name              string     `json:"name"`
	UsedPercent       int        `json:"used_percent"`
	WindowDurationMin int        `json:"window_duration_min,omitempty"`
	ResetsAt          *time.Time `json:"resets_at,omitempty"`
}

// AuthStatusResponse describes the OAuth bootstrap polling response.
type AuthStatusResponse struct {
	Status          string `json:"status"`
	URL             string `json:"url,omitempty"`
	Error           string `json:"error,omitempty"`
	VerificationURL string `json:"verification_url,omitempty"`
	UserCode        string `json:"user_code,omitempty"`
}

// AuthStartResponse describes the initial OAuth bootstrap response from CLIProxy.
type AuthStartResponse struct {
	Status string `json:"status"`
	URL    string `json:"url,omitempty"`
	State  string `json:"state,omitempty"`
	Method string `json:"method,omitempty"`
	Error  string `json:"error,omitempty"`
}

type authFilesResponse struct {
	Files []authFileRaw `json:"files"`
}

type authFileRaw struct {
	ID               string               `json:"id"`
	AuthIndex        string               `json:"auth_index"`
	Name             string               `json:"name"`
	Type             string               `json:"type"`
	Provider         string               `json:"provider"`
	Label            string               `json:"label"`
	Status           string               `json:"status"`
	StatusMessage    string               `json:"status_message"`
	Disabled         bool                 `json:"disabled"`
	Unavailable      bool                 `json:"unavailable"`
	RuntimeOnly      bool                 `json:"runtime_only"`
	Source           string               `json:"source"`
	Email            string               `json:"email"`
	AccountType      string               `json:"account_type"`
	Account          string               `json:"account"`
	LastRefresh      any                  `json:"last_refresh"`
	NextRetryAfter   any                  `json:"next_retry_after"`
	RateLimitedUntil any                  `json:"rate_limited_until"`
	RateLimitWindows []rateLimitWindowRaw `json:"rate_limit_windows"`
	IDToken          map[string]any       `json:"id_token"`
}

type rateLimitWindowRaw struct {
	Name              string `json:"name"`
	UsedPercent       int    `json:"used_percent"`
	WindowDurationMin int    `json:"window_duration_min"`
	ResetsAt          any    `json:"resets_at"`
}

// NewClient constructs a client rooted at the CLIProxy server base URL.
func NewClient(baseURL, secretKey string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return &Client{
		baseURL:   baseURL,
		secretKey: strings.TrimSpace(secretKey),
		http:      &http.Client{Timeout: 10 * time.Second},
	}
}

// SetSecretKey updates the bearer token used for management requests.
func (c *Client) SetSecretKey(secretKey string) {
	if c == nil {
		return
	}
	c.secretKey = strings.TrimSpace(secretKey)
}

// GetUsage fetches the management usage snapshot.
func (c *Client) GetUsage(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	if err := c.getJSON(ctx, "/v0/management/usage", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAuthFiles fetches auth-file metadata from the management API.
func (c *Client) GetAuthFiles(ctx context.Context) ([]AuthFile, error) {
	var wrapper authFilesResponse
	if err := c.getJSON(ctx, "/v0/management/auth-files", &wrapper); err != nil {
		return nil, err
	}
	out := make([]AuthFile, 0, len(wrapper.Files))
	for _, raw := range wrapper.Files {
		lastRefresh, err := parseOptionalTime(raw.LastRefresh)
		if err != nil {
			return nil, fmt.Errorf("parse last_refresh for %s: %w", raw.Name, err)
		}
		nextRetryAfter, err := parseOptionalTime(raw.NextRetryAfter)
		if err != nil {
			return nil, fmt.Errorf("parse next_retry_after for %s: %w", raw.Name, err)
		}
		rateLimitedUntil, err := parseOptionalTime(raw.RateLimitedUntil)
		if err != nil {
			return nil, fmt.Errorf("parse rate_limited_until for %s: %w", raw.Name, err)
		}
		rateLimitWindows := make([]RateLimitWindow, 0, len(raw.RateLimitWindows))
		for _, window := range raw.RateLimitWindows {
			resetsAt, err := parseOptionalTime(window.ResetsAt)
			if err != nil {
				return nil, fmt.Errorf("parse rate_limit_window resets_at for %s: %w", raw.Name, err)
			}
			rateLimitWindows = append(rateLimitWindows, RateLimitWindow{
				Name:              strings.TrimSpace(window.Name),
				UsedPercent:       window.UsedPercent,
				WindowDurationMin: window.WindowDurationMin,
				ResetsAt:          resetsAt,
			})
		}
		out = append(out, AuthFile{
			ID:               raw.ID,
			AuthIndex:        raw.AuthIndex,
			Name:             raw.Name,
			Type:             raw.Type,
			Provider:         raw.Provider,
			Label:            raw.Label,
			Status:           raw.Status,
			StatusMessage:    raw.StatusMessage,
			Disabled:         raw.Disabled,
			Unavailable:      raw.Unavailable,
			RuntimeOnly:      raw.RuntimeOnly,
			Source:           raw.Source,
			Email:            raw.Email,
			AccountType:      raw.AccountType,
			Account:          raw.Account,
			LastRefresh:      lastRefresh,
			NextRetryAfter:   nextRetryAfter,
			RateLimitedUntil: rateLimitedUntil,
			RateLimitWindows: rateLimitWindows,
			IDToken:          raw.IDToken,
		})
	}
	return out, nil
}

// GetAuthStatus polls OAuth bootstrap status for a given state value.
func (c *Client) GetAuthStatus(ctx context.Context, state string) (AuthStatusResponse, error) {
	query := url.Values{}
	query.Set("state", strings.TrimSpace(state))
	var out AuthStatusResponse
	if err := c.getJSON(ctx, "/v0/management/get-auth-status?"+query.Encode(), &out); err != nil {
		return AuthStatusResponse{}, err
	}
	return out, nil
}

// StartAuthFlow requests a provider OAuth bootstrap URL from the management API.
func (c *Client) StartAuthFlow(ctx context.Context, provider string) (AuthStartResponse, error) {
	path, err := authStartPath(provider)
	if err != nil {
		return AuthStartResponse{}, err
	}
	var out AuthStartResponse
	if err := c.getJSON(ctx, path, &out); err != nil {
		return AuthStartResponse{}, err
	}
	return out, nil
}

// DownloadAuthFile fetches the raw JSON contents for a single auth file.
func (c *Client) DownloadAuthFile(ctx context.Context, name string) ([]byte, error) {
	query := url.Values{}
	query.Set("name", strings.TrimSpace(name))
	return c.doRequest(ctx, http.MethodGet, "/v0/management/auth-files/download?"+query.Encode(), nil)
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	data, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("management client is nil")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if c.secretKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.secretKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func parseOptionalTime(raw any) (*time.Time, error) {
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil, nil
		}
		parsed, err := time.Parse(time.RFC3339, s)
		if err != nil {
			parsed, err = time.Parse(time.RFC3339Nano, s)
			if err != nil {
				return nil, err
			}
		}
		return &parsed, nil
	default:
		return nil, nil
	}
}

func authStartPath(provider string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "anthropic", "claude":
		return "/v0/management/anthropic-auth-url", nil
	case "openai", "codex":
		return "/v0/management/codex-auth-url", nil
	case "google", "gemini":
		return "/v0/management/gemini-cli-auth-url", nil
	default:
		return "", fmt.Errorf("unsupported auth flow provider %q", strings.TrimSpace(provider))
	}
}
