package model

import "time"

type TenantStatus string

const (
	TenantStatusActive    TenantStatus = "active"
	TenantStatusSuspended TenantStatus = "suspended"
	TenantStatusDeleted   TenantStatus = "deleted"
)

type Tenant struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Region    string       `json:"region"`
	Status    TenantStatus `json:"status"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type UserStatus string

const (
	UserStatusActive    UserStatus = "active"
	UserStatusSuspended UserStatus = "suspended"
	UserStatusDeleted   UserStatus = "deleted"
)

type User struct {
	ID               string     `json:"id"`
	TenantID         string     `json:"tenant_id"`
	Email            string     `json:"email"`
	DisplayName      string     `json:"display_name"`
	PasswordHash     string     `json:"-"`
	TOTPSecret       string     `json:"-"`
	TOTPEnabled      bool       `json:"totp_enabled"`
	RefreshTokenHash string     `json:"-"`
	Status           UserStatus `json:"status"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type Role struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	Name        string    `json:"name"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
}

type UserTenantRole struct {
	UserID    string    `json:"user_id"`
	TenantID  string    `json:"tenant_id"`
	RoleID    string    `json:"role_id"`
	CreatedAt time.Time `json:"created_at"`
}

type APIKey struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`
	KeyHash   string    `json:"-"`
	KeyPrefix string    `json:"key_prefix"`
	Revoked   bool      `json:"revoked"`
	CreatedAt time.Time `json:"created_at"`
}

type AuditLog struct {
	ID         string         `json:"id"`
	TenantID   string         `json:"tenant_id"`
	ActorID    string         `json:"actor_id"`
	ActorEmail string         `json:"actor_email"`
	Action     string         `json:"action"`
	Resource   string         `json:"resource"`
	ResourceID string         `json:"resource_id"`
	Before     map[string]any `json:"before,omitempty"`
	After      map[string]any `json:"after,omitempty"`
	IPAddress  string         `json:"ip_address"`
	OccurredAt time.Time      `json:"occurred_at"`
}
