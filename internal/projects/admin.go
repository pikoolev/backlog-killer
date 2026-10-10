package projects

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pikoolev/backlog-killer/internal/store"
)

type Admin struct{ DB *pgxpool.Pool }
type Origin struct {
	Origin string `json:"origin"`
}
type Member struct {
	UserID string `json:"userId"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	Owner  bool   `json:"owner"`
}
type Project struct {
	TenantID string   `json:"tenantId"`
	ID       string   `json:"id"`
	Key      string   `json:"projectKey"`
	Name     string   `json:"name"`
	Role     string   `json:"role"`
	Origins  []Origin `json:"origins"`
	Members  []Member `json:"members"`
}

func ValidateOrigin(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.Contains(u.Host, "*") {
		return errors.New("exact http(s) origin required")
	}
	return nil
}
func (s *Admin) CreateTenant(ctx context.Context, issuer, subject, email, name, projectName, key string) (Project, error) {
	var p Project
	if strings.TrimSpace(name) == "" || strings.TrimSpace(projectName) == "" || strings.TrimSpace(key) == "" {
		return p, errors.New("tenant, project, and key are required")
	}
	err := s.DB.QueryRow(ctx, `SELECT tenant_id,project_id FROM create_tenant_with_owner($1,$2,$3,$4,$5,$6)`, issuer, subject, email, name, projectName, key).Scan(&p.TenantID, &p.ID)
	p.Key = key
	p.Name = projectName
	p.Role = "owner"
	p.Origins = []Origin{}
	p.Members = []Member{}
	return p, err
}
func (s *Admin) CreateProject(ctx context.Context, tenant, creator, name, key string) (Project, error) {
	var p Project
	p = Project{TenantID: tenant, ID: uuid.Must(uuid.NewV7()).String(), Key: key, Name: name, Role: "owner", Origins: []Origin{}, Members: []Member{}}
	if name == "" || key == "" {
		return p, errors.New("name and projectKey required")
	}
	err := store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO projects(id,tenant_id,project_key,name) VALUES($1,$2,$3,$4)`, p.ID, tenant, key, name)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `INSERT INTO project_memberships(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'admin')`, tenant, p.ID, creator)
		return e
	})
	return p, err
}
func (s *Admin) Get(ctx context.Context, tenant, project string) (Project, error) {
	var p Project
	p.Origins = []Origin{}
	p.Members = []Member{}
	err := store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		if e := tx.QueryRow(ctx, `SELECT id,tenant_id,project_key,name FROM projects WHERE id=$1`, project).Scan(&p.ID, &p.TenantID, &p.Key, &p.Name); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `SELECT origin FROM project_origins WHERE project_id=$1 ORDER BY origin`, project)
		if e != nil {
			return e
		}
		for rows.Next() {
			var o Origin
			if e = rows.Scan(&o.Origin); e != nil {
				rows.Close()
				return e
			}
			p.Origins = append(p.Origins, o)
		}
		rows.Close()
		rows, e = tx.Query(ctx, `SELECT u.id,COALESCE(u.email,''),pm.role,tm.is_owner FROM project_memberships pm JOIN users u ON u.id=pm.user_id JOIN tenant_memberships tm ON tm.tenant_id=pm.tenant_id AND tm.user_id=pm.user_id WHERE pm.project_id=$1 ORDER BY tm.is_owner DESC,u.email`, project)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var m Member
			if e = rows.Scan(&m.UserID, &m.Email, &m.Role, &m.Owner); e != nil {
				return e
			}
			if m.Owner {
				m.Role = "owner"
			}
			p.Members = append(p.Members, m)
		}
		return rows.Err()
	})
	return p, err
}
func (s *Admin) AddOrigin(ctx context.Context, tenant, project, raw string) error {
	if e := ValidateOrigin(raw); e != nil {
		return e
	}
	return store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO project_origins(tenant_id,project_id,origin) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, tenant, project, raw)
		return e
	})
}
func (s *Admin) RemoveOrigin(ctx context.Context, tenant, project, raw string) error {
	return store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		tag, e := tx.Exec(ctx, `DELETE FROM project_origins WHERE project_id=$1 AND origin=$2`, project, raw)
		if e == nil && tag.RowsAffected() != 1 {
			return pgx.ErrNoRows
		}
		return e
	})
}
func (s *Admin) SetMemberRole(ctx context.Context, tenant, project, user, role string) error {
	if role != "admin" && role != "triager" && role != "reporter" {
		return errors.New("invalid role")
	}
	return store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		tag, e := tx.Exec(ctx, `UPDATE project_memberships SET role=$1 WHERE project_id=$2 AND user_id=$3`, role, project, user)
		if e == nil && tag.RowsAffected() != 1 {
			return pgx.ErrNoRows
		}
		return e
	})
}
func (s *Admin) RemoveMember(ctx context.Context, tenant, project, user string) error {
	return store.WithTenant(ctx, s.DB, tenant, func(ctx context.Context, tx pgx.Tx) error {
		var owner bool
		if e := tx.QueryRow(ctx, `SELECT is_owner FROM tenant_memberships WHERE tenant_id=$1 AND user_id=$2`, tenant, user).Scan(&owner); e != nil {
			return e
		}
		if owner {
			return errors.New("tenant owner cannot be removed")
		}
		tag, e := tx.Exec(ctx, `DELETE FROM project_memberships WHERE project_id=$1 AND user_id=$2`, project, user)
		if e == nil && tag.RowsAffected() != 1 {
			return pgx.ErrNoRows
		}
		return e
	})
}
func (s *Admin) CreateProjectForOwner(ctx context.Context, issuer, subject, tenant, name, key string) (Project, error) {
	var p Project
	if name == "" || key == "" {
		return p, errors.New("name and projectKey required")
	}
	err := s.DB.QueryRow(ctx, `SELECT tenant_id,project_id FROM create_project_for_owner($1,$2,$3,$4,$5)`, issuer, subject, tenant, name, key).Scan(&p.TenantID, &p.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, errors.New("tenant owner required")
	}
	p.Key = key
	p.Name = name
	p.Role = "owner"
	p.Origins = []Origin{}
	p.Members = []Member{}
	return p, err
}
