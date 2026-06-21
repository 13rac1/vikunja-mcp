package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Client is a thin HTTP client for the Vikunja v2 REST API.
type Client struct {
	baseURL    string // always ends with /api/v2
	token      string
	httpClient *http.Client
}

// NewClient creates a Vikunja API client. baseURL can be either the instance
// root (e.g. "https://vikunja.example.com") or already include /api/v2.
func NewClient(baseURL, token string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(baseURL, "/api/v2") {
		baseURL += "/api/v2"
	}
	return &Client{
		baseURL:    baseURL,
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// VikunjaError represents an error response from the Vikunja v2 API.
// Matches the vikunjaErrorModel (RFC 9457 + Vikunja numeric code).
type VikunjaError struct {
	Status int    `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Code   int    `json:"code"`
}

func (e *VikunjaError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%s: %s (HTTP %d, code %d)", e.Title, e.Detail, e.Status, e.Code)
	}
	return fmt.Sprintf("%s (HTTP %d, code %d)", e.Title, e.Status, e.Code)
}

// doRaw executes an HTTP request and returns the raw response body bytes.
// Returns nil bytes for 204 No Content. Returns *VikunjaError on non-2xx.
func (c *Client) doRaw(ctx context.Context, method, path string, body any) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshaling request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var ve VikunjaError
		if err := json.Unmarshal(data, &ve); err != nil {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
		}
		if ve.Status == 0 {
			ve.Status = resp.StatusCode
		}
		return nil, &ve
	}

	return data, nil
}

// do executes an HTTP request and decodes the JSON response into result.
// Built on top of doRaw.
func (c *Client) do(ctx context.Context, method, path string, body, result any) error {
	data, err := c.doRaw(ctx, method, path, body)
	if err != nil || data == nil || result == nil {
		return err
	}
	if err := json.Unmarshal(data, result); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	return nil
}

// buildPageQuery returns query parameters for the standard page/per_page/q pattern.
func buildPageQuery(search string, page, perPage int) url.Values {
	params := url.Values{}
	if search != "" {
		params.Set("q", search)
	}
	if page > 0 {
		params.Set("page", strconv.Itoa(page))
	}
	if perPage > 0 {
		params.Set("per_page", strconv.Itoa(perPage))
	}
	return params
}

// appendQuery appends query parameters to a path if any are set.
func appendQuery(path string, params url.Values) string {
	if len(params) == 0 {
		return path
	}
	return path + "?" + params.Encode()
}

// textResult wraps raw JSON bytes as an MCP text content result.
func textResult(raw []byte) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}},
	}
}

// errorResult converts an error into an MCP error result.
func errorResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}

// deleteResult returns a success message for delete operations (which return 204).
func deleteResult() *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "deleted"}},
	}
}
