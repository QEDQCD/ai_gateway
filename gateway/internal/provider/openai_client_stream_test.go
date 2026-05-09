package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/ai_gateway/gateway/internal/domain"
	"github.com/example/ai_gateway/gateway/internal/service"
)

func TestOpenAIClientStreamCompleteForwardsSSEAndBuildsFinalResponse(t *testing.T) {
	t.Parallel()

	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("io.ReadAll failed: %v", err)
		}
		if !strings.Contains(string(body), `"stream":true`) {
			t.Fatalf("expected request body to include stream=true, got %s", string(body))
		}

		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected response writer to implement http.Flusher")
		}

		_, _ = io.WriteString(w, "data: {\"model\":\"qwen-flash\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"你\"}}]}\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, "data: {\"model\":\"qwen-flash\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"好\"}}]}\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, "data: {\"model\":\"qwen-flash\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12}}\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	t.Cleanup(providerServer.Close)

	client := NewOpenAIClient(http.DefaultClient)
	stream, statusCode, err := client.StreamComplete(
		context.Background(),
		domain.ProviderTarget{
			BaseURL: providerServer.URL,
			APIKey:  "provider-secret-key",
		},
		service.ChatRequest{
			Model:  "qwen-flash",
			Stream: true,
			Messages: []service.ChatMessage{
				{Role: "user", Content: "你好"},
			},
		},
	)
	if err != nil {
		t.Fatalf("StreamComplete returned unexpected error: %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", statusCode)
	}
	if got := stream.ContentType; !strings.Contains(got, "text/event-stream") {
		t.Fatalf("expected text/event-stream content type, got %q", got)
	}

	var chunks bytes.Buffer
	firstTokenCallbacks := 0
	result, err := stream.Run(func(chunk []byte) error {
		_, writeErr := chunks.Write(chunk)
		return writeErr
	}, func() {
		firstTokenCallbacks++
	})
	if err != nil {
		t.Fatalf("stream.Run returned unexpected error: %v", err)
	}
	resp := result.Response

	if got := chunks.String(); !strings.Contains(got, "data: [DONE]") {
		t.Fatalf("expected stream output to include [DONE], got %q", got)
	}
	if firstTokenCallbacks != 1 {
		t.Fatalf("expected first token callback to be invoked once, got %d", firstTokenCallbacks)
	}
	if !result.SawContentToken {
		t.Fatal("expected stream result to report content token")
	}
	if result.ClientAborted {
		t.Fatal("expected successful stream not to report client abort")
	}
	if got := len(resp.Choices); got != 1 {
		t.Fatalf("expected 1 choice, got %d", got)
	}
	if got := resp.Choices[0].Message.Role; got != "assistant" {
		t.Fatalf("expected assistant role, got %q", got)
	}
	if got := resp.Choices[0].Message.Content; got != "你好" {
		t.Fatalf("expected final content %q, got %q", "你好", got)
	}
	if resp.Usage == nil {
		t.Fatal("expected usage to be populated from final stream chunk")
	}
	if resp.Usage.TotalTokens != 12 {
		t.Fatalf("expected total tokens 12, got %d", resp.Usage.TotalTokens)
	}
}

func TestOpenAIClientStreamCompleteRedactsSensitiveContentInForwardedChunksAndFinalResponse(t *testing.T) {
	t.Parallel()

	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected response writer to implement http.Flusher")
		}

		_, _ = io.WriteString(w, "data: {\"model\":\"qwen-flash\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"手机号是13333333333\"}}]}\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, "data: {\"model\":\"qwen-flash\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":8,\"total_tokens\":18}}\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	t.Cleanup(providerServer.Close)

	client := NewOpenAIClient(http.DefaultClient)
	stream, _, err := client.StreamComplete(
		context.Background(),
		domain.ProviderTarget{
			BaseURL: providerServer.URL,
			APIKey:  "provider-secret-key",
		},
		service.ChatRequest{
			Model:  "qwen-flash",
			Stream: true,
			Messages: []service.ChatMessage{
				{Role: "user", Content: "你好"},
			},
		},
	)
	if err != nil {
		t.Fatalf("StreamComplete returned unexpected error: %v", err)
	}

	var chunks bytes.Buffer
	result, err := stream.Run(func(chunk []byte) error {
		_, writeErr := chunks.Write(chunk)
		return writeErr
	}, nil)
	if err != nil {
		t.Fatalf("stream.Run returned unexpected error: %v", err)
	}

	if strings.Contains(chunks.String(), "13333333333") {
		t.Fatalf("expected forwarded stream chunks to redact phone number, got %q", chunks.String())
	}
	if !strings.Contains(chunks.String(), "133XXXX3333") {
		t.Fatalf("expected forwarded stream chunks to contain redacted phone number, got %q", chunks.String())
	}
	if got := result.Response.Choices[0].Message.Content; got != "手机号是133XXXX3333" {
		t.Fatalf("expected final content %q, got %q", "手机号是133XXXX3333", got)
	}
}

func TestOpenAIClientStreamCompleteMarksClientAbortAfterFirstContentToken(t *testing.T) {
	t.Parallel()

	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected response writer to implement http.Flusher")
		}

		_, _ = io.WriteString(w, "data: {\"model\":\"qwen-flash\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, "data: {\"model\":\"qwen-flash\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"你\"}}]}\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	t.Cleanup(providerServer.Close)

	client := NewOpenAIClient(http.DefaultClient)
	stream, _, err := client.StreamComplete(
		context.Background(),
		domain.ProviderTarget{
			BaseURL: providerServer.URL,
			APIKey:  "provider-secret-key",
		},
		service.ChatRequest{
			Model:  "qwen-flash",
			Stream: true,
			Messages: []service.ChatMessage{
				{Role: "user", Content: "你好"},
			},
		},
	)
	if err != nil {
		t.Fatalf("StreamComplete returned unexpected error: %v", err)
	}

	emitErr := errors.New("client disconnected")
	firstTokenCallbacks := 0
	result, err := stream.Run(func(chunk []byte) error {
		if strings.Contains(string(chunk), `"content":"你"`) {
			return emitErr
		}
		return nil
	}, func() {
		firstTokenCallbacks++
	})
	if !errors.Is(err, emitErr) {
		t.Fatalf("expected emit error %v, got %v", emitErr, err)
	}

	if firstTokenCallbacks != 1 {
		t.Fatalf("expected first token callback to be invoked once, got %d", firstTokenCallbacks)
	}
	if !result.SawContentToken {
		t.Fatal("expected stream result to report content token before abort")
	}
	if !result.ClientAborted {
		t.Fatal("expected stream result to report client abort")
	}
	if got := len(result.Response.Choices); got != 1 {
		t.Fatalf("expected 1 accumulated choice, got %d", got)
	}
	if got := result.Response.Choices[0].Message.Content; got != "你" {
		t.Fatalf("expected partial response content %q, got %q", "你", got)
	}
}

func TestOpenAIClientStreamCompleteDoesNotTriggerFirstTokenForRoleOrUsageOnlyChunks(t *testing.T) {
	t.Parallel()

	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected response writer to implement http.Flusher")
		}

		_, _ = io.WriteString(w, "data: {\"model\":\"qwen-flash\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, "data: {\"model\":\"qwen-flash\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":0,\"total_tokens\":10}}\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	t.Cleanup(providerServer.Close)

	client := NewOpenAIClient(http.DefaultClient)
	stream, _, err := client.StreamComplete(
		context.Background(),
		domain.ProviderTarget{
			BaseURL: providerServer.URL,
			APIKey:  "provider-secret-key",
		},
		service.ChatRequest{
			Model:  "qwen-flash",
			Stream: true,
			Messages: []service.ChatMessage{
				{Role: "user", Content: "你好"},
			},
		},
	)
	if err != nil {
		t.Fatalf("StreamComplete returned unexpected error: %v", err)
	}

	firstTokenCallbacks := 0
	result, err := stream.Run(func([]byte) error {
		return nil
	}, func() {
		firstTokenCallbacks++
	})
	if err != nil {
		t.Fatalf("stream.Run returned unexpected error: %v", err)
	}
	if firstTokenCallbacks != 0 {
		t.Fatalf("expected no first token callback, got %d", firstTokenCallbacks)
	}
	if result.SawContentToken {
		t.Fatal("expected no content token signal for role/usage-only stream")
	}
	if result.ClientAborted {
		t.Fatal("expected completed stream not to report client abort")
	}
	if got := len(result.Response.Choices); got != 1 {
		t.Fatalf("expected 1 accumulated choice, got %d", got)
	}
	if got := result.Response.Choices[0].Message.Content; got != "" {
		t.Fatalf("expected empty content, got %q", got)
	}
}

func TestOpenAIClientStreamCompleteTreatsReasoningOnlyChunkAsFirstToken(t *testing.T) {
	t.Parallel()

	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected response writer to implement http.Flusher")
		}

		_, _ = io.WriteString(w, "data: {\"model\":\"mimo-v2.5-pro\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"reasoning_content\":\"思考中\"}]}\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	t.Cleanup(providerServer.Close)

	client := NewOpenAIClient(http.DefaultClient)
	stream, _, err := client.StreamComplete(
		context.Background(),
		domain.ProviderTarget{
			BaseURL: providerServer.URL,
			APIKey:  "provider-secret-key",
		},
		service.ChatRequest{
			Model:  "mimo-v2.5-pro",
			Stream: true,
			Messages: []service.ChatMessage{
				{Role: "user", Content: "你好"},
			},
		},
	)
	if err != nil {
		t.Fatalf("StreamComplete returned unexpected error: %v", err)
	}

	firstTokenCallbacks := 0
	result, err := stream.Run(func([]byte) error { return nil }, func() {
		firstTokenCallbacks++
	})
	if err != nil {
		t.Fatalf("stream.Run returned unexpected error: %v", err)
	}
	if firstTokenCallbacks != 1 {
		t.Fatalf("expected first token callback once for reasoning chunk, got %d", firstTokenCallbacks)
	}
	if !result.SawContentToken {
		t.Fatal("expected reasoning-only stream to satisfy first token detection")
	}
	if got := len(result.Response.Choices); got != 1 {
		t.Fatalf("expected 1 accumulated choice, got %d", got)
	}
	if got := result.Response.Choices[0].Message.Content; got != "" {
		t.Fatalf("expected reasoning text not to be mixed into assistant content, got %q", got)
	}
}

func TestOpenAIClientCompleteFallsBackToReasoningContentWhenContentIsEmpty(t *testing.T) {
	t.Parallel()

	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"deepseek-r1-distill-qwen-7b","usage":{"prompt_tokens":10,"completion_tokens":32,"total_tokens":42},"choices":[{"message":{"role":"assistant","content":"","reasoning_content":"你好。"}}]}`)
	}))
	t.Cleanup(providerServer.Close)

	client := NewOpenAIClient(http.DefaultClient)
	resp, statusCode, err := client.Complete(
		context.Background(),
		domain.ProviderTarget{
			BaseURL: providerServer.URL,
			APIKey:  "provider-secret-key",
		},
		service.ChatRequest{
			Model: "deepseek-r1-distill-qwen-7b",
			Messages: []service.ChatMessage{
				{Role: "user", Content: "你好"},
			},
		},
	)
	if err != nil {
		t.Fatalf("Complete returned unexpected error: %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", statusCode)
	}
	if got := resp.Choices[0].Message.Content; got != "你好。" {
		t.Fatalf("expected fallback content %q, got %q", "你好。", got)
	}
}

func TestOpenAIClientStreamCompleteFallsBackToReasoningContentWhenContentIsEmpty(t *testing.T) {
	t.Parallel()

	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected response writer to implement http.Flusher")
		}

		_, _ = io.WriteString(w, "data: {\"model\":\"deepseek-r1-distill-qwen-7b\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\",\"reasoning_content\":\"你\"}}]}\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, "data: {\"model\":\"deepseek-r1-distill-qwen-7b\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"\",\"reasoning_content\":\"好\"}}]}\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	t.Cleanup(providerServer.Close)

	client := NewOpenAIClient(http.DefaultClient)
	stream, _, err := client.StreamComplete(
		context.Background(),
		domain.ProviderTarget{
			BaseURL: providerServer.URL,
			APIKey:  "provider-secret-key",
		},
		service.ChatRequest{
			Model:  "deepseek-r1-distill-qwen-7b",
			Stream: true,
			Messages: []service.ChatMessage{
				{Role: "user", Content: "你好"},
			},
		},
	)
	if err != nil {
		t.Fatalf("StreamComplete returned unexpected error: %v", err)
	}

	result, err := stream.Run(func([]byte) error { return nil }, nil)
	if err != nil {
		t.Fatalf("stream.Run returned unexpected error: %v", err)
	}
	if got := result.Response.Choices[0].Message.Content; got != "你好" {
		t.Fatalf("expected fallback content %q, got %q", "你好", got)
	}
}

func TestOpenAIClientStreamCompletePreservesReasoningContentInForwardedChunks(t *testing.T) {
	t.Parallel()

	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected response writer to implement http.Flusher")
		}

		_, _ = io.WriteString(w, "data: {\"model\":\"deepseek-r1-distill-qwen-7b\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\",\"reasoning_content\":\"你好\"}}]}\n\n")
		flusher.Flush()
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	t.Cleanup(providerServer.Close)

	client := NewOpenAIClient(http.DefaultClient)
	stream, _, err := client.StreamComplete(
		context.Background(),
		domain.ProviderTarget{
			BaseURL: providerServer.URL,
			APIKey:  "provider-secret-key",
		},
		service.ChatRequest{
			Model:  "deepseek-r1-distill-qwen-7b",
			Stream: true,
			Messages: []service.ChatMessage{
				{Role: "user", Content: "你好"},
			},
		},
	)
	if err != nil {
		t.Fatalf("StreamComplete returned unexpected error: %v", err)
	}

	var chunks bytes.Buffer
	_, err = stream.Run(func(chunk []byte) error {
		_, writeErr := chunks.Write(chunk)
		return writeErr
	}, nil)
	if err != nil {
		t.Fatalf("stream.Run returned unexpected error: %v", err)
	}

	lines := strings.Split(chunks.String(), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: {") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		var decoded map[string]any
		if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
			t.Fatalf("failed to decode forwarded payload: %v", err)
		}
		choices, ok := decoded["choices"].([]any)
		if !ok || len(choices) == 0 {
			continue
		}
		choice, ok := choices[0].(map[string]any)
		if !ok {
			continue
		}
		delta, ok := choice["delta"].(map[string]any)
		if !ok {
			continue
		}
		if got, _ := delta["reasoning_content"].(string); got != "你好" {
			t.Fatalf("expected forwarded reasoning_content %q, got %q", "你好", got)
		}
		return
	}
	t.Fatal("expected forwarded chunk containing reasoning_content")
}
