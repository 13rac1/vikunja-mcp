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

type UpdateProjectInput struct {
	ID              int64   `json:"id"                                jsonschema:"the project ID,required"`
	Title           *string `json:"title,omitempty"                   jsonschema:"new title"`
	Description     *string `json:"description,omitempty"             jsonschema:"new description"`
	Identifier      *string `json:"identifier,omitempty"              jsonschema:"new short identifier"`
	HexColor        *string `json:"hex_color,omitempty"               jsonschema:"new hex color without #"`
	ParentProjectID *int64  `json:"parent_project_id,omitempty"       jsonschema:"new parent project ID"`
	IsArchived      *bool   `json:"is_archived,omitempty"             jsonschema:"archive or unarchive the project"`
}

type DeleteProjectInput struct {
	ID int64 `json:"id" jsonschema:"the numeric project ID to delete,required"`
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
		result := filteredResult(raw, projectFields)
		if isEmptyPaginatedResult(raw) && client.isBot(ctx) {
			result.Content = append(result.Content, &mcp.TextContent{
				Text: "\n\nThis is a bot user. Bot users start with no access to any projects. " +
					"The bot's owner must share projects with this bot in Vikunja " +
					"(project settings > sharing, or Settings > Bots) before they will appear here.",
			})
		}
		return result, nil, nil
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

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_project",
		Description: "Update a project's fields. Only the provided fields are changed (partial update via PATCH).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpdateProjectInput) (*mcp.CallToolResult, any, error) {
		raw, err := client.doRaw(ctx, "PATCH", fmt.Sprintf("/projects/%d", input.ID), input)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, projectFields), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_project",
		Description: "Delete a project by its numeric ID. This also deletes all tasks in the project.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeleteProjectInput) (*mcp.CallToolResult, any, error) {
		err := client.do(ctx, "DELETE", fmt.Sprintf("/projects/%d", input.ID), nil, nil)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return deleteResult(), nil, nil
	})
}
