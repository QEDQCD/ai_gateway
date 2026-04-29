package provider

import (
	"bytes"
	"context"
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
	resp, err := stream.Run(func(chunk []byte) error {
		_, writeErr := chunks.Write(chunk)
		return writeErr
	})
	if err != nil {
		t.Fatalf("stream.Run returned unexpected error: %v", err)
	}

	if got := chunks.String(); !strings.Contains(got, "data: [DONE]") {
		t.Fatalf("expected stream output to include [DONE], got %q", got)
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
