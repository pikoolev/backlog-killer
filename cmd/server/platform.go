package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/pikoolev/backlog-killer/internal/auth"
	"github.com/pikoolev/backlog-killer/internal/backlog"
)

func (a *api) requirePlatformAdmin(r *http.Request) (auth.Principal, bool) {
	p, ok := auth.FromContext(r.Context())
	return p, ok && p.Source == "oidc" && a.platformAdminSubjects[p.Issuer+"|"+p.Subject]
}
func (a *api) platformCatalog(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requirePlatformAdmin(r); !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "platform administrator required"})
		return
	}
	items, err := a.platform.Catalog(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"items": items})
}
func (a *api) platformUpdateTenant(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requirePlatformAdmin(r)
	if !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "platform administrator required"})
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	tenant := chi.URLParam(r, "tenantId")
	if err := a.platform.UpdateTenant(r.Context(), tenant, in.Name); err != nil {
		writeError(w, r, err)
		return
	}
	_ = a.platform.Audit(r.Context(), p.Issuer, p.Subject, "update_tenant", &tenant, nil, nil, in)
	w.WriteHeader(http.StatusNoContent)
}
func (a *api) platformCreateProject(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requirePlatformAdmin(r)
	if !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "platform administrator required"})
		return
	}
	var in struct {
		Name       string `json:"name"`
		ProjectKey string `json:"projectKey"`
	}
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	tenant := chi.URLParam(r, "tenantId")
	id, err := a.platform.CreateProject(r.Context(), tenant, in.Name, in.ProjectKey)
	if err != nil {
		writeError(w, r, err)
		return
	}
	_ = a.platform.Audit(r.Context(), p.Issuer, p.Subject, "create_project", &tenant, &id, nil, in)
	jsonResponse(w, 201, map[string]string{"id": id})
}
func (a *api) platformUpdateProject(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requirePlatformAdmin(r)
	if !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "platform administrator required"})
		return
	}
	var in struct {
		Name       string `json:"name"`
		ProjectKey string `json:"projectKey"`
	}
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	project := chi.URLParam(r, "platformProjectId")
	if err := a.platform.UpdateProject(r.Context(), project, in.Name, in.ProjectKey); err != nil {
		writeError(w, r, err)
		return
	}
	_ = a.platform.Audit(r.Context(), p.Issuer, p.Subject, "update_project", nil, &project, nil, in)
	w.WriteHeader(http.StatusNoContent)
}
func (a *api) platformUpsertUser(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requirePlatformAdmin(r)
	if !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "platform administrator required"})
		return
	}
	var in struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	identity, err := a.identities.Create(r.Context(), in.Username, in.Email, in.Password)
	if err != nil {
		writeError(w, r, err)
		return
	}
	tenant := chi.URLParam(r, "tenantId")
	id, err := a.platform.UpsertUser(r.Context(), tenant, a.oidcPublicIssuer, identity.ID, in.Email, in.Role)
	if err != nil {
		_ = a.identities.Delete(r.Context(), identity.ID)
		writeError(w, r, err)
		return
	}
	_ = a.platform.Audit(r.Context(), p.Issuer, p.Subject, "create_tenant_user", &tenant, nil, &id, map[string]string{"email": in.Email, "role": in.Role})
	jsonResponse(w, 201, map[string]string{"id": id, "subject": identity.ID})
}
func (a *api) platformRemoveUser(w http.ResponseWriter, r *http.Request) {
	p, ok := a.requirePlatformAdmin(r)
	if !ok {
		writeError(w, r, &backlog.Error{Code: "forbidden", Status: 403, Message: "platform administrator required"})
		return
	}
	tenant, user := chi.URLParam(r, "tenantId"), chi.URLParam(r, "userId")
	if err := a.platform.RemoveUser(r.Context(), tenant, user); err != nil {
		writeError(w, r, err)
		return
	}
	_ = a.platform.Audit(r.Context(), p.Issuer, p.Subject, "remove_tenant_user", &tenant, nil, &user, map[string]string{})
	w.WriteHeader(http.StatusNoContent)
}
