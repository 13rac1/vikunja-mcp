package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListProjectsInput struct {
	Page    int    `json:"page,omitempty"     jsonschema:"page number (1-based, default 1)"`
	PerPage int    `json:"per_page,omitempty" jsonschema:"items per page (default 50, max 1000)"`
	Search  string `json:"search,omitempty"   jsonschema:"search string to filter projects by title"`
}

type GetProjectInput struct {
	ID int64 `json:"id" jsonschema:"the numeric project ID,required"`
}

type CreateProjectInput struct {
	Title           string `json:"title"                       jsonschema:"the project title,required"`
	Description     string `json:"description,omitempty"       jsonschema:"the project description"`
	ParentProjectID int64  `json:"parent_project_id,omitempty" jsonschema:"parent project ID for nesting"`
	Identifier      string `json:"identifier,omitempty"        jsonschema:"short identifier for task numbering (e.g. PROJ, max 10 chars)"`
	HexColor        string `json:"hex_color,omitempty"         jsonschema:"hex color without leading #"`
}

func registerProjectTools(server *mcp.Server, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_projects",
		Description: "List all projects the authenticated user has access to. Supports search and pagination.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListProjectsInput) (*mcp.CallToolResult, any, error) {
		path := appendQuery("/projects", buildPageQuery(input.Search, input.Page, input.PerPage))
		raw, err := client.doRaw(ctx, "GET", path, nil)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, projectFields), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_project",
		Description: "Get a project by its numeric ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetProjectInput) (*mcp.CallToolResult, any, error) {
		raw, err := client.doRaw(ctx, "GET", fmt.Sprintf("/projects/%d", input.ID), nil)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, projectFields), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_project",
		Description: "Create a new project. Only title is required.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreateProjectInput) (*mcp.CallToolResult, any, error) {
		raw, err := client.doRaw(ctx, "POST", "/projects", input)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, projectFields), nil, nil
	})
}
