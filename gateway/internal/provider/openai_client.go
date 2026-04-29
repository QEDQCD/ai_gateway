package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/example/ai_gateway/gateway/internal/domain"
	"github.com/example/ai_gateway/gateway/internal/service"
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

func (c *OpenAIClient) StreamComplete(ctx context.Context, target domain.ProviderTarget, request service.ChatRequest) (service.ChatCompletionStream, int, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return service.ChatCompletionStream{}, 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, joinURL(target.BaseURL, "/chat/completions"), bytes.NewReader(body))
	if err != nil {
		return service.ChatCompletionStream{}, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+target.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return service.ChatCompletionStream{}, 0, err
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		return service.ChatCompletionStream{}, resp.StatusCode, fmt.Errorf("provider request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "text/event-stream; charset=utf-8"
	}

	return service.ChatCompletionStream{
		StatusCode:  resp.StatusCode,
		ContentType: contentType,
		Run: func(emit func([]byte) error, onFirstToken func()) (service.ChatStreamResult, error) {
			defer resp.Body.Close()
			return consumeChatCompletionStream(resp.Body, emit, onFirstToken)
		},
	}, resp.StatusCode, nil
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

type openAIChatCompletionChunk struct {
	Model   string              `json:"model"`
	Usage   *service.TokenUsage `json:"usage,omitempty"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

type openAIChoiceAccumulator struct {
	Role    string
	Content strings.Builder
}

func consumeChatCompletionStream(body io.Reader, emit func([]byte) error, onFirstToken func()) (service.ChatStreamResult, error) {
	reader := bufio.NewReader(body)
	accumulators := map[int]*openAIChoiceAccumulator{}
	choiceOrder := map[int]struct{}{}
	result := service.ChatStreamResult{}

	buildResponse := func() service.ChatResponse {
		finalResponse := result.Response
		indexes := make([]int, 0, len(choiceOrder))
		for index := range choiceOrder {
			indexes = append(indexes, index)
		}
		sort.Ints(indexes)
		finalResponse.Choices = make([]service.ChatChoice, 0, len(indexes))
		for _, index := range indexes {
			accumulator := accumulators[index]
			role := strings.TrimSpace(accumulator.Role)
			if role == "" {
				role = "assistant"
			}
			finalResponse.Choices = append(finalResponse.Choices, service.ChatChoice{
				Message: service.ChatMessage{
					Role:    role,
					Content: accumulator.Content.String(),
				},
			})
		}
		return finalResponse
	}

	for {
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			result.Response = buildResponse()
			return result, err
		}

		trimmed := strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(trimmed, "data: ") {
			payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data: "))
			if payload == "[DONE]" {
				if emit != nil {
					if emitErr := emit([]byte("data: [DONE]\n\n")); emitErr != nil {
						if result.SawContentToken {
							result.ClientAborted = true
						}
						result.Response = buildResponse()
						return result, emitErr
					}
				}
				break
			}

			var chunk openAIChatCompletionChunk
			if unmarshalErr := json.Unmarshal([]byte(payload), &chunk); unmarshalErr != nil {
				result.Response = buildResponse()
				return result, unmarshalErr
			}
			if strings.TrimSpace(chunk.Model) != "" {
				result.Response.Model = strings.TrimSpace(chunk.Model)
			}
			if chunk.Usage != nil {
				usageCopy := *chunk.Usage
				result.Response.Usage = &usageCopy
			}
			for _, choice := range chunk.Choices {
				accumulator := accumulators[choice.Index]
				if accumulator == nil {
					accumulator = &openAIChoiceAccumulator{}
					accumulators[choice.Index] = accumulator
				}
				choiceOrder[choice.Index] = struct{}{}
				if strings.TrimSpace(choice.Delta.Role) != "" {
					accumulator.Role = strings.TrimSpace(choice.Delta.Role)
				}
				accumulator.Content.WriteString(choice.Delta.Content)
				if !result.SawContentToken && choice.Delta.Content != "" {
					result.SawContentToken = true
					if onFirstToken != nil {
						onFirstToken()
					}
				}
			}

			if emit != nil {
				if emitErr := emit([]byte("data: " + payload + "\n\n")); emitErr != nil {
					if result.SawContentToken {
						result.ClientAborted = true
					}
					result.Response = buildResponse()
					return result, emitErr
				}
			}
		}

		if errors.Is(err, io.EOF) {
			break
		}
	}

	result.Response = buildResponse()
	return result, nil
}
