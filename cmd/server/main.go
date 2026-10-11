package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pikoolev/backlog-killer/internal/attachments"
	"github.com/pikoolev/backlog-killer/internal/auth"
	"github.com/pikoolev/backlog-killer/internal/backlog"
	"github.com/pikoolev/backlog-killer/internal/config"
	"github.com/pikoolev/backlog-killer/internal/httpops"
	mcpserver "github.com/pikoolev/backlog-killer/internal/mcp"
	"github.com/pikoolev/backlog-killer/internal/projects"
	"github.com/pikoolev/backlog-killer/internal/store"
)

type api struct {
	service                                            *backlog.Service
	attachments                                        *attachments.Service
	invitations                                        *projects.Invitations
	tokens                                             *projects.Tokens
	admin                                              *projects.Admin
	flows                                              *auth.Flows
	embedIssuer                                        auth.EmbedIssuer
	webPublicURL, oidcPublicIssuer, oidcInternalIssuer string
	oidcClientID, oidcClientSecret                     string
	platformAdminSubjects                              map[string]bool
	authenticator                                      *auth.Authenticator
}
type errorBody struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	RequestID string         `json:"requestId"`
	Details   map[string]any `json:"details,omitempty"`
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		resp, err := http.Get("http://127.0.0.1:8080/healthz")
		if err != nil || resp.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	migrationPool, err := pgxpool.New(ctx, cfg.MigrationDatabaseURL)
	if err != nil {
		fatal(err)
	}
	if err = store.Migrate(ctx, migrationPool); err != nil {
		fatal(err)
	}
	if cfg.LocalDemoSeed {
		if err = store.SeedLocalDemo(ctx, migrationPool); err != nil {
			fatal(err)
		}
	}
	if cfg.Mode == "onprem" {
		bootstrap := store.OnPremBootstrap{TenantName: cfg.OnPremTenant, ProjectName: cfg.OnPremProject, ProjectKey: cfg.OnPremProjectKey, Issuer: cfg.OIDCPublicIssuer, OwnerSubject: cfg.OnPremOwnerSubject, OwnerEmail: cfg.OnPremOwnerEmail, ProjectOrigin: cfg.OnPremProjectOrigin}
		if err = store.BootstrapOnPrem(ctx, migrationPool, bootstrap); err != nil {
			fatal(err)
		}
	}
	migrationPool.Close()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		fatal(err)
	}
	if err = pool.Ping(ctx); err != nil {
		fatal(fmt.Errorf("connect runtime database: %w", err))
	}
	if err = store.ValidateRuntime(ctx, pool); err != nil {
		fatal(err)
	}
	if err = attachments.ValidateStorage(cfg.StoragePath); err != nil {
		fatal(err)
	}
	defer pool.Close()
	service := &backlog.Service{DB: pool}
	authentication := auth.New(pool, cfg.OIDCIssuer, cfg.OIDCPublicIssuer, cfg.APIPublicURL, cfg.OIDCAudience, cfg.EmbedSigningKey)
	platformAdmins := make(map[string]bool, len(cfg.PlatformAdminSubjects))
	for _, subject := range cfg.PlatformAdminSubjects {
		platformAdmins[subject] = true
	}
	application := &api{service: service, attachments: &attachments.Service{DB: pool, Root: cfg.StoragePath}, invitations: &projects.Invitations{DB: pool}, tokens: &projects.Tokens{DB: pool}, admin: &projects.Admin{DB: pool}, flows: &auth.Flows{DB: pool}, embedIssuer: auth.EmbedIssuer{Key: []byte(cfg.EmbedSigningKey), Issuer: cfg.APIPublicURL, Audience: cfg.OIDCAudience}, webPublicURL: cfg.WebPublicURL, oidcPublicIssuer: cfg.OIDCPublicIssuer, oidcInternalIssuer: cfg.OIDCIssuer, oidcClientID: cfg.OIDCClientID, oidcClientSecret: cfg.OIDCClientSecret, platformAdminSubjects: platformAdmins, authenticator: authentication}
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = application.attachments.Cleanup(context.Background())
				_, _ = pool.Exec(context.Background(), `DELETE FROM embed_auth_flows WHERE expires_at<now()-interval '1 hour'; DELETE FROM idempotency_keys WHERE created_at<now()-interval '7 days'; DELETE FROM invitations WHERE expires_at<now()-interval '30 days'`)
			}
		}
	}()
	router := chi.NewRouter()
	limiter, metrics := httpops.NewLimiter(), &httpops.Metrics{}
	router.Use(application.dashboardCors, middleware.RequestID, middleware.Recoverer, middleware.Timeout(30*time.Second), metrics.Middleware, limiter.Middleware, securityHeaders, authentication.Middleware)
	router.Get("/livez", func(w http.ResponseWriter, _ *http.Request) { jsonResponse(w, 200, map[string]string{"status": "ok"}) })
	router.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			writeError(w, r, err)
			return
		}
		jsonResponse(w, 200, map[string]string{"status": "ok"})
	})
	router.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			writeError(w, r, err)
			return
		}
		if err := attachments.ValidateStorage(cfg.StoragePath); err != nil {
			writeError(w, r, err)
			return
		}
		jsonResponse(w, 200, map[string]string{"status": "ready"})
	})
	router.Handle("/metrics", metrics)
	router.Get("/embed/auth/start", application.embedStart)
	router.Get("/embed/auth/callback", application.embedCallback)
	router.Route("/v1", func(r chi.Router) {
		r.Get("/me", application.me)
		r.Post("/invitations/accept", application.acceptInvitation)
		r.Post("/embed/token", application.embedToken)
		r.Options("/embed/token", application.embedTokenPreflight)
		r.Post("/tenants", application.createTenant)
		r.Post("/projects", application.createProject)
		r.Route("/projects/{projectId}", func(r chi.Router) {
			r.Use(application.projectIdentity)
			r.Use(application.cors)
			r.Options("/*", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
			r.Get("/tickets", application.list)
			r.Post("/tickets", application.create)
			r.Post("/tickets/{ticketId}/comments", application.comment)
			r.Patch("/tickets/{ticketId}", application.update)
			r.Delete("/tickets/{ticketId}", application.deleteTicket)
			r.Post("/tickets/{ticketId}/restore", application.restoreTicket)
			r.Post("/tickets/bulk", application.bulk)
			r.Delete("/attachments/{attachmentId}", application.deleteAttachment)
			r.Post("/tickets/{ticketId}/attachments", application.uploadAttachment)
			r.Get("/attachments/{attachmentId}", application.downloadAttachment)
			r.Get("/tickets/{ticketId}", application.get)
			r.Post("/tickets/{ticketId}/transition", application.transition)
			r.Post("/agent-batches", application.claim)
			r.Get("/agent-batches", application.listBatches)
			r.Delete("/agent-batches/{batchId}", application.unassignBatch)
			r.Post("/invitations", application.createInvitation)
			r.Get("/source", application.projectSource)
			r.Get("/settings", application.projectSettings)
			r.Post("/origins", application.addOrigin)
			r.Delete("/origins", application.removeOrigin)
			r.Patch("/members/{userId}", application.updateMember)
			r.Delete("/members/{userId}", application.removeMember)
			r.Post("/agent-tokens", application.createAgentToken)
			r.Get("/agent-tokens", application.listAgentTokens)
			r.Delete("/agent-tokens/{tokenId}", application.revokeAgentToken)
		})
	})
	allowed := map[string]bool{}
	for _, origin := range cfg.MCPAllowedOrigins {
		allowed[origin] = true
	}
	router.Handle("/mcp", mcpserver.Handler(mcpserver.New(service, application.attachments), allowed))
	router.Get("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, 200, map[string]any{"resource": cfg.APIPublicURL, "authorization_servers": []string{cfg.OIDCPublicIssuer}, "scopes_supported": []string{"tickets:read", "tickets:claim", "tickets:comment", "tickets:complete"}})
	})
	server := http.Server{Addr: "0.0.0.0:8080", Handler: http.MaxBytesHandler(router, 12<<20), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = server.Shutdown(shutdown)
	}()
	slog.Info("server listening", "address", server.Addr, "mode", cfg.Mode)
	if err = server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fatal(err)
	}
}
func (a *api) embedStart(w http.ResponseWriter, r *http.Request) {
	flowID, err := a.flows.Start(r.Context(), auth.EmbedStart{ProjectKey: r.URL.Query().Get("project_key"), Origin: r.URL.Query().Get("origin"), State: r.URL.Query().Get("state"), CodeChallenge: r.URL.Query().Get("code_challenge")})
	if err != nil {
		writeError(w, r, &backlog.Error{Code: "invalid_embed_origin", Status: 403, Message: "project origin not allowed"})
		return
	}
	callback := fmt.Sprintf("%s/embed/auth/callback?flow=%s", a.embedIssuer.Issuer, url.QueryEscape(flowID))
	authURL := fmt.Sprintf("%s/protocol/openid-connect/auth?client_id=backlog-killer-web&response_type=code&scope=openid%%20email&redirect_uri=%s&state=%s", a.oidcPublicIssuer, url.QueryEscape(callback), url.QueryEscape(flowID))
	http.Redirect(w, r, authURL, http.StatusFound)
}
func (a *api) embedCallback(w http.ResponseWriter, r *http.Request) {
	values := url.Values{"grant_type": {"authorization_code"}, "client_id": {a.oidcClientID}, "code": {r.URL.Query().Get("code")}, "redirect_uri": {fmt.Sprintf("%s/embed/auth/callback?flow=%s", a.embedIssuer.Issuer, url.QueryEscape(r.URL.Query().Get("state")))}}
	if a.oidcClientSecret != "" {
		values.Set("client_secret", a.oidcClientSecret)
	}
	response, err := http.Post(a.oidcInternalIssuer+"/protocol/openid-connect/token", "application/x-www-form-urlencoded", strings.NewReader(values.Encode()))
	if err != nil {
		writeError(w, r, &backlog.Error{Code: "oidc_exchange_failed", Status: 502, Message: "OIDC exchange failed"})
		return
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	var tokens struct {
		AccessToken string `json:"access_token"`
	}
	if response.StatusCode != 200 || json.Unmarshal(body, &tokens) != nil {
		writeError(w, r, &backlog.Error{Code: "oidc_exchange_failed", Status: 401, Message: "OIDC exchange rejected"})
		return
	}
	principal, err := a.authenticator.Authenticate(r.Context(), tokens.AccessToken)
	if err != nil {
		writeError(w, r, &backlog.Error{Code: "unauthorized", Status: 401, Message: "invalid OIDC token"})
		return
	}
	issued, err := a.flows.IssueCode(r.Context(), r.URL.Query().Get("flow"), principal.Subject)
	if err != nil {
		writeError(w, r, &backlog.Error{Code: "invalid_embed_flow", Status: 409, Message: "embed flow expired"})
		return
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'nonce-bkk'; frame-ancestors 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	payload, _ := json.Marshal(map[string]string{"type": "backlog-killer-auth", "state": issued.State, "code": issued.Code})
	fmt.Fprintf(w, "<!doctype html><script nonce=bkk>opener.postMessage(%s,%q);close()</script>", payload, issued.Origin)
}
func (a *api) embedTokenPreflight(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if _, _, err := a.flows.ValidateOrigin(r.Context(), r.URL.Query().Get("project_key"), origin); err != nil {
		writeError(w, r, &backlog.Error{Code: "invalid_embed_origin", Status: 403, Message: "origin not allowed"})
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Methods", "POST,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) embedToken(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	var input struct{ ProjectKey, Code, Verifier string }
	if err := decode(r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	tenant, project, err := a.flows.ValidateOrigin(r.Context(), input.ProjectKey, origin)
	if err != nil {
		writeError(w, r, &backlog.Error{Code: "invalid_embed_origin", Status: 403, Message: "origin not allowed"})
		return
	}
	subject, err := a.flows.Exchange(r.Context(), input.ProjectKey, origin, input.Code, input.Verifier)
	if err != nil {
		writeError(w, r, &backlog.Error{Code: "invalid_embed_code", Status: 401, Message: err.Error()})
		return
	}
	var userID, resolvedTenant, resolvedProject, role string
	err = a.service.DB.QueryRow(r.Context(), `SELECT user_id,tenant_id,project_id,role FROM resolve_project_identity($1,$2,$3)`, a.oidcPublicIssuer, subject, project).Scan(&userID, &resolvedTenant, &resolvedProject, &role)
	if err != nil || resolvedTenant != tenant {
		writeError(w, r, backlog.ErrNotFound)
		return
	}
	token, err := a.embedIssuer.Issue(subject, tenant, project, role)
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Vary", "Origin")
	jsonResponse(w, 200, map[string]any{"accessToken": token, "expiresIn": 300, "projectId": project})
}

func (a *api) createProject(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.FromContext(r.Context())
	if !ok || p.Kind == "agent" {
		writeError(w, r, &backlog.Error{Code: "unauthorized", Status: 401, Message: "authentication required"})
		return
	}
	var in struct{ TenantID, Name, ProjectKey string }
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	project, err := a.admin.CreateProjectForOwner(r.Context(), p.Issuer, p.Subject, in.TenantID, in.Name, in.ProjectKey)
	if err != nil {
		writeError(w, r, &backlog.Error{Code: "invalid_project", Status: 400, Message: err.Error()})
		return
	}
	jsonResponse(w, 201, project)
}

func (a *api) createTenant(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.FromContext(r.Context())
	if !ok || p.Kind == "agent" || !a.platformAdminSubjects[p.Subject] {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "platform administrator required"})
		return
	}
	var in struct{ Name, ProjectName, ProjectKey string }
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	project, err := a.admin.CreateTenant(r.Context(), p.Issuer, p.Subject, p.Email, in.Name, in.ProjectName, in.ProjectKey)
	if err != nil {
		writeError(w, r, &backlog.Error{Code: "invalid_tenant", Status: 400, Message: err.Error()})
		return
	}
	jsonResponse(w, 201, project)
}
func (a *api) projectSettings(w http.ResponseWriter, r *http.Request) {
	p, ok := requireAdmin(r)
	if !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "admin role required"})
		return
	}
	project, err := a.admin.Get(r.Context(), p.TenantID, p.ProjectID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	project.Role = p.Role
	jsonResponse(w, 200, project)
}
func (a *api) addOrigin(w http.ResponseWriter, r *http.Request) {
	p, ok := requireAdmin(r)
	if !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "admin role required"})
		return
	}
	var in struct{ Origin string }
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	if err := a.admin.AddOrigin(r.Context(), p.TenantID, p.ProjectID, in.Origin); err != nil {
		writeError(w, r, &backlog.Error{Code: "invalid_origin", Status: 400, Message: err.Error()})
		return
	}
	jsonResponse(w, 201, in)
}
func (a *api) removeOrigin(w http.ResponseWriter, r *http.Request) {
	p, ok := requireAdmin(r)
	if !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "admin role required"})
		return
	}
	if err := a.admin.RemoveOrigin(r.Context(), p.TenantID, p.ProjectID, r.URL.Query().Get("origin")); err != nil {
		writeError(w, r, backlog.ErrNotFound)
		return
	}
	w.WriteHeader(204)
}
func (a *api) updateMember(w http.ResponseWriter, r *http.Request) {
	p, ok := requireAdmin(r)
	if !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "admin role required"})
		return
	}
	var in struct{ Role string }
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	if err := a.admin.SetMemberRole(r.Context(), p.TenantID, p.ProjectID, chi.URLParam(r, "userId"), in.Role); err != nil {
		writeError(w, r, &backlog.Error{Code: "invalid_member", Status: 400, Message: err.Error()})
		return
	}
	w.WriteHeader(204)
}
func (a *api) removeMember(w http.ResponseWriter, r *http.Request) {
	p, ok := requireAdmin(r)
	if !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "admin role required"})
		return
	}
	if err := a.admin.RemoveMember(r.Context(), p.TenantID, p.ProjectID, chi.URLParam(r, "userId")); err != nil {
		writeError(w, r, &backlog.Error{Code: "invalid_member", Status: 400, Message: err.Error()})
		return
	}
	w.WriteHeader(204)
}

func requireAdmin(r *http.Request) (auth.Principal, bool) {
	p, ok := auth.FromContext(r.Context())
	return p, ok && (p.Role == "owner" || p.Role == "admin")
}
func (a *api) createAgentToken(w http.ResponseWriter, r *http.Request) {
	p, ok := requireAdmin(r)
	if !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "admin role required"})
		return
	}
	var input struct {
		Scopes    []string
		ExpiresAt *time.Time
	}
	if err := decode(r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	token, err := a.tokens.Create(r.Context(), p.TenantID, p.ProjectID, p.ID, input.Scopes, input.ExpiresAt)
	if err != nil {
		writeError(w, r, &backlog.Error{Code: "invalid_token", Status: 400, Message: err.Error()})
		return
	}
	jsonResponse(w, 201, token)
}
func (a *api) listAgentTokens(w http.ResponseWriter, r *http.Request) {
	p, ok := requireAdmin(r)
	if !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "admin role required"})
		return
	}
	tokens, err := a.tokens.List(r.Context(), p.TenantID, p.ProjectID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"items": tokens})
}
func (a *api) revokeAgentToken(w http.ResponseWriter, r *http.Request) {
	p, ok := requireAdmin(r)
	if !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "admin role required"})
		return
	}
	if err := a.tokens.Revoke(r.Context(), p.TenantID, p.ProjectID, chi.URLParam(r, "tokenId")); err != nil {
		writeError(w, r, backlog.ErrNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) createInvitation(w http.ResponseWriter, r *http.Request) {
	principal, ok := authorize(r, "")
	if !ok || (principal.Role != "owner" && principal.Role != "admin") {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "admin role required"})
		return
	}
	var input struct{ Email, Role string }
	if err := decode(r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	invitation, err := a.invitations.Create(r.Context(), principal.TenantID, principal.ProjectID, input.Email, input.Role, a.webPublicURL)
	if err != nil {
		writeError(w, r, err)
		return
	}
	jsonResponse(w, 201, invitation)
}
func (a *api) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.FromContext(r.Context())
	if !ok || !principal.EmailVerified {
		writeError(w, r, &backlog.Error{Code: "unauthorized", Status: 401, Message: "verified OIDC email required"})
		return
	}
	var input struct{ Code string }
	if err := decode(r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	tenant, project, role, err := a.invitations.Accept(r.Context(), input.Code, principal.Issuer, principal.Subject, principal.Email)
	if err != nil {
		writeError(w, r, &backlog.Error{Code: "invalid_invitation", Status: 409, Message: err.Error()})
		return
	}
	jsonResponse(w, 200, map[string]string{"tenantId": tenant, "projectId": project, "role": role})
}

func (a *api) me(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, r, &backlog.Error{Code: "unauthorized", Status: 401, Message: "authentication required"})
		return
	}
	rows, err := a.service.DB.Query(r.Context(), `SELECT tenant_id,project_id,project_key,project_name,role FROM list_user_projects($1,$2)`, p.Issuer, p.Subject)
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer rows.Close()
	type project struct {
		TenantID   string `json:"tenantId"`
		ProjectID  string `json:"projectId"`
		ProjectKey string `json:"projectKey"`
		Name       string `json:"name"`
		Role       string `json:"role"`
	}
	items := []project{}
	for rows.Next() {
		var item project
		if err = rows.Scan(&item.TenantID, &item.ProjectID, &item.ProjectKey, &item.Name, &item.Role); err != nil {
			writeError(w, r, err)
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, r, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"principal": p, "projects": items, "platformAdmin": a.platformAdminSubjects[p.Subject]})
}
func (a *api) list(w http.ResponseWriter, r *http.Request) {
	p, ok := authorize(r, "tickets:read")
	if !ok {
		writeError(w, r, &backlog.Error{Code: "unauthorized", Status: 401, Message: "authentication required"})
		return
	}
	project := chi.URLParam(r, "projectId")
	if p.ProjectID != "" && p.ProjectID != project {
		writeError(w, r, backlog.ErrNotFound)
		return
	}
	allowed := map[string]bool{"status": true, "page": true, "cursor": true, "limit": true}
	for key := range r.URL.Query() {
		if !allowed[key] {
			writeError(w, r, &backlog.Error{Code: "unknown_filter", Status: 400, Message: "unsupported ticket filter"})
			return
		}
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	var statuses []backlog.Status
	valid := map[backlog.Status]bool{backlog.New: true, backlog.Clarification: true, backlog.InProgress: true, backlog.Review: true, backlog.Rework: true, backlog.Done: true, backlog.Deleted: true}
	for _, raw := range r.URL.Query()["status"] {
		status := backlog.Status(raw)
		if !valid[status] {
			writeError(w, r, &backlog.Error{Code: "invalid_filter", Status: 400, Message: "invalid status filter"})
			return
		}
		statuses = append(statuses, status)
	}
	var cursor time.Time
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		var err error
		cursor, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			writeError(w, r, &backlog.Error{Code: "invalid_cursor", Status: 400, Message: "invalid cursor"})
			return
		}
	}
	items, next, err := a.service.ListFiltered(r.Context(), p.TenantID, project, statuses, r.URL.Query().Get("page"), limit, cursor)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var nextCursor any
	if next != nil {
		nextCursor = next.Format(time.RFC3339Nano)
	}
	jsonResponse(w, 200, map[string]any{"items": items, "nextCursor": nextCursor})
}
func (a *api) create(w http.ResponseWriter, r *http.Request) {
	p, ok := authorize(r, "tickets:comment")
	if !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "create not allowed"})
		return
	}
	if r.Header.Get("Idempotency-Key") == "" {
		writeError(w, r, &backlog.Error{Code: "idempotency_key_required", Status: 400, Message: "Idempotency-Key required"})
		return
	}
	var input backlog.CreateInput
	if err := decode(r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	ticket, err := a.service.Create(r.Context(), p.TenantID, chi.URLParam(r, "projectId"), p.Kind+":"+p.ID, r.Header.Get("Idempotency-Key"), actor(p), input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", ticket.Version))
	jsonResponse(w, 201, ticket)
}
func (a *api) deleteAttachment(w http.ResponseWriter, r *http.Request) {
	p, ok := authorize(r, "")
	if !ok || (p.Role != "owner" && p.Role != "admin" && p.Role != "triager") {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "delete attachment not allowed"})
		return
	}
	if err := a.attachments.Delete(r.Context(), p.TenantID, chi.URLParam(r, "projectId"), chi.URLParam(r, "attachmentId")); err != nil {
		writeError(w, r, backlog.ErrNotFound)
		return
	}
	w.WriteHeader(204)
}
func (a *api) listBatches(w http.ResponseWriter, r *http.Request) {
	p, ok := requireAdmin(r)
	if !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "admin role required"})
		return
	}
	items, err := a.service.ListBatches(r.Context(), p.TenantID, p.ProjectID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"items": items})
}
func (a *api) unassignBatch(w http.ResponseWriter, r *http.Request) {
	p, ok := requireAdmin(r)
	if !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "admin role required"})
		return
	}
	if err := a.service.UnassignBatch(r.Context(), p.TenantID, p.ProjectID, chi.URLParam(r, "batchId")); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(204)
}
func (a *api) projectSource(w http.ResponseWriter, r *http.Request) {
	p, ok := authorize(r, "tickets:read")
	if !ok {
		writeError(w, r, &backlog.Error{Code: "unauthorized", Status: 401, Message: "authentication required"})
		return
	}
	settings, err := a.admin.Get(r.Context(), p.TenantID, p.ProjectID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"projectId": settings.ID, "projectKey": settings.Key, "origins": settings.Origins})
}
func (a *api) comment(w http.ResponseWriter, r *http.Request) {
	p, ok := authorize(r, "tickets:comment")
	if !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "comment not allowed"})
		return
	}
	version, err := backlog.ParseVersion(r.Header.Get("If-Match"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input struct {
		Body string `json:"body"`
	}
	if err = decode(r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	ticket, err := a.service.Comment(r.Context(), p.TenantID, chi.URLParam(r, "projectId"), chi.URLParam(r, "ticketId"), version, actor(p), input.Body)
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", ticket.Version))
	jsonResponse(w, 200, ticket)
}
func (a *api) bulk(w http.ResponseWriter, r *http.Request) {
	p, ok := authorize(r, "")
	if !ok || (p.Role != "owner" && p.Role != "admin" && p.Role != "triager") {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "bulk not allowed"})
		return
	}
	if r.Header.Get("Idempotency-Key") == "" {
		writeError(w, r, &backlog.Error{Code: "idempotency_key_required", Status: 400, Message: "Idempotency-Key required"})
		return
	}
	var input backlog.BulkInput
	if err := decode(r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	if err := a.service.Bulk(r.Context(), p.TenantID, chi.URLParam(r, "projectId"), p.Kind+":"+p.ID, r.Header.Get("Idempotency-Key"), actor(p), input); err != nil {
		writeError(w, r, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"updated": len(input.TicketIDs)})
}
func (a *api) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	p, ok := authorize(r, "tickets:comment")
	if !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "upload not allowed"})
		return
	}
	if err := r.ParseMultipartForm(attachments.MaxSize + 1); err != nil {
		writeError(w, r, &backlog.Error{Code: "invalid_upload", Status: 400, Message: "invalid multipart upload"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, &backlog.Error{Code: "invalid_upload", Status: 400, Message: "file required"})
		return
	}
	defer file.Close()
	meta, err := a.attachments.Upload(r.Context(), p.TenantID, chi.URLParam(r, "projectId"), chi.URLParam(r, "ticketId"), header.Filename, file)
	if err != nil {
		writeError(w, r, &backlog.Error{Code: "invalid_upload", Status: 400, Message: err.Error()})
		return
	}
	jsonResponse(w, 201, meta)
}
func (a *api) downloadAttachment(w http.ResponseWriter, r *http.Request) {
	p, ok := authorize(r, "tickets:read")
	if !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "download not allowed"})
		return
	}
	file, meta, err := a.attachments.Open(r.Context(), p.TenantID, chi.URLParam(r, "projectId"), chi.URLParam(r, "attachmentId"))
	if err != nil {
		writeError(w, r, backlog.ErrNotFound)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", meta.MediaType)
	w.Header().Set("Content-Disposition", attachments.Disposition(meta.Name, meta.MediaType))
	w.Header().Set("Content-Length", strconv.FormatInt(meta.Size, 10))
	_, _ = io.Copy(w, file)
}

func (a *api) get(w http.ResponseWriter, r *http.Request) {
	p, ok := authorize(r, "tickets:read")
	if !ok {
		writeError(w, r, &backlog.Error{Code: "unauthorized", Status: 401, Message: "authentication required"})
		return
	}
	project := chi.URLParam(r, "projectId")
	if p.ProjectID != "" && p.ProjectID != project {
		writeError(w, r, backlog.ErrNotFound)
		return
	}
	detail, err := a.service.Detail(r.Context(), p.TenantID, project, chi.URLParam(r, "ticketId"), actor(p))
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", detail.Version))
	jsonResponse(w, 200, detail)
}
func (a *api) update(w http.ResponseWriter, r *http.Request) {
	p, ok := authorize(r, "")
	if !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "update not allowed"})
		return
	}
	version, err := backlog.ParseVersion(r.Header.Get("If-Match"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input backlog.UpdateInput
	if err = decode(r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	ticket, err := a.service.Update(r.Context(), p.TenantID, chi.URLParam(r, "projectId"), chi.URLParam(r, "ticketId"), version, actor(p), input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", ticket.Version))
	jsonResponse(w, 200, ticket)
}
func (a *api) deleteTicket(w http.ResponseWriter, r *http.Request)  { a.deleted(w, r, false) }
func (a *api) restoreTicket(w http.ResponseWriter, r *http.Request) { a.deleted(w, r, true) }
func (a *api) deleted(w http.ResponseWriter, r *http.Request, restore bool) {
	p, ok := authorize(r, "")
	if !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "operation not allowed"})
		return
	}
	version, err := backlog.ParseVersion(r.Header.Get("If-Match"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var ticket backlog.Ticket
	if restore {
		ticket, err = a.service.Restore(r.Context(), p.TenantID, chi.URLParam(r, "projectId"), chi.URLParam(r, "ticketId"), version, actor(p))
	} else {
		ticket, err = a.service.Delete(r.Context(), p.TenantID, chi.URLParam(r, "projectId"), chi.URLParam(r, "ticketId"), version, actor(p))
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", ticket.Version))
	jsonResponse(w, 200, ticket)
}
func (a *api) transition(w http.ResponseWriter, r *http.Request) {
	p, ok := authorize(r, "tickets:complete")
	if !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "transition not allowed"})
		return
	}
	version, err := backlog.ParseVersion(r.Header.Get("If-Match"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	var input backlog.TransitionInput
	if err = decode(r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if p.Kind == "agent" && key == "" {
		writeError(w, r, &backlog.Error{Code: "idempotency_key_required", Status: 400, Message: "Idempotency-Key required"})
		return
	}
	ticket, err := a.service.Transition(r.Context(), p.TenantID, chi.URLParam(r, "projectId"), chi.URLParam(r, "ticketId"), version, p.Kind+":"+p.ID, key, actor(p), input)
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", ticket.Version))
	jsonResponse(w, 200, ticket)
}
func (a *api) claim(w http.ResponseWriter, r *http.Request) {
	p, ok := authorize(r, "tickets:claim")
	if !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "claim not allowed"})
		return
	}
	if r.Header.Get("Idempotency-Key") == "" {
		writeError(w, r, &backlog.Error{Code: "idempotency_key_required", Status: 400, Message: "Idempotency-Key required"})
		return
	}
	var input struct {
		TicketIDs []string `json:"ticketIds"`
	}
	if err := decode(r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	id, err := a.service.Claim(r.Context(), p.TenantID, chi.URLParam(r, "projectId"), p.Kind+":"+p.ID, r.Header.Get("Idempotency-Key"), input.TicketIDs, actor(p))
	if err != nil {
		writeError(w, r, err)
		return
	}
	jsonResponse(w, 201, map[string]string{"batchId": id})
}
func (a *api) projectIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.FromContext(r.Context())
		if !ok || principal.Kind == "agent" {
			next.ServeHTTP(w, r)
			return
		}
		if principal.Issuer == "" && principal.TenantID != "" {
			if principal.ProjectID != chi.URLParam(r, "projectId") {
				writeError(w, r, backlog.ErrNotFound)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		projectID := chi.URLParam(r, "projectId")
		var userID, tenantID, resolvedProjectID, role string
		err := a.service.DB.QueryRow(r.Context(), `SELECT user_id,tenant_id,project_id,role FROM resolve_project_identity($1,$2,$3)`, principal.Issuer, principal.Subject, projectID).Scan(&userID, &tenantID, &resolvedProjectID, &role)
		if err != nil {
			writeError(w, r, backlog.ErrNotFound)
			return
		}
		principal.ID, principal.TenantID, principal.ProjectID, principal.Role = userID, tenantID, resolvedProjectID, role
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), principal)))
	})
}

func (a *api) dashboardCors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == a.webPublicURL {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type,Idempotency-Key,If-Match")
			if r.Method == http.MethodOptions {
				w.WriteHeader(204)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (a *api) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		projectID := chi.URLParam(r, "projectId")
		var allowed bool
		_ = a.service.DB.QueryRow(r.Context(), `SELECT lookup_project_id_origin($1,$2)`, projectID, origin).Scan(&allowed)
		if !allowed {
			writeError(w, r, &backlog.Error{Code: "origin_forbidden", Status: 403, Message: "origin not allowed"})
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type,Idempotency-Key,If-Match")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func authorize(r *http.Request, scope string) (auth.Principal, bool) {
	p, ok := auth.FromContext(r.Context())
	if !ok {
		return p, false
	}
	if p.Kind == "agent" && !p.Scopes[scope] {
		return p, false
	}
	return p, true
}
func actor(p auth.Principal) backlog.Actor {
	return backlog.Actor{Kind: p.Kind, ID: p.ID, Name: p.Name, Role: p.Role, Scopes: p.Scopes}
}
func decode(r *http.Request, target any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return &backlog.Error{Code: "content_type", Status: 415, Message: "application/json required"}
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return &backlog.Error{Code: "invalid_json", Status: 400, Message: "invalid request body"}
	}
	return nil
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status := 500
	code := "internal_error"
	message := "internal server error"
	var domain *backlog.Error
	if errors.As(err, &domain) {
		status = domain.Status
		code = domain.Code
		message = domain.Message
	}
	id := middleware.GetReqID(r.Context())
	if id == "" {
		var b [8]byte
		_, _ = rand.Read(b[:])
		id = hex.EncodeToString(b[:])
	}
	if status >= 500 {
		slog.Error("request failed", "requestId", id, "error", err)
	}
	jsonResponse(w, status, errorBody{Code: code, Message: message, RequestID: id})
}
func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fatal(err error) { slog.Error("fatal", "error", err); os.Exit(1) }
