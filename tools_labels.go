package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListLabelsInput struct {
	Page    int    `json:"page,omitempty"     jsonschema:"page number (1-based, default 1)"`
	PerPage int    `json:"per_page,omitempty" jsonschema:"items per page (default 50, max 1000)"`
	Search  string `json:"search,omitempty"   jsonschema:"filter labels by title"`
}

type CreateLabelInput struct {
	Title       string `json:"title"                 jsonschema:"label title,required"`
	Description string `json:"description,omitempty" jsonschema:"label description"`
	HexColor    string `json:"hex_color,omitempty"   jsonschema:"hex color without leading #"`
}

type AddLabelToTaskInput struct {
	TaskID  int64 `json:"task_id"  jsonschema:"the task ID,required"`
	LabelID int64 `json:"label_id" jsonschema:"the label ID to add,required"`
}

type RemoveLabelFromTaskInput struct {
	TaskID  int64 `json:"task_id"  jsonschema:"the task ID,required"`
	LabelID int64 `json:"label_id" jsonschema:"the label ID to remove,required"`
}

func registerLabelTools(server *mcp.Server, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_labels",
		Description: "List all labels created by the authenticated user. Supports search and pagination.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListLabelsInput) (*mcp.CallToolResult, any, error) {
		path := appendQuery("/labels", buildPageQuery(input.Search, input.Page, input.PerPage))
		raw, err := client.doRaw(ctx, "GET", path, nil)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return textResult(raw), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_label",
		Description: "Create a new label. Only title is required.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreateLabelInput) (*mcp.CallToolResult, any, error) {
		raw, err := client.doRaw(ctx, "POST", "/labels", input)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return textResult(raw), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_label_to_task",
		Description: "Add an existing label to a task.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input AddLabelToTaskInput) (*mcp.CallToolResult, any, error) {
		body := map[string]any{"label_id": input.LabelID}
		raw, err := client.doRaw(ctx, "POST", fmt.Sprintf("/tasks/%d/labels", input.TaskID), body)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return textResult(raw), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "remove_label_from_task",
		Description: "Remove a label from a task.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input RemoveLabelFromTaskInput) (*mcp.CallToolResult, any, error) {
		err := client.do(ctx, "DELETE", fmt.Sprintf("/tasks/%d/labels/%d", input.TaskID, input.LabelID), nil, nil)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return deleteResult(), nil, nil
	})
}
