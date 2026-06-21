package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListCommentsInput struct {
	TaskID  int64 `json:"task_id"            jsonschema:"the task ID,required"`
	Page    int   `json:"page,omitempty"     jsonschema:"page number (1-based, default 1)"`
	PerPage int   `json:"per_page,omitempty" jsonschema:"items per page (default 50, max 1000)"`
}

type AddCommentInput struct {
	TaskID  int64  `json:"task_id" jsonschema:"the task ID,required"`
	Comment string `json:"comment" jsonschema:"the comment text,required"`
}

func registerCommentTools(server *mcp.Server, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_comments",
		Description: "List comments on a task. Supports pagination.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListCommentsInput) (*mcp.CallToolResult, any, error) {
		path := appendQuery(fmt.Sprintf("/tasks/%d/comments", input.TaskID), buildPageQuery("", input.Page, input.PerPage))
		raw, err := client.doRaw(ctx, "GET", path, nil)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return textResult(raw), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_comment",
		Description: "Add a comment to a task.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input AddCommentInput) (*mcp.CallToolResult, any, error) {
		body := map[string]any{"comment": input.Comment}
		raw, err := client.doRaw(ctx, "POST", fmt.Sprintf("/tasks/%d/comments", input.TaskID), body)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return textResult(raw), nil, nil
	})
}
