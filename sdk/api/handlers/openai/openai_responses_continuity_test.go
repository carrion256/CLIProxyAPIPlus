package openai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/api/handlers"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

type responsesAffinityExecutor struct {
	mu      sync.Mutex
	authIDs []string
}

func (e *responsesAffinityExecutor) Identifier() string { return "codex" }

func (e *responsesAffinityExecutor) Execute(_ context.Context, auth *coreauth.Auth, _ coreexecutor.Request, _ coreexecutor.Options) (coreexecutor.Response, error) {
	authID := e.recordAuthID(auth)
	payload := fmt.Sprintf(`{"id":"resp-%s","output":[{"type":"reasoning","encrypted_content":"enc-%s"}]}`, authID, authID)
	return coreexecutor.Response{Payload: []byte(payload)}, nil
}

func (e *responsesAffinityExecutor) ExecuteStream(_ context.Context, auth *coreauth.Auth, _ coreexecutor.Request, _ coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	authID := e.recordAuthID(auth)
	ch := make(chan coreexecutor.StreamChunk, 2)
	ch <- coreexecutor.StreamChunk{
		Payload: []byte(fmt.Sprintf("event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"reasoning\",\"encrypted_content\":\"enc-%s\"}}\n\n", authID)),
	}
	ch <- coreexecutor.StreamChunk{
		Payload: []byte(fmt.Sprintf("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-%s\",\"output\":[]}}\n\n", authID)),
	}
	close(ch)
	return &coreexecutor.StreamResult{Chunks: ch}, nil
}

func (e *responsesAffinityExecutor) Refresh(_ context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	return auth, nil
}

func (e *responsesAffinityExecutor) CountTokens(context.Context, *coreauth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, &coreauth.Error{Code: "not_implemented", Message: "CountTokens not implemented"}
}

func (e *responsesAffinityExecutor) HttpRequest(context.Context, *coreauth.Auth, *http.Request) (*http.Response, error) {
	return nil, &coreauth.Error{Code: "not_implemented", Message: "HttpRequest not implemented", HTTPStatus: http.StatusNotImplemented}
}

func (e *responsesAffinityExecutor) recordAuthID(auth *coreauth.Auth) string {
	authID := ""
	if auth != nil {
		authID = auth.ID
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.authIDs = append(e.authIDs, authID)
	return authID
}

func (e *responsesAffinityExecutor) AuthIDs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.authIDs))
	copy(out, e.authIDs)
	return out
}

func newResponsesAffinityTestRouter(t *testing.T) (*responsesAffinityExecutor, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	executor := &responsesAffinityExecutor{}
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(executor)

	for _, authID := range []string{"auth1", "auth2"} {
		auth := &coreauth.Auth{
			ID:       authID,
			Provider: "codex",
			Status:   coreauth.StatusActive,
		}
		if _, err := manager.Register(context.Background(), auth); err != nil {
			t.Fatalf("manager.Register(%s): %v", authID, err)
		}
		registry.GetGlobalRegistry().RegisterClient(authID, auth.Provider, []*registry.ModelInfo{{ID: "test-model"}})
		t.Cleanup(func(id string) func() {
			return func() { registry.GetGlobalRegistry().UnregisterClient(id) }
		}(authID))
	}

	base := handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, manager)
	h := NewOpenAIResponsesAPIHandler(base)
	router := gin.New()
	router.POST("/v1/responses", h.Responses)
	return executor, router
}

func TestResponsesPinsAuthFromPreviousResponseID(t *testing.T) {
	executor, router := newResponsesAffinityTestRouter(t)

	firstReq := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test-model","input":"hello"}`))
	firstReq.Header.Set("Content-Type", "application/json")
	firstResp := httptest.NewRecorder()
	router.ServeHTTP(firstResp, firstReq)
	if firstResp.Code != http.StatusOK {
		t.Fatalf("first response status = %d, want %d, body=%s", firstResp.Code, http.StatusOK, firstResp.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test-model","previous_response_id":"resp-auth1","input":"continue"}`))
	secondReq.Header.Set("Content-Type", "application/json")
	secondResp := httptest.NewRecorder()
	router.ServeHTTP(secondResp, secondReq)
	if secondResp.Code != http.StatusOK {
		t.Fatalf("second response status = %d, want %d, body=%s", secondResp.Code, http.StatusOK, secondResp.Body.String())
	}

	if got := executor.AuthIDs(); len(got) != 2 || got[0] != "auth1" || got[1] != "auth1" {
		t.Fatalf("auth sequence = %v, want [auth1 auth1]", got)
	}
}

func TestResponsesPinsAuthFromEncryptedContent(t *testing.T) {
	executor, router := newResponsesAffinityTestRouter(t)

	firstReq := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test-model","input":"hello"}`))
	firstReq.Header.Set("Content-Type", "application/json")
	firstResp := httptest.NewRecorder()
	router.ServeHTTP(firstResp, firstReq)
	if firstResp.Code != http.StatusOK {
		t.Fatalf("first response status = %d, want %d, body=%s", firstResp.Code, http.StatusOK, firstResp.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test-model","input":[{"type":"reasoning","encrypted_content":"enc-auth1"}]}`))
	secondReq.Header.Set("Content-Type", "application/json")
	secondResp := httptest.NewRecorder()
	router.ServeHTTP(secondResp, secondReq)
	if secondResp.Code != http.StatusOK {
		t.Fatalf("second response status = %d, want %d, body=%s", secondResp.Code, http.StatusOK, secondResp.Body.String())
	}

	if got := executor.AuthIDs(); len(got) != 2 || got[0] != "auth1" || got[1] != "auth1" {
		t.Fatalf("auth sequence = %v, want [auth1 auth1]", got)
	}
}

func TestResponsesPinsAuthAfterStreamingTurn(t *testing.T) {
	executor, router := newResponsesAffinityTestRouter(t)

	firstReq := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test-model","stream":true,"input":"hello"}`))
	firstReq.Header.Set("Content-Type", "application/json")
	firstResp := httptest.NewRecorder()
	router.ServeHTTP(firstResp, firstReq)
	if firstResp.Code != http.StatusOK {
		t.Fatalf("first streaming response status = %d, want %d, body=%s", firstResp.Code, http.StatusOK, firstResp.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"test-model","previous_response_id":"resp-auth1","input":"continue"}`))
	secondReq.Header.Set("Content-Type", "application/json")
	secondResp := httptest.NewRecorder()
	router.ServeHTTP(secondResp, secondReq)
	if secondResp.Code != http.StatusOK {
		t.Fatalf("second response status = %d, want %d, body=%s", secondResp.Code, http.StatusOK, secondResp.Body.String())
	}

	if got := executor.AuthIDs(); len(got) != 2 || got[0] != "auth1" || got[1] != "auth1" {
		t.Fatalf("auth sequence = %v, want [auth1 auth1]", got)
	}
}
