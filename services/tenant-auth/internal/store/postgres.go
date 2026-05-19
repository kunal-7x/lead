package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lead/libs/go/pgjson"
	"github.com/lead/services/tenant-auth/internal/model"
)

// PG is a PostgreSQL-backed Store.
type PG struct {
	pool *pgxpool.Pool
}

type PostgresStore struct {
	db *pgxpool.Pool
	*PG
}

var _ Store = (*PostgresStore)(nil)

func NewPostgres(dsn string) (*PostgresStore, error) {
	pool, err := pgjson.OpenPool(context.Background(), dsn, "tenant_auth")
	if err != nil {
		return nil, err
	}
	return &PostgresStore{db: pool, PG: NewPG(pool)}, nil
}

func (p *PostgresStore) Close() {
	p.db.Close()
}

func NewPG(pool *pgxpool.Pool) *PG {
	return &PG{pool: pool}
}

// setRLS injects tenant/user context for RLS policies.
func (p *PG) setRLS(ctx context.Context, tenantID, userID string) error {
	_, err := p.pool.Exec(ctx,
		fmt.Sprintf("SET LOCAL app.current_tenant_id = '%s'; SET LOCAL app.current_user_id = '%s'", tenantID, userID))
	return err
}

func (p *PG) CreateTenant(ctx context.Context, name, region string) (*model.Tenant, error) {
	id := uuid.NewString()
	now := time.Now()
	_, err := p.pool.Exec(ctx,
		`INSERT INTO tenants (id, name, region, status, created_at, updated_at)
		 VALUES ($1, $2, $3, 'active', $4, $4)`,
		id, name, region, now)
	if err != nil {
		return nil, fmt.Errorf("create tenant: %w", err)
	}
	return &model.Tenant{ID: id, Name: name, Region: region, Status: model.TenantStatusActive, CreatedAt: now, UpdatedAt: now}, nil
}

func (p *PG) GetTenant(ctx context.Context, tenantID string) (*model.Tenant, error) {
	var t model.Tenant
	err := p.pool.QueryRow(ctx,
		`SELECT id, name, region, status, created_at, updated_at FROM tenants WHERE id = $1 AND status != 'deleted'`,
		tenantID).Scan(&t.ID, &t.Name, &t.Region, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get tenant: %w", err)
	}
	return &t, nil
}

func (p *PG) UpdateTenant(ctx context.Context, tenantID, name string) (*model.Tenant, error) {
	now := time.Now()
	_, err := p.pool.Exec(ctx,
		`UPDATE tenants SET name = $2, updated_at = $3 WHERE id = $1`, tenantID, name, now)
	if err != nil {
		return nil, fmt.Errorf("update tenant: %w", err)
	}
	return p.GetTenant(ctx, tenantID)
}

func (p *PG) SuspendTenant(ctx context.Context, tenantID string) error {
	_, err := p.pool.Exec(ctx,
		`UPDATE tenants SET status = 'suspended', updated_at = NOW() WHERE id = $1`, tenantID)
	return err
}

func (p *PG) CreateUser(ctx context.Context, tenantID, email, displayName, passwordHash string) (*model.User, error) {
	id := uuid.NewString()
	now := time.Now()
	_, err := p.pool.Exec(ctx,
		`INSERT INTO tenant_users (id, tenant_id, email, display_name, password_hash, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, 'active', $6, $6)`,
		id, tenantID, email, displayName, passwordHash, now)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return &model.User{ID: id, TenantID: tenantID, Email: email, DisplayName: displayName, PasswordHash: passwordHash,
		Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}, nil
}

func (p *PG) GetUserByEmail(ctx context.Context, tenantID, email string) (*model.User, error) {
	var u model.User
	err := p.pool.QueryRow(ctx,
		`SELECT id, tenant_id, email, display_name, password_hash, COALESCE(totp_secret,''), totp_enabled,
		        COALESCE(refresh_token_hash,''), status, created_at, updated_at
		 FROM tenant_users WHERE tenant_id = $1 AND email = $2 AND status = 'active'`,
		tenantID, email).Scan(&u.ID, &u.TenantID, &u.Email, &u.DisplayName, &u.PasswordHash,
		&u.TOTPSecret, &u.TOTPEnabled, &u.RefreshTokenHash, &u.Status, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get user by email: %w", err)
	}
	return &u, nil
}

func (p *PG) GetUserByID(ctx context.Context, tenantID, userID string) (*model.User, error) {
	var u model.User
	err := p.pool.QueryRow(ctx,
		`SELECT id, tenant_id, email, display_name, password_hash, COALESCE(totp_secret,''), totp_enabled,
		        COALESCE(refresh_token_hash,''), status, created_at, updated_at
		 FROM tenant_users WHERE tenant_id = $1 AND id = $2`,
		tenantID, userID).Scan(&u.ID, &u.TenantID, &u.Email, &u.DisplayName, &u.PasswordHash,
		&u.TOTPSecret, &u.TOTPEnabled, &u.RefreshTokenHash, &u.Status, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return &u, nil
}

func (p *PG) UpdateUserTOTP(ctx context.Context, tenantID, userID, secret string, enabled bool) error {
	_, err := p.pool.Exec(ctx,
		`UPDATE tenant_users SET totp_secret = $3, totp_enabled = $4, updated_at = NOW()
		 WHERE tenant_id = $1 AND id = $2`, tenantID, userID, secret, enabled)
	return err
}

func (p *PG) SetRefreshToken(ctx context.Context, tenantID, userID, tokenHash string) error {
	_, err := p.pool.Exec(ctx,
		`UPDATE tenant_users SET refresh_token_hash = $3, updated_at = NOW()
		 WHERE tenant_id = $1 AND id = $2`, tenantID, userID, tokenHash)
	return err
}

func (p *PG) GetUserByRefreshToken(ctx context.Context, tenantID, tokenHash string) (*model.User, error) {
	var u model.User
	err := p.pool.QueryRow(ctx,
		`SELECT id, tenant_id, email, display_name, password_hash, COALESCE(totp_secret,''), totp_enabled,
		        COALESCE(refresh_token_hash,''), status, created_at, updated_at
		 FROM tenant_users WHERE tenant_id = $1 AND refresh_token_hash = $2 AND status = 'active'`,
		tenantID, tokenHash).Scan(&u.ID, &u.TenantID, &u.Email, &u.DisplayName, &u.PasswordHash,
		&u.TOTPSecret, &u.TOTPEnabled, &u.RefreshTokenHash, &u.Status, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get user by refresh token: %w", err)
	}
	return &u, nil
}

func (p *PG) ClearRefreshToken(ctx context.Context, tenantID, userID string) error {
	_, err := p.pool.Exec(ctx,
		`UPDATE tenant_users SET refresh_token_hash = NULL, updated_at = NOW()
		 WHERE tenant_id = $1 AND id = $2`, tenantID, userID)
	return err
}

func (p *PG) CreateRole(ctx context.Context, tenantID, name string, permissions []string) (*model.Role, error) {
	id := uuid.NewString()
	now := time.Now()
	// Store permissions as JSON array
	_, err := p.pool.Exec(ctx,
		`INSERT INTO roles (id, tenant_id, name, permissions, created_at) VALUES ($1, $2, $3, $4, $5)`,
		id, tenantID, name, permissions, now)
	if err != nil {
		return nil, fmt.Errorf("create role: %w", err)
	}
	return &model.Role{ID: id, TenantID: tenantID, Name: name, Permissions: permissions, CreatedAt: now}, nil
}

func (p *PG) ListRoles(ctx context.Context, tenantID string) ([]*model.Role, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, tenant_id, name, permissions, created_at FROM roles WHERE tenant_id = $1`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()
	var roles []*model.Role
	for rows.Next() {
		var r model.Role
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Name, &r.Permissions, &r.CreatedAt); err != nil {
			return nil, err
		}
		roles = append(roles, &r)
	}
	return roles, nil
}

func (p *PG) GetRole(ctx context.Context, tenantID, roleID string) (*model.Role, error) {
	var r model.Role
	err := p.pool.QueryRow(ctx,
		`SELECT id, tenant_id, name, permissions, created_at FROM roles WHERE tenant_id = $1 AND id = $2`,
		tenantID, roleID).Scan(&r.ID, &r.TenantID, &r.Name, &r.Permissions, &r.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get role: %w", err)
	}
	return &r, nil
}

func (p *PG) AssignRole(ctx context.Context, tenantID, userID, roleID string) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO user_tenant_roles (user_id, tenant_id, role_id, created_at)
		 VALUES ($1, $2, $3, NOW()) ON CONFLICT DO NOTHING`, userID, tenantID, roleID)
	return err
}

func (p *PG) GetUserRoles(ctx context.Context, tenantID, userID string) ([]*model.Role, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT r.id, r.tenant_id, r.name, r.permissions, r.created_at
		 FROM roles r
		 JOIN user_tenant_roles utr ON utr.role_id = r.id
		 WHERE utr.tenant_id = $1 AND utr.user_id = $2`, tenantID, userID)
	if err != nil {
		return nil, fmt.Errorf("get user roles: %w", err)
	}
	defer rows.Close()
	var roles []*model.Role
	for rows.Next() {
		var r model.Role
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Name, &r.Permissions, &r.CreatedAt); err != nil {
			return nil, err
		}
		roles = append(roles, &r)
	}
	return roles, nil
}

func (p *PG) CheckPermission(ctx context.Context, tenantID, userID, permission string) (bool, error) {
	roles, err := p.GetUserRoles(ctx, tenantID, userID)
	if err != nil {
		return false, err
	}
	for _, r := range roles {
		for _, perm := range r.Permissions {
			if perm == permission || perm == "*" {
				return true, nil
			}
		}
	}
	return false, nil
}

func (p *PG) CreateAPIKey(ctx context.Context, tenantID, userID, name, keyHash, keyPrefix string) (*model.APIKey, error) {
	id := uuid.NewString()
	now := time.Now()
	_, err := p.pool.Exec(ctx,
		`INSERT INTO api_keys (id, tenant_id, user_id, name, key_hash, key_prefix, revoked, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, FALSE, $7)`,
		id, tenantID, userID, name, keyHash, keyPrefix, now)
	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}
	return &model.APIKey{ID: id, TenantID: tenantID, UserID: userID, Name: name, KeyHash: keyHash, KeyPrefix: keyPrefix, CreatedAt: now}, nil
}

func (p *PG) RevokeAPIKey(ctx context.Context, tenantID, keyID string) error {
	_, err := p.pool.Exec(ctx,
		`UPDATE api_keys SET revoked = TRUE WHERE tenant_id = $1 AND id = $2`, tenantID, keyID)
	return err
}

func (p *PG) WriteAuditLog(ctx context.Context, entry *model.AuditLog) error {
	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}
	if entry.OccurredAt.IsZero() {
		entry.OccurredAt = time.Now()
	}
	_, err := p.pool.Exec(ctx,
		`INSERT INTO audit_logs (id, tenant_id, actor_id, actor_email, action, resource, resource_id,
		  before_state, after_state, ip_address, occurred_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		entry.ID, entry.TenantID, entry.ActorID, entry.ActorEmail, entry.Action,
		entry.Resource, entry.ResourceID, entry.Before, entry.After, entry.IPAddress, entry.OccurredAt)
	return err
}

func (p *PG) QueryAuditLogs(ctx context.Context, tenantID string, limit, offset int) ([]*model.AuditLog, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := p.pool.Query(ctx,
		`SELECT id, tenant_id, actor_id, actor_email, action, resource, resource_id,
		        before_state, after_state, ip_address, occurred_at
		 FROM audit_logs WHERE tenant_id = $1
		 ORDER BY occurred_at DESC LIMIT $2 OFFSET $3`,
		tenantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query audit logs: %w", err)
	}
	defer rows.Close()
	var logs []*model.AuditLog
	for rows.Next() {
		var a model.AuditLog
		if err := rows.Scan(&a.ID, &a.TenantID, &a.ActorID, &a.ActorEmail, &a.Action,
			&a.Resource, &a.ResourceID, &a.Before, &a.After, &a.IPAddress, &a.OccurredAt); err != nil {
			return nil, err
		}
		logs = append(logs, &a)
	}
	return logs, nil
}
