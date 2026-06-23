package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListViewsInput struct {
	ProjectID int64 `json:"project_id" jsonschema:"the project ID,required"`
}

type CreateViewInput struct {
	ProjectID               int64  `json:"project_id"                          jsonschema:"the project ID,required"`
	Title                   string `json:"title"                               jsonschema:"view title,required"`
	ViewKind                string `json:"view_kind"                           jsonschema:"view type: list kanban gantt table,required"`
	Filter                  string `json:"filter,omitempty"                    jsonschema:"Vikunja filter expression for the view"`
	BucketConfigurationMode string `json:"bucket_configuration_mode,omitempty" jsonschema:"how buckets are configured: none manual filter"`
}

type UpdateViewInput struct {
	ProjectID               int64   `json:"project_id"                          jsonschema:"the project ID,required"`
	ViewID                  int64   `json:"view_id"                             jsonschema:"the view ID,required"`
	Title                   *string `json:"title,omitempty"                     jsonschema:"new title"`
	Filter                  *string `json:"filter,omitempty"                    jsonschema:"new filter expression"`
	BucketConfigurationMode *string `json:"bucket_configuration_mode,omitempty" jsonschema:"new bucket configuration mode"`
}

type DeleteViewInput struct {
	ProjectID int64 `json:"project_id" jsonschema:"the project ID,required"`
	ViewID    int64 `json:"view_id"    jsonschema:"the view ID to delete,required"`
}

type ListBucketsInput struct {
	ProjectID int64 `json:"project_id" jsonschema:"the project ID,required"`
	ViewID    int64 `json:"view_id"    jsonschema:"the view ID,required"`
	Page      int   `json:"page,omitempty"     jsonschema:"page number (1-based, default 1)"`
	PerPage   int   `json:"per_page,omitempty" jsonschema:"items per page (default 50, max 1000)"`
}

type MoveTaskToBucketInput struct {
	ProjectID int64 `json:"project_id" jsonschema:"the project ID,required"`
	ViewID    int64 `json:"view_id"    jsonschema:"the view ID,required"`
	BucketID  int64 `json:"bucket_id"  jsonschema:"the target bucket ID,required"`
	TaskID    int64 `json:"task_id"    jsonschema:"the task ID to move,required"`
}

func registerViewTools(server *mcp.Server, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_views",
		Description: "List all views for a project (list, kanban, gantt, table).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListViewsInput) (*mcp.CallToolResult, any, error) {
		raw, err := client.doRaw(ctx, "GET", fmt.Sprintf("/projects/%d/views", input.ProjectID), nil)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, viewFields), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_view",
		Description: "Create a new view (list, kanban, gantt, table) for a project.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreateViewInput) (*mcp.CallToolResult, any, error) {
		body := map[string]any{
			"title":     input.Title,
			"view_kind": input.ViewKind,
		}
		if input.Filter != "" {
			body["filter"] = input.Filter
		}
		if input.BucketConfigurationMode != "" {
			body["bucket_configuration_mode"] = input.BucketConfigurationMode
		}
		raw, err := client.doRaw(ctx, "POST", fmt.Sprintf("/projects/%d/views", input.ProjectID), body)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, viewFields), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_view",
		Description: "Update a view's fields. Only the provided fields are changed.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateViewInput) (*mcp.CallToolResult, any, error) {
		body := map[string]any{}
		if input.Title != nil {
			body["title"] = *input.Title
		}
		if input.Filter != nil {
			body["filter"] = *input.Filter
		}
		if input.BucketConfigurationMode != nil {
			body["bucket_configuration_mode"] = *input.BucketConfigurationMode
		}
		raw, err := client.doRaw(ctx, "PATCH", fmt.Sprintf("/projects/%d/views/%d", input.ProjectID, input.ViewID), body)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, viewFields), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_view",
		Description: "Delete a view from a project.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeleteViewInput) (*mcp.CallToolResult, any, error) {
		err := client.do(ctx, "DELETE", fmt.Sprintf("/projects/%d/views/%d", input.ProjectID, input.ViewID), nil, nil)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return deleteResult(), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_buckets",
		Description: "List buckets and their tasks for a kanban view.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListBucketsInput) (*mcp.CallToolResult, any, error) {
		params := buildPageQuery("", input.Page, input.PerPage)
		path := appendQuery(fmt.Sprintf("/projects/%d/views/%d/buckets/tasks", input.ProjectID, input.ViewID), params)
		raw, err := client.doRaw(ctx, "GET", path, nil)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, bucketFields), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "move_task_to_bucket",
		Description: "Move a task to a different bucket in a kanban view.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input MoveTaskToBucketInput) (*mcp.CallToolResult, any, error) {
		body := map[string]any{"task_id": input.TaskID}
		raw, err := client.doRaw(ctx, "PUT", fmt.Sprintf("/projects/%d/views/%d/buckets/%d/tasks", input.ProjectID, input.ViewID, input.BucketID), body)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, taskFields), nil, nil
	})
}
