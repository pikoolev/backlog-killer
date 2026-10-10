package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := os.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		var applied bool
		if err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)", entry.Name()).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		sql, err := os.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(sql)); err == nil {
			_, err = tx.Exec(ctx, "INSERT INTO schema_migrations(version) VALUES($1)", entry.Name())
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", entry.Name(), err)
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

func SeedLocalDemo(ctx context.Context, pool *pgxpool.Pool) error {
	sql, err := os.ReadFile("infra/local-seed.sql")
	if err != nil {
		return fmt.Errorf("read local demo seed: %w", err)
	}
	if _, err = pool.Exec(ctx, string(sql)); err != nil {
		return fmt.Errorf("apply local demo seed: %w", err)
	}
	return nil
}

type OnPremBootstrap struct{ TenantName, ProjectName, ProjectKey, Issuer, OwnerSubject, OwnerEmail, ProjectOrigin string }

func BootstrapOnPrem(ctx context.Context, pool *pgxpool.Pool, in OnPremBootstrap) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(913704221)`); err != nil {
		return err
	}
	var tenant, user, project string
	if err = tx.QueryRow(ctx, `SELECT id FROM tenants ORDER BY created_at,id LIMIT 1`).Scan(&tenant); errors.Is(err, pgx.ErrNoRows) {
		if err = tx.QueryRow(ctx, `INSERT INTO tenants(name) VALUES($1) RETURNING id`, in.TenantName).Scan(&tenant); err != nil {
			return err
		}
	} else if err != nil {
		return fmt.Errorf("resolve on-prem tenant: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE tenants SET name=$1 WHERE id=$2`, in.TenantName, tenant); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `INSERT INTO users(issuer,subject,email,verified_email) VALUES($1,$2,$3,true) ON CONFLICT(issuer,subject) DO UPDATE SET email=EXCLUDED.email,verified_email=true RETURNING id`, in.Issuer, in.OwnerSubject, in.OwnerEmail).Scan(&user); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO tenant_memberships(tenant_id,user_id,is_owner) VALUES($1,$2,true) ON CONFLICT(tenant_id,user_id) DO UPDATE SET is_owner=true`, tenant, user); err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `INSERT INTO projects(tenant_id,project_key,name) VALUES($1,$2,$3) ON CONFLICT(project_key) DO UPDATE SET name=EXCLUDED.name RETURNING id`, tenant, in.ProjectKey, in.ProjectName).Scan(&project); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO project_memberships(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'admin') ON CONFLICT(tenant_id,project_id,user_id) DO UPDATE SET role='admin'`, tenant, project, user); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO project_origins(tenant_id,project_id,origin) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, tenant, project, in.ProjectOrigin); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ValidateRuntime rejects privileged or migration-owner credentials before traffic is served.
func ValidateRuntime(ctx context.Context, pool *pgxpool.Pool) error {
	var role string
	var superuser, bypassRLS bool
	var ownedTables int
	if err := pool.QueryRow(ctx, `SELECT current_user,r.rolsuper,r.rolbypassrls,(SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN ('r','p') AND pg_get_userbyid(c.relowner)=current_user) FROM pg_roles r WHERE r.rolname=current_user`).Scan(&role, &superuser, &bypassRLS, &ownedTables); err != nil {
		return fmt.Errorf("inspect runtime database role: %w", err)
	}
	if superuser || bypassRLS || ownedTables != 0 {
		return fmt.Errorf("runtime database role %q must be non-superuser, NOBYPASSRLS, and own no application tables", role)
	}
	return nil
}

func WithTenant(ctx context.Context, pool *pgxpool.Pool, tenantID string, fn func(context.Context, pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenantID); err != nil {
		return err
	}
	if err = fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
