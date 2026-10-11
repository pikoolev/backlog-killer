package mcpserver

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pikoolev/backlog-killer/internal/attachments"
	"github.com/pikoolev/backlog-killer/internal/auth"
	"github.com/pikoolev/backlog-killer/internal/backlog"
)

type ListInput struct {
	Filters struct {
		Status []backlog.Status `json:"status,omitempty"`
	} `json:"filters"`
	Cursor string `json:"cursor,omitempty"`
}
type TicketInput struct {
	TicketID string `json:"ticketId"`
}
type AttachmentInput struct {
	AttachmentID string `json:"attachmentId"`
}
type ClaimInput struct {
	TicketIDs      []string `json:"ticketIds"`
	IdempotencyKey string   `json:"idempotencyKey"`
}
type CommentInput struct {
	TicketID string `json:"ticketId"`
	Body     string `json:"body"`
}
type SubmitInput struct {
	TicketID       string `json:"ticketId"`
	AgentComment   string `json:"agentComment"`
	WorkLog        string `json:"workLog"`
	IdempotencyKey string `json:"idempotencyKey"`
	Version        int64  `json:"version"`
}
type Output struct {
	Result any `json:"result"`
}

func New(service *backlog.Service, attachmentService *attachments.Service) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "backlog-killer", Version: "1.0.0"}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "backlog_list_tickets", Description: "List actionable project tickets"}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListInput) (*mcp.CallToolResult, Output, error) {
		principal, err := required(ctx, "tickets:read")
		if err != nil {
			return result(nil, err)
		}
		items, err := service.List(ctx, principal.TenantID, principal.ProjectID, input.Filters.Status, 50)
		return result(items, err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "backlog_get_ticket", Description: "Read one ticket with location and history"}, func(ctx context.Context, _ *mcp.CallToolRequest, input TicketInput) (*mcp.CallToolResult, Output, error) {
		principal, err := required(ctx, "tickets:read")
		if err != nil {
			return result(nil, err)
		}
		ticket, err := service.Detail(ctx, principal.TenantID, principal.ProjectID, input.TicketID, actor(principal))
		return result(ticket, err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "backlog_get_attachment", Description: "Read an authorized ticket attachment for inspection; returns metadata and base64 content"}, func(ctx context.Context, _ *mcp.CallToolRequest, input AttachmentInput) (*mcp.CallToolResult, Output, error) {
		principal, err := required(ctx, "tickets:read")
		if err != nil {
			return result(nil, err)
		}
		file, metadata, err := attachmentService.Open(ctx, principal.TenantID, principal.ProjectID, input.AttachmentID)
		if err != nil {
			return result(nil, err)
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			return result(nil, err)
		}
		return result(map[string]any{"metadata": metadata, "contentBase64": base64.StdEncoding.EncodeToString(data)}, nil)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "backlog_claim_batch", Description: "Atomically claim a coherent ticket batch"}, func(ctx context.Context, _ *mcp.CallToolRequest, input ClaimInput) (*mcp.CallToolResult, Output, error) {
		principal, err := required(ctx, "tickets:claim")
		if err != nil {
			return result(nil, err)
		}
		id, err := service.Claim(ctx, principal.TenantID, principal.ProjectID, principal.Kind+":"+principal.ID, input.IdempotencyKey, input.TicketIDs, actor(principal))
		return result(map[string]string{"batchId": id}, err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "backlog_add_comment", Description: "Add a durable ticket comment"}, func(ctx context.Context, _ *mcp.CallToolRequest, input CommentInput) (*mcp.CallToolResult, Output, error) {
		principal, err := required(ctx, "tickets:comment")
		if err != nil {
			return result(nil, err)
		}
		ticket, err := service.Get(ctx, principal.TenantID, principal.ProjectID, input.TicketID)
		if err != nil {
			return result(nil, err)
		}
		updated, err := service.Comment(ctx, principal.TenantID, principal.ProjectID, input.TicketID, ticket.Version, actor(principal), input.Body)
		return result(updated, err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "backlog_submit_for_review", Description: "Submit verified work for human review"}, func(ctx context.Context, _ *mcp.CallToolRequest, input SubmitInput) (*mcp.CallToolResult, Output, error) {
		principal, err := required(ctx, "tickets:complete")
		if err != nil {
			return result(nil, err)
		}
		if input.IdempotencyKey == "" {
			return result(nil, fmt.Errorf("idempotencyKey required"))
		}
		ticket, err := service.Transition(ctx, principal.TenantID, principal.ProjectID, input.TicketID, input.Version, principal.Kind+":"+principal.ID, input.IdempotencyKey, actor(principal), backlog.TransitionInput{To: backlog.Review, AgentComment: input.AgentComment, WorkLog: input.WorkLog})
		return result(ticket, err)
	})
	s.AddResource(&mcp.Resource{URI: "backlog://project/{projectId}/workflow", Name: "Backlog workflow", MIMEType: "text/markdown"}, func(ctx context.Context, request *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		principal, err := required(ctx, "tickets:read")
		if err != nil {
			return nil, err
		}
		expected := fmt.Sprintf("backlog://project/%s/workflow", principal.ProjectID)
		if request.Params == nil || request.Params.URI != expected {
			return nil, fmt.Errorf("resource not found")
		}
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: expected, MIMEType: "text/markdown", Text: "Read rework first, then priority and deadline. Read full event history and prior feedback. Claim atomically. Verify real changes. Submit agentComment and durable workLog. Only an administrator can accept or return work."}}}, nil
	})
	return s
}
func Handler(server *mcp.Server, allowedOrigins map[string]bool) http.Handler {
	protection := http.NewCrossOriginProtection()
	for origin := range allowedOrigins {
		if err := protection.AddTrustedOrigin(origin); err != nil {
			panic(fmt.Sprintf("invalid MCP origin: %v", err))
		}
	}
	inner := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{CrossOriginProtection: protection})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && !allowedOrigins[origin] {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		inner.ServeHTTP(w, r)
	})
}
func required(ctx context.Context, scope string) (auth.Principal, error) {
	p, ok := auth.FromContext(ctx)
	if !ok {
		return p, fmt.Errorf("unauthenticated")
	}
	if p.Kind == "agent" && !p.Scopes[scope] {
		return p, fmt.Errorf("missing scope %s", scope)
	}
	return p, nil
}
func actor(p auth.Principal) backlog.Actor {
	return backlog.Actor{Kind: p.Kind, ID: p.ID, Name: p.Name, Role: p.Role, Scopes: p.Scopes}
}
func result(value any, err error) (*mcp.CallToolResult, Output, error) {
	return nil, Output{Result: value}, err
}
