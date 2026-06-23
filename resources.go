package main

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerResources(server *mcp.Server, client *Client) {
	server.AddResource(&mcp.Resource{
		URI:         "vikunja://projects",
		Name:        "All Projects",
		Description: "List of all Vikunja projects",
		MIMEType:    "application/json",
	}, func(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		raw, err := client.doRaw(ctx, "GET", "/projects?per_page=100", nil)
		if err != nil {
			return nil, err
		}
		filtered, err := filterJSON(raw, projectFields)
		if err != nil {
			return nil, fmt.Errorf("filtering response: %w", err)
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{URI: "vikunja://projects", Text: string(filtered)}},
		}, nil
	})

	projectIDPattern := regexp.MustCompile(`^vikunja://projects/(\d+)$`)
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "vikunja://projects/{id}",
		Name:        "Project",
		Description: "A Vikunja project by ID",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		matches := projectIDPattern.FindStringSubmatch(req.Params.URI)
		if matches == nil {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}
		id, err := strconv.ParseInt(matches[1], 10, 64)
		if err != nil {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}
		raw, err := client.doRaw(ctx, "GET", fmt.Sprintf("/projects/%d", id), nil)
		if err != nil {
			return nil, err
		}
		filtered, err := filterJSON(raw, projectFields)
		if err != nil {
			return nil, fmt.Errorf("filtering response: %w", err)
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{URI: req.Params.URI, Text: string(filtered)}},
		}, nil
	})

	projectTasksPattern := regexp.MustCompile(`^vikunja://projects/(\d+)/tasks$`)
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "vikunja://projects/{project_id}/tasks",
		Name:        "Project Tasks",
		Description: "Tasks in a Vikunja project",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		matches := projectTasksPattern.FindStringSubmatch(req.Params.URI)
		if matches == nil {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}
		id, err := strconv.ParseInt(matches[1], 10, 64)
		if err != nil {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}
		raw, err := client.doRaw(ctx, "GET", fmt.Sprintf("/projects/%d/tasks?per_page=100", id), nil)
		if err != nil {
			return nil, err
		}
		filtered, err := filterJSON(raw, taskFields)
		if err != nil {
			return nil, fmt.Errorf("filtering response: %w", err)
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{URI: req.Params.URI, Text: string(filtered)}},
		}, nil
	})

	taskIDPattern := regexp.MustCompile(`^vikunja://tasks/(\d+)$`)
	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: "vikunja://tasks/{id}",
		Name:        "Task",
		Description: "A Vikunja task by ID",
		MIMEType:    "application/json",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		matches := taskIDPattern.FindStringSubmatch(req.Params.URI)
		if matches == nil {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}
		id, err := strconv.ParseInt(matches[1], 10, 64)
		if err != nil {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}
		raw, err := client.doRaw(ctx, "GET", fmt.Sprintf("/tasks/%d", id), nil)
		if err != nil {
			return nil, err
		}
		filtered, err := filterJSON(raw, taskFields)
		if err != nil {
			return nil, fmt.Errorf("filtering response: %w", err)
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{URI: req.Params.URI, Text: string(filtered)}},
		}, nil
	})
}
