// Package main implements an MCP server that wraps the Vikunja v2 REST API.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// userInfo holds cached identity information about the authenticated user.
type userInfo struct {
	ID         int64
	Username   string
	BotOwnerID int64
}

// Client is a thin HTTP client for the Vikunja v2 REST API.
type Client struct {
	baseURL    string // always ends with /api/v2
	token      string
	httpClient *http.Client

	userOnce sync.Once
	user     *userInfo
	userErr  error
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

// fetchUserInfo calls GET /user once and caches the result.
func (c *Client) fetchUserInfo(ctx context.Context) (*userInfo, error) {
	c.userOnce.Do(func() {
		var raw struct {
			ID         int64  `json:"id"`
			Username   string `json:"username"`
			BotOwnerID int64  `json:"bot_owner_id"`
		}
		c.userErr = c.do(ctx, "GET", "/user", nil, &raw)
		if c.userErr != nil {
			return
		}
		c.user = &userInfo{
			ID:         raw.ID,
			Username:   raw.Username,
			BotOwnerID: raw.BotOwnerID,
		}
	})
	return c.user, c.userErr
}

// isBot returns true if the authenticated user is a bot (has a non-zero BotOwnerID).
// Returns false on error so callers can skip bot-specific logic gracefully.
func (c *Client) isBot(ctx context.Context) bool {
	info, err := c.fetchUserInfo(ctx)
	return err == nil && info.BotOwnerID > 0
}

// isEmptyPaginatedResult returns true if raw JSON is a paginated envelope with zero items.
func isEmptyPaginatedResult(raw []byte) bool {
	var envelope struct {
		Total int              `json:"total"`
		Items *json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return false
	}
	if envelope.Items == nil {
		return false
	}
	return envelope.Total == 0 && (string(*envelope.Items) == "[]" || string(*envelope.Items) == "null")
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
	"bot_owner_id": true,
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

var attachmentFields = map[string]bool{
	"id": true, "task_id": true, "created_by": true,
	"file": true, "created": true,
}

var fileFields = map[string]bool{
	"id": true, "name": true, "mime": true, "size": true, "created": true,
}

// uploadResultFields whitelists the upload response envelope fields.
// The "success" array contains attachment objects, filtered via nestedWhitelists.
var uploadResultFields = map[string]bool{
	"success": true, "errors": true,
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
	"file":       fileFields,
	"success":    attachmentFields,
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
// For 401/403 responses it appends actionable guidance so the LLM can
// report the problem clearly instead of retrying blindly.
func errorResult(err error) *mcp.CallToolResult {
	msg := err.Error()
	var ve *VikunjaError
	if errors.As(err, &ve) {
		switch ve.Status {
		case http.StatusUnauthorized:
			msg += "\n\nThe API token is invalid or expired. Check that VIKUNJA_TOKEN is set to a valid, non-expired token."
		case http.StatusForbidden:
			msg += "\n\nThe API token does not have permission for this operation. The token needs the correct permission group and scope granted in the Vikunja UI under Settings > API Tokens."
		}
	}
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
	}
}

// deleteResult returns a success message for delete operations (which return 204).
func deleteResult() *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "deleted"}},
	}
}

// normalizeDate converts a bare YYYY-MM-DD date to a full RFC 3339 datetime
// (YYYY-MM-DDT00:00:00Z). Strings that already contain a time component or
// are not valid dates are returned unchanged.
func normalizeDate(s string) string {
	if _, err := time.Parse("2006-01-02", s); err == nil {
		return s + "T00:00:00Z"
	}
	return s
}

// normalizeDatePtr is normalizeDate for optional (pointer) fields.
func normalizeDatePtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := normalizeDate(*s)
	return &v
}

// dateKeys are the task fields that hold datetime values.
var dateKeys = map[string]bool{
	"due_date":   true,
	"start_date": true,
	"end_date":   true,
}

// normalizeDateMapKeys normalizes known date fields in a task map.
// Used by batch operations where input is untyped map[string]any.
func normalizeDateMapKeys(m map[string]any) {
	for key := range dateKeys {
		if v, ok := m[key].(string); ok {
			m[key] = normalizeDate(v)
		}
	}
	if reminders, ok := m["reminders"].([]any); ok {
		for _, r := range reminders {
			if rm, ok := r.(map[string]any); ok {
				if v, ok := rm["reminder"].(string); ok {
					rm["reminder"] = normalizeDate(v)
				}
			}
		}
	}
}

// doUpload sends a multipart/form-data POST streaming from an open file.
// The file is read in a background goroutine via io.Pipe to avoid buffering
// the entire file in memory. Returns the raw response body bytes.
// Returns *VikunjaError on non-2xx.
func (c *Client) doUpload(ctx context.Context, path string, file *os.File, fileName string) ([]byte, error) {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	contentType := writer.FormDataContentType()

	go func() {
		part, err := writer.CreateFormFile("files", fileName)
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err = io.Copy(part, file); err != nil {
			pw.CloseWithError(err)
			return
		}
		if err = writer.Close(); err != nil {
			pw.CloseWithError(err)
			return
		}
		pw.Close() //nolint:gosec // all errors already propagated via CloseWithError
	}()

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+path, pr) //nolint:gosec // URL is from trusted config
	if err != nil {
		pr.Close() //nolint:gosec // unblock goroutine; error already captured
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", contentType)

	resp, err := c.httpClient.Do(req) //nolint:gosec // covered above
	if err != nil {
		pr.Close() //nolint:gosec // unblock goroutine; error already captured
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var ve VikunjaError
		if jsonErr := json.Unmarshal(data, &ve); jsonErr != nil {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
		}
		if ve.Status == 0 {
			ve.Status = resp.StatusCode
		}
		return nil, &ve
	}

	return data, nil
}

// maxDownloadSize is the maximum response body size (in bytes) that doDownload
// will write to disk. Downloads exceeding this are aborted.
// Defined as var (not const) to allow test overrides.
var maxDownloadSize int64 = 100 * 1024 * 1024 // 100 MB

// doDownload sends an authenticated GET and writes the response body to destPath.
// Returns *VikunjaError on non-2xx responses.
func (c *Client) doDownload(ctx context.Context, path, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+path, http.NoBody) //nolint:gosec // URL is from trusted config
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req) //nolint:gosec // covered above
	if err != nil {
		return fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		data, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("reading error response: %w", readErr)
		}
		var ve VikunjaError
		if jsonErr := json.Unmarshal(data, &ve); jsonErr != nil {
			return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(data))
		}
		if ve.Status == 0 {
			ve.Status = resp.StatusCode
		}
		return &ve
	}

	// Write to a temp file then link atomically to prevent partial writes
	// and TOCTOU overwrites of files created between validation and write.
	tmp, err := os.CreateTemp(filepath.Dir(destPath), ".download-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()

	limited := io.LimitReader(resp.Body, maxDownloadSize+1)
	n, copyErr := io.Copy(tmp, limited)
	if copyErr != nil {
		tmp.Close() //nolint:gosec // best-effort close in error path
		removeTempFile(tmpName)
		return fmt.Errorf("writing file: %w", copyErr)
	}
	if n > maxDownloadSize {
		tmp.Close() //nolint:gosec // best-effort close in error path
		removeTempFile(tmpName)
		return fmt.Errorf("download too large: exceeded %d bytes", maxDownloadSize)
	}
	if err = tmp.Close(); err != nil {
		removeTempFile(tmpName)
		return fmt.Errorf("closing temp file: %w", err)
	}
	// Use Link (atomic create-or-fail) instead of Rename to prevent overwriting
	// a file created between validation and write (TOCTOU). Link fails if
	// destPath already exists. Both paths are in the same directory, so
	// cross-filesystem issues don't apply.
	if err = os.Link(tmpName, destPath); err != nil {
		removeTempFile(tmpName)
		return fmt.Errorf("creating output file: %w", err)
	}
	removeTempFile(tmpName)
	return nil
}

// removeTempFile removes a temporary file, logging the error if removal fails.
// Used for best-effort cleanup in error paths.
func removeTempFile(path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "warning: failed to remove temp file %s: %v\n", path, err)
	}
}
