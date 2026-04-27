package handlers

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestBuildOpenAIResponsesStreamErrorChunk(t *testing.T) {
	chunk := BuildOpenAIResponsesStreamErrorChunk(http.StatusInternalServerError, "unexpected EOF", 0)
	var payload map[string]any
	if err := json.Unmarshal(chunk, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["type"] != "error" {
		t.Fatalf("type = %v, want %q", payload["type"], "error")
	}
	if payload["code"] != "internal_server_error" {
		t.Fatalf("code = %v, want %q", payload["code"], "internal_server_error")
	}
	if payload["message"] != "unexpected EOF" {
		t.Fatalf("message = %v, want %q", payload["message"], "unexpected EOF")
	}
	if payload["sequence_number"] != float64(0) {
		t.Fatalf("sequence_number = %v, want %v", payload["sequence_number"], 0)
	}
}

func TestBuildOpenAIResponsesStreamErrorChunkExtractsHTTPErrorBody(t *testing.T) {
	chunk := BuildOpenAIResponsesStreamErrorChunk(
		http.StatusInternalServerError,
		`{"error":{"message":"oops","type":"server_error","code":"internal_server_error"}}`,
		0,
	)
	var payload map[string]any
	if err := json.Unmarshal(chunk, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["type"] != "error" {
		t.Fatalf("type = %v, want %q", payload["type"], "error")
	}
	if payload["code"] != "internal_server_error" {
		t.Fatalf("code = %v, want %q", payload["code"], "internal_server_error")
	}
	if payload["message"] != "oops" {
		t.Fatalf("message = %v, want %q", payload["message"], "oops")
	}
}

func TestBuildOpenAIResponsesStreamErrorChunkUnauthorizedWithoutCredentialSignalUsesGenericAuthCode(t *testing.T) {
	chunk := BuildOpenAIResponsesStreamErrorChunk(http.StatusUnauthorized, "signature expired", 0)

	var payload map[string]any
	if err := json.Unmarshal(chunk, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["code"] != "authentication_error" {
		t.Fatalf("code = %v, want %q", payload["code"], "authentication_error")
	}
	if payload["message"] != "signature expired" {
		t.Fatalf("message = %v, want %q", payload["message"], "signature expired")
	}
}

func TestBuildOpenAIResponsesStreamErrorChunkUnauthorizedCredentialSignalKeepsInvalidAPIKeyCode(t *testing.T) {
	chunk := BuildOpenAIResponsesStreamErrorChunk(http.StatusUnauthorized, "missing api key", 0)

	var payload map[string]any
	if err := json.Unmarshal(chunk, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["code"] != "invalid_api_key" {
		t.Fatalf("code = %v, want %q", payload["code"], "invalid_api_key")
	}
}

func TestBuildOpenAIResponsesStreamErrorChunkExtractsTopLevelJSONFields(t *testing.T) {
	chunk := BuildOpenAIResponsesStreamErrorChunk(
		http.StatusUnauthorized,
		`{"message":"signature expired","code":"signature_expired"}`,
		0,
	)

	var payload map[string]any
	if err := json.Unmarshal(chunk, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["code"] != "signature_expired" {
		t.Fatalf("code = %v, want %q", payload["code"], "signature_expired")
	}
	if payload["message"] != "signature expired" {
		t.Fatalf("message = %v, want %q", payload["message"], "signature expired")
	}
}
