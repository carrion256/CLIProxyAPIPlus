package openai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/sdk/api/handlers"
	"github.com/tidwall/gjson"
)

const (
	responsesAuthAffinityTTL        = 6 * time.Hour
	responsesAuthAffinityMaxEntries = 8192
)

type responsesAuthAffinityEntry struct {
	authID    string
	expiresAt time.Time
}

type responsesAuthAffinityStore struct {
	mu      sync.Mutex
	entries map[string]responsesAuthAffinityEntry
}

func newResponsesAuthAffinityStore() *responsesAuthAffinityStore {
	return &responsesAuthAffinityStore{
		entries: make(map[string]responsesAuthAffinityEntry),
	}
}

func (s *responsesAuthAffinityStore) Lookup(keys []string) string {
	if s == nil || len(keys) == 0 {
		return ""
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredLocked(now)
	for _, key := range keys {
		if key = strings.TrimSpace(key); key == "" {
			continue
		}
		entry, ok := s.entries[key]
		if !ok {
			continue
		}
		if entry.authID == "" || (!entry.expiresAt.IsZero() && now.After(entry.expiresAt)) {
			delete(s.entries, key)
			continue
		}
		return entry.authID
	}
	return ""
}

func (s *responsesAuthAffinityStore) Bind(authID string, keys ...string) {
	authID = strings.TrimSpace(authID)
	if s == nil || authID == "" || len(keys) == 0 {
		return
	}
	now := time.Now()
	expiresAt := now.Add(responsesAuthAffinityTTL)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneExpiredLocked(now)
	for _, key := range keys {
		if key = strings.TrimSpace(key); key == "" {
			continue
		}
		s.entries[key] = responsesAuthAffinityEntry{
			authID:    authID,
			expiresAt: expiresAt,
		}
	}
	s.pruneOverflowLocked()
}

func (s *responsesAuthAffinityStore) pruneExpiredLocked(now time.Time) {
	for key, entry := range s.entries {
		if entry.authID == "" || (!entry.expiresAt.IsZero() && now.After(entry.expiresAt)) {
			delete(s.entries, key)
		}
	}
}

func (s *responsesAuthAffinityStore) pruneOverflowLocked() {
	if len(s.entries) <= responsesAuthAffinityMaxEntries {
		return
	}
	for len(s.entries) > responsesAuthAffinityMaxEntries {
		oldestKey := ""
		var oldestExpiry time.Time
		for key, entry := range s.entries {
			if oldestKey == "" || entry.expiresAt.Before(oldestExpiry) {
				oldestKey = key
				oldestExpiry = entry.expiresAt
			}
		}
		if oldestKey == "" {
			return
		}
		delete(s.entries, oldestKey)
	}
}

type responsesContinuationBinding struct {
	store       *responsesAuthAffinityStore
	requestKeys []string

	mu     sync.RWMutex
	authID string
}

func (h *OpenAIResponsesAPIHandler) newResponsesContinuationBinding(rawJSON []byte) *responsesContinuationBinding {
	requestKeys := collectResponsesRequestAffinityKeys(rawJSON)
	binding := &responsesContinuationBinding{
		store:       h.continuity,
		requestKeys: requestKeys,
	}
	if authID := h.continuity.Lookup(requestKeys); authID != "" {
		binding.authID = authID
	}
	return binding
}

func (b *responsesContinuationBinding) PrepareContext(ctx context.Context) context.Context {
	if b == nil {
		return ctx
	}
	if authID := b.currentAuthID(); authID != "" {
		ctx = handlers.WithPinnedAuthID(ctx, authID)
	}
	return handlers.WithSelectedAuthIDCallback(ctx, b.bindSelectedAuth)
}

func (b *responsesContinuationBinding) WrapStream(in <-chan []byte) <-chan []byte {
	if b == nil || in == nil {
		return in
	}
	out := make(chan []byte)
	go func() {
		defer close(out)
		for chunk := range in {
			b.BindStreamChunk(chunk)
			out <- chunk
		}
	}()
	return out
}

func (b *responsesContinuationBinding) BindResponsePayload(payload []byte) {
	if b == nil {
		return
	}
	bindKeys := collectResponsesResponseAffinityKeys(payload)
	if len(bindKeys) == 0 {
		return
	}
	b.store.Bind(b.currentAuthID(), bindKeys...)
}

func (b *responsesContinuationBinding) BindStreamChunk(chunk []byte) {
	if b == nil || len(chunk) == 0 {
		return
	}
	keys := collectResponsesSSEAffinityKeys(chunk)
	if len(keys) == 0 {
		return
	}
	b.store.Bind(b.currentAuthID(), keys...)
}

func (b *responsesContinuationBinding) bindSelectedAuth(authID string) {
	authID = strings.TrimSpace(authID)
	if authID == "" || b == nil {
		return
	}
	b.mu.Lock()
	b.authID = authID
	b.mu.Unlock()
	if len(b.requestKeys) > 0 {
		b.store.Bind(authID, b.requestKeys...)
	}
}

func (b *responsesContinuationBinding) currentAuthID() string {
	if b == nil {
		return ""
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return strings.TrimSpace(b.authID)
}

func collectResponsesRequestAffinityKeys(rawJSON []byte) []string {
	if len(rawJSON) == 0 {
		return nil
	}
	root := gjson.ParseBytes(rawJSON)
	keys := make([]string, 0, 4)
	if previousResponseID := strings.TrimSpace(root.Get("previous_response_id").String()); previousResponseID != "" {
		keys = append(keys, "response_id:"+previousResponseID)
	}
	if promptCacheKey := strings.TrimSpace(root.Get("prompt_cache_key").String()); promptCacheKey != "" {
		keys = append(keys, "prompt_cache_key:"+promptCacheKey)
	}
	input := root.Get("input")
	if input.IsArray() {
		for _, item := range input.Array() {
			if encryptedContent := strings.TrimSpace(item.Get("encrypted_content").String()); encryptedContent != "" {
				keys = append(keys, "encrypted_content:"+hashResponsesEncryptedContent(encryptedContent))
			}
			content := item.Get("content")
			if !content.IsArray() {
				continue
			}
			for _, part := range content.Array() {
				if encryptedContent := strings.TrimSpace(part.Get("encrypted_content").String()); encryptedContent != "" {
					keys = append(keys, "encrypted_content:"+hashResponsesEncryptedContent(encryptedContent))
				}
			}
		}
	}
	return dedupeResponsesAffinityKeys(keys)
}

func collectResponsesResponseAffinityKeys(payload []byte) []string {
	if len(payload) == 0 {
		return nil
	}
	root := gjson.ParseBytes(payload)
	keys := make([]string, 0, 4)
	if responseID := strings.TrimSpace(root.Get("id").String()); responseID != "" {
		keys = append(keys, "response_id:"+responseID)
	}
	if responseID := strings.TrimSpace(root.Get("response.id").String()); responseID != "" {
		keys = append(keys, "response_id:"+responseID)
	}
	if promptCacheKey := strings.TrimSpace(root.Get("prompt_cache_key").String()); promptCacheKey != "" {
		keys = append(keys, "prompt_cache_key:"+promptCacheKey)
	}
	if promptCacheKey := strings.TrimSpace(root.Get("response.prompt_cache_key").String()); promptCacheKey != "" {
		keys = append(keys, "prompt_cache_key:"+promptCacheKey)
	}
	keys = append(keys, collectResponsesOutputEncryptedContentKeys(root.Get("output"))...)
	keys = append(keys, collectResponsesOutputEncryptedContentKeys(root.Get("response.output"))...)
	return dedupeResponsesAffinityKeys(keys)
}

func collectResponsesSSEAffinityKeys(chunk []byte) []string {
	if len(chunk) == 0 {
		return nil
	}
	keys := make([]string, 0, 4)
	for _, line := range strings.Split(string(chunk), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" || !gjson.Valid(data) {
			continue
		}
		event := gjson.Parse(data)
		if responseID := strings.TrimSpace(event.Get("response.id").String()); responseID != "" {
			keys = append(keys, "response_id:"+responseID)
		}
		if promptCacheKey := strings.TrimSpace(event.Get("response.prompt_cache_key").String()); promptCacheKey != "" {
			keys = append(keys, "prompt_cache_key:"+promptCacheKey)
		}
		if encryptedContent := strings.TrimSpace(event.Get("item.encrypted_content").String()); encryptedContent != "" {
			keys = append(keys, "encrypted_content:"+hashResponsesEncryptedContent(encryptedContent))
		}
		keys = append(keys, collectResponsesOutputEncryptedContentKeys(event.Get("response.output"))...)
	}
	return dedupeResponsesAffinityKeys(keys)
}

func collectResponsesOutputEncryptedContentKeys(output gjson.Result) []string {
	if !output.Exists() || !output.IsArray() {
		return nil
	}
	keys := make([]string, 0, len(output.Array()))
	for _, item := range output.Array() {
		if encryptedContent := strings.TrimSpace(item.Get("encrypted_content").String()); encryptedContent != "" {
			keys = append(keys, "encrypted_content:"+hashResponsesEncryptedContent(encryptedContent))
		}
	}
	return keys
}

func dedupeResponsesAffinityKeys(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	out := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func hashResponsesEncryptedContent(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
