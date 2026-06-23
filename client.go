// Package main implements an MCP server that wraps the Vikunja v2 REST API.
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

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader) //nolint:gosec // URL is from trusted config, not user input
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req) //nolint:gosec // covered by the nolint above
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

// Field whitelists for response filtering. Only these fields are kept in tool responses.
var userFields = map[string]bool{
	"id": true, "username": true, "name": true,
}

var taskFields = map[string]bool{
	"id": true, "title": true, "description": true, "done": true,
	"done_at": true, "due_date": true, "project_id": true,
	"priority": true, "start_date": true, "end_date": true,
	"hex_color": true, "percent_done": true, "identifier": true,
	"index": true, "repeat_after": true, "repeat_mode": true,
	"assignees": true, "labels": true, "created_by": true,
	"created": true, "updated": true,
	"bucket_id": true, "is_favorite": true,
	"related_tasks": true, "reminders": true,
}

var projectFields = map[string]bool{
	"id": true, "title": true, "description": true,
	"identifier": true, "hex_color": true,
	"parent_project_id": true, "is_archived": true,
	"owner": true, "is_favorite": true,
	"created": true, "updated": true,
}

var labelFields = map[string]bool{
	"id": true, "title": true, "description": true,
	"hex_color": true, "created_by": true,
	"created": true, "updated": true,
}

var commentFields = map[string]bool{
	"id": true, "comment": true, "author": true,
	"created": true, "updated": true,
}

var timeEntryFields = map[string]bool{
	"id": true, "task_id": true, "project_id": true,
	"user_id": true, "start_time": true, "end_time": true,
	"comment": true, "created": true, "updated": true,
}

var taskRelationFields = map[string]bool{
	"task_id": true, "other_task_id": true,
	"relation_kind": true, "created_by": true, "created": true,
}

var reminderFields = map[string]bool{
	"reminder": true, "relative_period": true, "relative_to": true,
}

var viewFields = map[string]bool{
	"id": true, "title": true, "project_id": true,
	"view_kind": true, "filter": true, "position": true,
	"bucket_configuration_mode": true, "default_bucket_id": true,
	"done_bucket_id": true, "created": true, "updated": true,
}

var bucketFields = map[string]bool{
	"id": true, "title": true, "position": true,
	"limit": true, "count": true, "tasks": true,
	"created": true, "updated": true,
}

// nestedWhitelists maps field names to whitelists for their nested objects.
var nestedWhitelists = map[string]map[string]bool{
	"created_by": userFields,
	"owner":      userFields,
	"author":     userFields,
	"assignees":  userFields,
	"labels":     labelFields,
	"reminders":  reminderFields,
	"tasks":      taskFields,
}

// mapOfArraysWhitelists maps field names whose value is map[string][]object
// (e.g. related_tasks: {"subtask": [...tasks], "related": [...tasks]}).
var mapOfArraysWhitelists = map[string]map[string]bool{
	"related_tasks": taskFields,
}

// filterObject removes non-whitelisted keys from a map and filters nested objects.
func filterObject(m map[string]any, whitelist map[string]bool) {
	for key := range m {
		if !whitelist[key] {
			delete(m, key)
			continue
		}
		// Standard nested objects/arrays (e.g. created_by, assignees, labels).
		if nested, ok := nestedWhitelists[key]; ok {
			switch v := m[key].(type) {
			case map[string]any:
				filterObject(v, nested)
			case []any:
				for _, item := range v {
					if obj, ok := item.(map[string]any); ok {
						filterObject(obj, nested)
					}
				}
			}
			continue
		}
		// Map-of-arrays (e.g. related_tasks: {"subtask": [...tasks]}).
		if itemWhitelist, ok := mapOfArraysWhitelists[key]; ok {
			if kindMap, ok := m[key].(map[string]any); ok {
				for _, arr := range kindMap {
					if items, ok := arr.([]any); ok {
						for _, item := range items {
							if obj, ok := item.(map[string]any); ok {
								filterObject(obj, itemWhitelist)
							}
						}
					}
				}
			}
		}
	}
}

// paginationFields are the envelope keys from Vikunja v2's Paginated[T] type.
var paginationFields = map[string]bool{
	"items": true, "total": true, "page": true,
	"per_page": true, "total_pages": true,
}

// filterJSON removes non-whitelisted fields from a JSON object or array of objects.
// Handles the v2 Paginated envelope: if the object has an "items" array, each item
// is filtered and only pagination metadata is kept at the top level.
func filterJSON(raw []byte, whitelist map[string]bool) ([]byte, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return raw, nil
	}

	if raw[0] == '[' {
		var arr []map[string]any
		if err := json.Unmarshal(raw, &arr); err != nil {
			return nil, err
		}
		for _, obj := range arr {
			filterObject(obj, whitelist)
		}
		return json.Marshal(arr)
	}

	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}

	// Detect paginated envelope: has "items" array.
	if items, ok := obj["items"]; ok {
		if arr, ok := items.([]any); ok {
			for _, item := range arr {
				if m, ok := item.(map[string]any); ok {
					filterObject(m, whitelist)
				}
			}
			for key := range obj {
				if !paginationFields[key] {
					delete(obj, key)
				}
			}
			return json.Marshal(obj)
		}
	}

	filterObject(obj, whitelist)
	return json.Marshal(obj)
}

// filteredResult filters raw JSON to whitelisted fields, then wraps as MCP text content.
func filteredResult(raw []byte, whitelist map[string]bool) *mcp.CallToolResult {
	filtered, err := filterJSON(raw, whitelist)
	if err != nil {
		return textResult(raw)
	}
	return textResult(filtered)
}

// errorLoggingMiddleware logs tool call errors to the MCP client via notifications/message.
func errorLoggingMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		result, err := next(ctx, method, req)
		if err != nil {
			return result, err
		}
		toolResult, ok := result.(*mcp.CallToolResult)
		if !ok || !toolResult.IsError {
			return result, nil
		}
		ss, ok := req.GetSession().(*mcp.ServerSession)
		if !ok {
			return result, nil
		}
		var msg string
		if len(toolResult.Content) > 0 {
			if tc, ok := toolResult.Content[0].(*mcp.TextContent); ok {
				msg = tc.Text
			}
		}
		if logErr := ss.Log(ctx, &mcp.LoggingMessageParams{
			Logger: "vikunja-mcp",
			Level:  "error",
			Data:   map[string]string{"method": method, "error": msg},
		}); logErr != nil {
			return result, fmt.Errorf("logging error: %w", logErr)
		}
		return result, nil
	}
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
