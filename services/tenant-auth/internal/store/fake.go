package store

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lead/services/tenant-auth/internal/model"
)

// Fake is an in-memory Store implementation for unit tests.
type Fake struct {
	mu       sync.RWMutex
	tenants  map[string]*model.Tenant
	users    map[string]*model.User // key: tenantID+":"+userID
	roles    map[string]*model.Role // key: tenantID+":"+roleID
	userRole map[string][]string    // key: tenantID+":"+userID -> []roleID
	apiKeys  map[string]*model.APIKey
	audit    []*model.AuditLog
	// refresh token index: hash -> userID
	refreshTokens map[string]string
}

func NewFake() *Fake {
	return &Fake{
		tenants:       make(map[string]*model.Tenant),
		users:         make(map[string]*model.User),
		roles:         make(map[string]*model.Role),
		userRole:      make(map[string][]string),
		apiKeys:       make(map[string]*model.APIKey),
		refreshTokens: make(map[string]string),
	}
}

func (f *Fake) CreateTenant(_ context.Context, name, region string) (*model.Tenant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &model.Tenant{
		ID:        uuid.NewString(),
		Name:      name,
		Region:    region,
		Status:    model.TenantStatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	f.tenants[t.ID] = t
	return t, nil
}

func (f *Fake) GetTenant(_ context.Context, tenantID string) (*model.Tenant, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	t, ok := f.tenants[tenantID]
	if !ok {
		return nil, fmt.Errorf("tenant not found")
	}
	copy := *t
	return &copy, nil
}

func (f *Fake) UpdateTenant(_ context.Context, tenantID, name string) (*model.Tenant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tenants[tenantID]
	if !ok {
		return nil, fmt.Errorf("tenant not found")
	}
	t.Name = name
	t.UpdatedAt = time.Now()
	copy := *t
	return &copy, nil
}

func (f *Fake) SuspendTenant(_ context.Context, tenantID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tenants[tenantID]
	if !ok {
		return fmt.Errorf("tenant not found")
	}
	t.Status = model.TenantStatusSuspended
	t.UpdatedAt = time.Now()
	return nil
}

func (f *Fake) CreateUser(_ context.Context, tenantID, email, displayName, passwordHash string) (*model.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u := &model.User{
		ID:           uuid.NewString(),
		TenantID:     tenantID,
		Email:        email,
		DisplayName:  displayName,
		PasswordHash: passwordHash,
		Status:       model.UserStatusActive,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	f.users[tenantID+":"+u.ID] = u
	return u, nil
}

func (f *Fake) GetUserByEmail(_ context.Context, tenantID, email string) (*model.User, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, u := range f.users {
		if u.TenantID == tenantID && u.Email == email {
			copy := *u
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("user not found")
}

func (f *Fake) GetUserByID(_ context.Context, tenantID, userID string) (*model.User, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	u, ok := f.users[tenantID+":"+userID]
	if !ok {
		return nil, fmt.Errorf("user not found")
	}
	copy := *u
	return &copy, nil
}

func (f *Fake) UpdateUserTOTP(_ context.Context, tenantID, userID, secret string, enabled bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[tenantID+":"+userID]
	if !ok {
		return fmt.Errorf("user not found")
	}
	u.TOTPSecret = secret
	u.TOTPEnabled = enabled
	u.UpdatedAt = time.Now()
	return nil
}

func (f *Fake) SetRefreshToken(_ context.Context, tenantID, userID, tokenHash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[tenantID+":"+userID]
	if !ok {
		return fmt.Errorf("user not found")
	}
	// remove old token
	if u.RefreshTokenHash != "" {
		delete(f.refreshTokens, u.RefreshTokenHash)
	}
	u.RefreshTokenHash = tokenHash
	f.refreshTokens[tokenHash] = tenantID + ":" + userID
	return nil
}

func (f *Fake) GetUserByRefreshToken(_ context.Context, tenantID, tokenHash string) (*model.User, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	key, ok := f.refreshTokens[tokenHash]
	if !ok {
		return nil, fmt.Errorf("token not found")
	}
	u, ok := f.users[key]
	if !ok || u.TenantID != tenantID {
		return nil, fmt.Errorf("user not found")
	}
	copy := *u
	return &copy, nil
}

func (f *Fake) ClearRefreshToken(_ context.Context, tenantID, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[tenantID+":"+userID]
	if !ok {
		return fmt.Errorf("user not found")
	}
	delete(f.refreshTokens, u.RefreshTokenHash)
	u.RefreshTokenHash = ""
	return nil
}

func (f *Fake) CreateRole(_ context.Context, tenantID, name string, permissions []string) (*model.Role, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := &model.Role{
		ID:          uuid.NewString(),
		TenantID:    tenantID,
		Name:        name,
		Permissions: permissions,
		CreatedAt:   time.Now(),
	}
	f.roles[tenantID+":"+r.ID] = r
	return r, nil
}

func (f *Fake) ListRoles(_ context.Context, tenantID string) ([]*model.Role, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var roles []*model.Role
	for _, r := range f.roles {
		if r.TenantID == tenantID {
			copy := *r
			roles = append(roles, &copy)
		}
	}
	return roles, nil
}

func (f *Fake) GetRole(_ context.Context, tenantID, roleID string) (*model.Role, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	r, ok := f.roles[tenantID+":"+roleID]
	if !ok {
		return nil, fmt.Errorf("role not found")
	}
	copy := *r
	return &copy, nil
}

func (f *Fake) AssignRole(_ context.Context, tenantID, userID, roleID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := tenantID + ":" + userID
	for _, rid := range f.userRole[key] {
		if rid == roleID {
			return nil // idempotent
		}
	}
	f.userRole[key] = append(f.userRole[key], roleID)
	return nil
}

func (f *Fake) GetUserRoles(_ context.Context, tenantID, userID string) ([]*model.Role, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	key := tenantID + ":" + userID
	var roles []*model.Role
	for _, rid := range f.userRole[key] {
		r, ok := f.roles[tenantID+":"+rid]
		if ok {
			copy := *r
			roles = append(roles, &copy)
		}
	}
	return roles, nil
}

func (f *Fake) CheckPermission(_ context.Context, tenantID, userID, permission string) (bool, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	key := tenantID + ":" + userID
	for _, rid := range f.userRole[key] {
		r, ok := f.roles[tenantID+":"+rid]
		if !ok {
			continue
		}
		for _, p := range r.Permissions {
			if p == permission || p == "*" {
				return true, nil
			}
		}
	}
	return false, nil
}

func (f *Fake) CreateAPIKey(_ context.Context, tenantID, userID, name, keyHash, keyPrefix string) (*model.APIKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := &model.APIKey{
		ID:        uuid.NewString(),
		TenantID:  tenantID,
		UserID:    userID,
		Name:      name,
		KeyHash:   keyHash,
		KeyPrefix: keyPrefix,
		CreatedAt: time.Now(),
	}
	f.apiKeys[k.ID] = k
	return k, nil
}

func (f *Fake) RevokeAPIKey(_ context.Context, tenantID, keyID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	k, ok := f.apiKeys[keyID]
	if !ok || k.TenantID != tenantID {
		return fmt.Errorf("api key not found")
	}
	k.Revoked = true
	return nil
}

func (f *Fake) WriteAuditLog(_ context.Context, entry *model.AuditLog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}
	if entry.OccurredAt.IsZero() {
		entry.OccurredAt = time.Now()
	}
	copy := *entry
	f.audit = append(f.audit, &copy)
	return nil
}

func (f *Fake) QueryAuditLogs(_ context.Context, tenantID string, limit, offset int) ([]*model.AuditLog, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var result []*model.AuditLog
	for _, a := range f.audit {
		if a.TenantID == tenantID {
			result = append(result, a)
		}
	}
	if offset >= len(result) {
		return nil, nil
	}
	result = result[offset:]
	if limit > 0 && limit < len(result) {
		result = result[:limit]
	}
	return result, nil
}
