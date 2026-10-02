package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestAiGatewayRun(t *testing.T) {
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "test-account")
	t.Setenv("CLOUDFLARE_API_TOKEN", "test-token")
	gateway, err := NewAiGateway()
	if err != nil {
		t.Fatal(err)
	}
	if gateway.Client.Timeout <= 0 {
		t.Fatal("client must have a timeout")
	}

	chat := ChatRequest{
		Id: uuid.New(),
		Messages: []ChatMessage{
			{Role: "user", Content: "What is Cloudflare?"},
		},
	}
	if chat.Subject() != SubjectChat || chat.Content().(ChatRequest).Id != chat.Id || chat.Recipiant() != DefaultModel {
		t.Fatal("chat request no longer implements the message contract")
	}
	ctx := context.WithValue(context.Background(), struct{}{}, "test-context")
	gateway.Client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != "https://api.cloudflare.com/client/v4/accounts/test-account/ai/run" {
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
		}
		if req.Context().Value(struct{}{}) != "test-context" {
			t.Fatal("request context was not propagated")
		}
		for key, value := range map[string]string{
			"Authorization":     "Bearer test-token",
			"cf-aig-gateway-id": "default",
			"Content-Type":      "application/json",
		} {
			if req.Header.Get(key) != value {
				t.Errorf("unexpected %s header", key)
			}
		}
		var payload struct {
			Model string                     `json:"model"`
			Input map[string]json.RawMessage `json:"input"`
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Model != DefaultModel || len(payload.Input) != 1 {
			t.Fatalf("unexpected payload: %+v", payload)
		}
		var messages []ChatMessage
		if err := json.Unmarshal(payload.Input["messages"], &messages); err != nil {
			t.Fatal(err)
		}
		if len(messages) != 1 || messages[0] != chat.Messages[0] {
			t.Fatalf("unexpected messages: %+v", messages)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"success":true}`)), Header: make(http.Header)}, nil
	})
	response, err := gateway.Run(ctx, "", chat)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
}

func TestNewAiGatewayMissingCredentials(t *testing.T) {
	for _, name := range []string{"CLOUDFLARE_ACCOUNT_ID", "CLOUDFLARE_API_TOKEN"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("CLOUDFLARE_ACCOUNT_ID", "test-account")
			t.Setenv("CLOUDFLARE_API_TOKEN", "test-token")
			t.Setenv(name, "")
			if _, err := NewAiGateway(); err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("expected missing %s error, got %v", name, err)
			}
		})
	}
}

func TestAiGatewayRejectsRedirectsAndErrors(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Setenv("CLOUDFLARE_ACCOUNT_ID", "test-account")
			t.Setenv("CLOUDFLARE_API_TOKEN", "test-token")
			gateway, err := NewAiGateway()
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			gateway.Client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{
					StatusCode: status,
					Header:     http.Header{"Location": {"https://other.example/inference"}},
					Body:       io.NopCloser(strings.NewReader("error")),
				}, nil
			})
			if _, err := gateway.Run(context.Background(), "custom-model", ChatRequest{}); err == nil {
				t.Fatal("expected an HTTP error")
			}
			if calls != 1 {
				t.Fatalf("request redirected or retried: %d calls", calls)
			}
		})
	}
}
