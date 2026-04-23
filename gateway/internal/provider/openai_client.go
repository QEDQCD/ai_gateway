package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/liwenjian/ai_gateway/gateway/internal/domain"
	"github.com/liwenjian/ai_gateway/gateway/internal/service"
)

type OpenAIClient struct {
	httpClient *http.Client
}

func NewOpenAIClient(httpClient *http.Client) *OpenAIClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &OpenAIClient{httpClient: httpClient}
}

func (c *OpenAIClient) Complete(ctx context.Context, target domain.ProviderTarget, request service.ChatRequest) (service.ChatResponse, int, error) {
	var response service.ChatResponse
	statusCode, err := c.doJSONRequest(ctx, http.MethodPost, joinURL(target.BaseURL, "/chat/completions"), target.APIKey, request, &response)
	return response, statusCode, err
}

func (c *OpenAIClient) CreateEmbedding(ctx context.Context, target domain.ProviderTarget, request service.EmbeddingsRequest) (service.EmbeddingsResponse, int, error) {
	var response service.EmbeddingsResponse
	statusCode, err := c.doJSONRequest(ctx, http.MethodPost, joinURL(target.BaseURL, "/embeddings"), target.APIKey, request, &response)
	return response, statusCode, err
}

func (c *OpenAIClient) doJSONRequest(ctx context.Context, method string, url string, apiKey string, payload any, response any) (int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		respBody, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, fmt.Errorf("provider request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
		return resp.StatusCode, err
	}

	return resp.StatusCode, nil
}

func joinURL(baseURL string, path string) string {
	return strings.TrimRight(baseURL, "/") + path
}
