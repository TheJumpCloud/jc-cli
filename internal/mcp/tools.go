package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/klaassen-consulting/jc/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerTools adds MCP tools to the server.
// US-046 registers foundation tools; US-047 will add the full command set.
func (s *Server) registerTools() {
	// ping: A simple health-check tool.
	s.addTool("jc_ping", "Check if the JC MCP server is running and authenticated",
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			return textResult(fmt.Sprintf("jc MCP server v%s is running", version.Number)), nil, nil
		},
	)
}

// addTool wraps mcp.AddTool with rate limiting and audit logging.
func (s *Server) addTool(name, description string, handler func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error)) {
	tool := &mcp.Tool{
		Name:        name,
		Description: description,
	}

	wrappedHandler := func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
		// Rate limit check.
		if err := s.limiter.allow(); err != nil {
			s.auditLog.log(name, req.Params.Arguments, false, err.Error())
			return errorResult(err.Error()), nil, nil
		}

		// Execute the tool.
		result, out, err := handler(ctx, req, args)

		// Audit log.
		if err != nil {
			s.auditLog.log(name, req.Params.Arguments, false, err.Error())
		} else if result != nil && result.IsError {
			errMsg := "tool error"
			if len(result.Content) > 0 {
				if tc, ok := result.Content[0].(*mcp.TextContent); ok {
					errMsg = tc.Text
				}
			}
			s.auditLog.log(name, req.Params.Arguments, false, errMsg)
		} else {
			s.auditLog.log(name, req.Params.Arguments, true, "")
		}

		return result, out, err
	}

	mcp.AddTool(s.mcpServer, tool, wrappedHandler)
}

// addTypedTool wraps mcp.AddTool with typed input args, rate limiting, and audit logging.
func addTypedTool[In any](s *Server, name, description string, handler func(ctx context.Context, req *mcp.CallToolRequest, args In) (*mcp.CallToolResult, any, error)) {
	tool := &mcp.Tool{
		Name:        name,
		Description: description,
	}

	wrappedHandler := func(ctx context.Context, req *mcp.CallToolRequest, args In) (*mcp.CallToolResult, any, error) {
		// Rate limit check.
		if err := s.limiter.allow(); err != nil {
			s.auditLog.log(name, req.Params.Arguments, false, err.Error())
			return errorResult(err.Error()), nil, nil
		}

		// Execute the tool.
		result, out, err := handler(ctx, req, args)

		// Audit log.
		if err != nil {
			s.auditLog.log(name, req.Params.Arguments, false, err.Error())
		} else if result != nil && result.IsError {
			errMsg := "tool error"
			if len(result.Content) > 0 {
				if tc, ok := result.Content[0].(*mcp.TextContent); ok {
					errMsg = tc.Text
				}
			}
			s.auditLog.log(name, req.Params.Arguments, false, errMsg)
		} else {
			s.auditLog.log(name, req.Params.Arguments, true, "")
		}

		return result, out, err
	}

	mcp.AddTool(s.mcpServer, tool, wrappedHandler)
}

// textResult creates a simple text result.
func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: text},
		},
	}
}

// jsonResult creates a JSON result from a value.
func jsonResult(v any) (*mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling result: %w", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(data)},
		},
	}, nil
}

// errorResult creates an error result.
func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
		IsError: true,
	}
}
