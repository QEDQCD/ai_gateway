package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type RAGQueryRequest struct {
	KnowledgeBaseID string `json:"knowledge_base_id"`
	Question        string `json:"question"`
}

type RAGSource struct {
	DocumentID string  `json:"document_id"`
	ChunkID    string  `json:"chunk_id"`
	Score      float64 `json:"score"`
}

type RAGQueryResponse struct {
	Answer  string      `json:"answer"`
	Sources []RAGSource `json:"sources"`
}

type RAGProxyService interface {
	Query(ctx context.Context, req RAGQueryRequest, resolved any) (RAGQueryResponse, error)
}

type ragProxyService struct {
	httpClient *http.Client
}

type unavailableRAGProxyService struct{}

func NewRAGProxyService(httpClient *http.Client) RAGProxyService {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return ragProxyService{httpClient: httpClient}
}

func NewUnavailableRAGProxyService() RAGProxyService {
	return unavailableRAGProxyService{}
}

func (s ragProxyService) Query(ctx context.Context, req RAGQueryRequest, resolved any) (RAGQueryResponse, error) {
	requestContext, ok := resolvedRequestContext(resolved)
	if !ok {
		return RAGQueryResponse{}, StatusError{
			Code:    http.StatusUnauthorized,
			Message: "unauthorized",
			Err:     fmt.Errorf("%w: request context is missing", ErrUnauthorized),
		}
	}
	if strings.TrimSpace(requestContext.ProviderTarget.BaseURL) == "" {
		return RAGQueryResponse{}, StatusError{
			Code:    http.StatusBadGateway,
			Message: "rag proxy unavailable",
			Err:     fmt.Errorf("%w: rag target is missing", ErrProxyUnavailable),
		}
	}

	forward := map[string]any{
		"tenant_id":         requestContext.TenantID,
		"knowledge_base_id": req.KnowledgeBaseID,
		"question":          req.Question,
	}

	var response RAGQueryResponse
	statusCode, err := s.doJSONRequest(ctx, joinRAGURL(requestContext.ProviderTarget.BaseURL, "/internal/rag/query"), forward, &response)
	if err != nil {
		return RAGQueryResponse{}, StatusError{
			Code:    defaultStatusCode(statusCode),
			Message: "upstream request failed",
			Err:     err,
		}
	}

	return response, nil
}

func (s ragProxyService) doJSONRequest(ctx context.Context, url string, payload any, response any) (int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		respBody, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, fmt.Errorf("rag request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
		return resp.StatusCode, err
	}
	return resp.StatusCode, nil
}

func (unavailableRAGProxyService) Query(context.Context, RAGQueryRequest, any) (RAGQueryResponse, error) {
	return RAGQueryResponse{}, StatusError{
		Code:    http.StatusNotImplemented,
		Message: "rag proxy unavailable",
		Err:     fmt.Errorf("%w: rag proxy", ErrProxyUnavailable),
	}
}

func joinRAGURL(baseURL string, path string) string {
	return strings.TrimRight(baseURL, "/") + path
}
