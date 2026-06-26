package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// currentUserFields is a broader whitelist than userFields (which is for nested user
// objects). The GET /user response includes extra fields useful for introspection.
var currentUserFields = map[string]bool{
	"id": true, "username": true, "name": true,
	"bot_owner_id": true, "is_local_user": true,
	"is_admin": true, "auth_provider": true,
}

func registerUserTools(server *mcp.Server, client *Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_current_user",
		Description: "Get the currently authenticated user's profile. " +
			"Returns user info including bot_owner_id — when non-zero, this token belongs to a bot user. " +
			"Useful for understanding what identity the MCP server is operating under.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		raw, err := client.doRaw(ctx, "GET", "/user", nil)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return filteredResult(raw, currentUserFields), nil, nil
	})
}
