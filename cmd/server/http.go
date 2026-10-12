package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/pikoolev/backlog-killer/internal/auth"
	"github.com/pikoolev/backlog-killer/internal/backlog"
)

func (a *api) projectIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := auth.FromContext(r.Context())
		if !ok || principal.Kind == "agent" {
			next.ServeHTTP(w, r)
			return
		}
		projectID := chi.URLParam(r, "projectId")
		if principal.Source == "oidc" && a.platformAdminSubjects[principal.Issuer+"|"+principal.Subject] && r.Header.Get("X-Platform-Mask") == "true" {
			masked, err := a.platform.Mask(r.Context(), projectID, r.Header.Get("X-Platform-User"))
			if err != nil {
				writeError(w, r, backlog.ErrNotFound)
				return
			}
			principal.ID, principal.TenantID, principal.ProjectID, principal.Role = masked.UserID, masked.TenantID, masked.ProjectID, masked.Role
			_ = a.platform.Audit(r.Context(), principal.Issuer, principal.Subject, "mask_project_request", &masked.TenantID, &masked.ProjectID, &masked.UserID, map[string]string{"method": r.Method, "path": r.URL.Path})
			next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), principal)))
			return
		}
		if principal.Source == "embed" {
			if principal.ProjectID != projectID {
				writeError(w, r, backlog.ErrNotFound)
				return
			}
			var userID, tenantID, resolvedProjectID, role string
			err := a.service.DB.QueryRow(r.Context(), `SELECT user_id,tenant_id,project_id,role FROM resolve_project_identity($1,$2,$3)`, a.oidcPublicIssuer, principal.Subject, projectID).Scan(&userID, &tenantID, &resolvedProjectID, &role)
			if err != nil || tenantID != principal.TenantID {
				writeError(w, r, backlog.ErrNotFound)
				return
			}
			principal.ID, principal.TenantID, principal.ProjectID, principal.Role = userID, tenantID, resolvedProjectID, role
			next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), principal)))
			return
		}
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
			w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type,Idempotency-Key,If-Match,X-Platform-Mask,X-Platform-User")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
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
		if origin != a.webPublicURL && origin != "chrome-extension://nkkilejiaedcndfekaibdagbnodglnoe" {
			projectID := chi.URLParam(r, "projectId")
			var allowed bool
			_ = a.service.DB.QueryRow(r.Context(), `SELECT lookup_project_id_origin($1,$2)`, projectID, origin).Scan(&allowed)
			if !allowed {
				writeError(w, r, &backlog.Error{Code: "origin_forbidden", Status: http.StatusForbidden, Message: "origin not allowed"})
				return
			}
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
		return &backlog.Error{Code: "content_type", Status: http.StatusUnsupportedMediaType, Message: "application/json required"}
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return &backlog.Error{Code: "invalid_json", Status: http.StatusBadRequest, Message: "invalid request body"}
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
	status := http.StatusInternalServerError
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
	if status >= http.StatusInternalServerError {
		slog.Error("request failed", "requestId", id, "error", err)
	}
	jsonResponse(w, status, errorBody{Code: code, Message: message, RequestID: id})
}

func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
