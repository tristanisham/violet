package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const DefaultModel = "@cf/moonshotai/kimi-k2.6"

type AiGateway struct {
	Client   *http.Client
	endpoint string
	apiToken string
}

func NewAiGateway() (*AiGateway, error) {
	accountID := strings.TrimSpace(os.Getenv("CLOUDFLARE_ACCOUNT_ID"))
	if accountID == "" {
		return nil, fmt.Errorf("CLOUDFLARE_ACCOUNT_ID is required")
	}
	apiToken := strings.TrimSpace(os.Getenv("CLOUDFLARE_API_TOKEN"))
	if apiToken == "" {
		return nil, fmt.Errorf("CLOUDFLARE_API_TOKEN is required")
	}

	return &AiGateway{
		Client: &http.Client{
			Timeout: 2 * time.Minute,
			// Never forward credentials or replay inference requests on redirects.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		endpoint: "https://api.cloudflare.com/client/v4/accounts/" + url.PathEscape(accountID) + "/ai/run",
		apiToken: apiToken,
	}, nil
}

// Run sends a chat request through the default gateway. An empty model uses
// DefaultModel. The caller must close the returned response body.
func (g *AiGateway) Run(ctx context.Context, model string, chat ChatRequest) (*http.Response, error) {
	if model == "" {
		model = DefaultModel
	}
	payload := struct {
		Model string `json:"model"`
		Input struct {
			Messages []ChatMessage `json:"messages"`
		} `json:"input"`
	}{Model: model}
	// The request ID is local metadata, not a Workers AI input.
	payload.Input.Messages = chat.Messages
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+g.apiToken)
	req.Header.Set("cf-aig-gateway-id", "default")
	req.Header.Set("Content-Type", "application/json")

	response, err := g.Client.Do(req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		response.Body.Close()
		return nil, fmt.Errorf("Cloudflare AI request failed: HTTP %d", response.StatusCode)
	}
	return response, nil
}
