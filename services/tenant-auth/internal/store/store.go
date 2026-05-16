package store

import (
	"context"

	"github.com/lead/services/tenant-auth/internal/model"
)

// Store is the data-access interface for the tenant-auth service.
type Store interface {
	// Tenant management
	CreateTenant(ctx context.Context, name, region string) (*model.Tenant, error)
	GetTenant(ctx context.Context, tenantID string) (*model.Tenant, error)
	UpdateTenant(ctx context.Context, tenantID, name string) (*model.Tenant, error)
	SuspendTenant(ctx context.Context, tenantID string) error

	// User management
	CreateUser(ctx context.Context, tenantID, email, displayName, passwordHash string) (*model.User, error)
	GetUserByEmail(ctx context.Context, tenantID, email string) (*model.User, error)
	GetUserByID(ctx context.Context, tenantID, userID string) (*model.User, error)
	UpdateUserTOTP(ctx context.Context, tenantID, userID, secret string, enabled bool) error
	SetRefreshToken(ctx context.Context, tenantID, userID, tokenHash string) error
	GetUserByRefreshToken(ctx context.Context, tenantID, tokenHash string) (*model.User, error)
	ClearRefreshToken(ctx context.Context, tenantID, userID string) error

	// Roles & permissions
	CreateRole(ctx context.Context, tenantID, name string, permissions []string) (*model.Role, error)
	ListRoles(ctx context.Context, tenantID string) ([]*model.Role, error)
	GetRole(ctx context.Context, tenantID, roleID string) (*model.Role, error)
	AssignRole(ctx context.Context, tenantID, userID, roleID string) error
	GetUserRoles(ctx context.Context, tenantID, userID string) ([]*model.Role, error)
	CheckPermission(ctx context.Context, tenantID, userID, permission string) (bool, error)

	// API keys
	CreateAPIKey(ctx context.Context, tenantID, userID, name, keyHash, keyPrefix string) (*model.APIKey, error)
	RevokeAPIKey(ctx context.Context, tenantID, keyID string) error

	// Audit log
	WriteAuditLog(ctx context.Context, entry *model.AuditLog) error
	QueryAuditLogs(ctx context.Context, tenantID string, limit, offset int) ([]*model.AuditLog, error)
}
