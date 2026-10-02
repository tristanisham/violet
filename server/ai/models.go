package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// CatalogModel describes a Workers AI model without making an inference request.
type CatalogModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Task        struct {
		Name string `json:"name"`
	} `json:"task"`
}

// ListTextGenerationModels reads the paginated Workers AI catalog, including
// experimental models but excluding deprecated models by default.
func (g *AiGateway) ListTextGenerationModels(ctx context.Context) ([]CatalogModel, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	endpoint, err := url.Parse(strings.TrimSuffix(g.endpoint, "/run") + "/models/search")
	if err != nil {
		return nil, err
	}
	var models []CatalogModel
	seen := make(map[string]bool)
	const pageSize = 50
	for page := 1; page <= 100; page++ {
		query := url.Values{"task": {"Text Generation"}, "page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(pageSize)}}
		endpoint.RawQuery = query.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+g.apiToken)
		response, err := g.Client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("load Cloudflare model catalog: %w", err)
		}
		var result struct {
			Success    bool           `json:"success"`
			Result     []CatalogModel `json:"result"`
			ResultInfo struct {
				TotalPages int `json:"total_pages"`
			} `json:"result_info"`
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return nil, fmt.Errorf("Cloudflare model catalog: HTTP %d (check account ID and Workers AI Read token permission)", response.StatusCode)
		}
		err = json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&result)
		response.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("decode Cloudflare model catalog: %w", err)
		}
		if !result.Success {
			return nil, fmt.Errorf("Cloudflare model catalog request was unsuccessful")
		}
		added := 0
		for _, model := range result.Result {
			if model.Name == "" || seen[model.Name] {
				continue
			}
			seen[model.Name] = true
			added++
			if model.Task.Name == "" || model.Task.Name == "Text Generation" {
				models = append(models, model)
			}
		}
		if (result.ResultInfo.TotalPages > 0 && page >= result.ResultInfo.TotalPages) || len(result.Result) < pageSize {
			sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })
			return models, nil
		}
		if added == 0 {
			return nil, fmt.Errorf("Cloudflare model catalog repeated a page")
		}
	}
	return nil, fmt.Errorf("Cloudflare model catalog exceeded pagination limit")
}
