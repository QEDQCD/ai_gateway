package provider

import "net/http"

type DashScopeClient struct {
	*OpenAIClient
}

func NewDashScopeClient(httpClient *http.Client) *DashScopeClient {
	return &DashScopeClient{
		OpenAIClient: NewOpenAIClient(httpClient),
	}
}
