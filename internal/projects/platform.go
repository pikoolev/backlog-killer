package projects

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PlatformAdmin struct{ DB *pgxpool.Pool }

type PlatformTenant struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Projects []PlatformProject `json:"projects"`
	Users    []PlatformUser    `json:"users"`
}
type PlatformProject struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ProjectKey string `json:"projectKey"`
}
type PlatformUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}
type MaskedProject struct {
	UserID      string `json:"userId"`
	TenantID    string `json:"tenantId"`
	ProjectID   string `json:"projectId"`
	ProjectKey  string `json:"projectKey"`
	ProjectName string `json:"projectName"`
	Role        string `json:"role"`
}

func (s *PlatformAdmin) Catalog(ctx context.Context) ([]PlatformTenant, error) {
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT platform_admin_catalog()`).Scan(&raw); err != nil {
		return nil, err
	}
	var tenants []PlatformTenant
	return tenants, json.Unmarshal(raw, &tenants)
}
func (s *PlatformAdmin) UpdateTenant(ctx context.Context, tenant, name string) error {
	_, err := s.DB.Exec(ctx, `SELECT platform_admin_update_tenant($1,$2)`, tenant, name)
	return err
}
func (s *PlatformAdmin) CreateProject(ctx context.Context, tenant, name, key string) (string, error) {
	var ignored, id string
	err := s.DB.QueryRow(ctx, `SELECT tenant_id,project_id FROM platform_admin_create_project($1,$2,$3)`, tenant, name, key).Scan(&ignored, &id)
	return id, err
}
func (s *PlatformAdmin) UpdateProject(ctx context.Context, project, name, key string) error {
	_, err := s.DB.Exec(ctx, `SELECT platform_admin_update_project($1,$2,$3)`, project, name, key)
	return err
}
func (s *PlatformAdmin) UpsertUser(ctx context.Context, tenant, issuer, subject, email, role string) (string, error) {
	if role != "owner" && role != "admin" && role != "member" {
		return "", errors.New("invalid tenant role")
	}
	var id string
	err := s.DB.QueryRow(ctx, `SELECT platform_admin_upsert_user($1,$2,$3,$4,$5::tenant_role)`, tenant, issuer, subject, email, role).Scan(&id)
	return id, err
}
func (s *PlatformAdmin) RemoveUser(ctx context.Context, tenant, user string) error {
	_, err := s.DB.Exec(ctx, `SELECT platform_admin_remove_user($1,$2)`, tenant, user)
	return err
}
func (s *PlatformAdmin) Mask(ctx context.Context, project, user string) (MaskedProject, error) {
	var out MaskedProject
	var target *uuid.UUID
	if user != "" {
		parsed, err := uuid.Parse(user)
		if err != nil {
			return out, err
		}
		target = &parsed
	}
	err := s.DB.QueryRow(ctx, `SELECT user_id,tenant_id,project_id,project_key,project_name,role FROM platform_admin_mask_project($1,$2)`, project, target).Scan(&out.UserID, &out.TenantID, &out.ProjectID, &out.ProjectKey, &out.ProjectName, &out.Role)
	return out, err
}
func (s *PlatformAdmin) Audit(ctx context.Context, issuer, subject, action string, tenant, project, user *string, metadata any) error {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, `SELECT platform_admin_log($1,$2,$3,$4,$5,$6,$7)`, issuer, subject, action, tenant, project, user, raw)
	return err
}
