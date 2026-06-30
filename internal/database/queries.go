package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// --- Tenant Queries ---

// TenantRow represents a tenant record from the database.
type TenantRow struct {
	ID           string
	Name         string
	APIKey       string
	TokenBalance int64
	IsActive     bool
	RateLimitRPM int
	CreatedAt    string
	UpdatedAt    string
}

// GetTenantByAPIKey fetches a tenant by their virtual API key.
func GetTenantByAPIKey(ctx context.Context, pool *pgxpool.Pool, apiKey string) (*TenantRow, error) {
	row := pool.QueryRow(ctx, `
		SELECT id, name, api_key, token_balance, is_active, rate_limit_rpm, 
		       created_at::text, updated_at::text
		FROM tenants WHERE api_key = $1
	`, apiKey)

	t := &TenantRow{}
	err := row.Scan(&t.ID, &t.Name, &t.APIKey, &t.TokenBalance, &t.IsActive, &t.RateLimitRPM, &t.CreatedAt, &t.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}
	return t, nil
}

// ListTenants returns all tenants.
func ListTenants(ctx context.Context, pool *pgxpool.Pool) ([]*TenantRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, name, api_key, token_balance, is_active, rate_limit_rpm,
		       created_at::text, updated_at::text
		FROM tenants ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list tenants: %w", err)
	}
	defer rows.Close()

	var tenants []*TenantRow
	for rows.Next() {
		t := &TenantRow{}
		if err := rows.Scan(&t.ID, &t.Name, &t.APIKey, &t.TokenBalance, &t.IsActive, &t.RateLimitRPM, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan tenant: %w", err)
		}
		tenants = append(tenants, t)
	}
	return tenants, nil
}

// CreateTenant inserts a new tenant and returns the generated ID.
func CreateTenant(ctx context.Context, pool *pgxpool.Pool, name, apiKey string, tokenBalance int64, rateLimitRPM int) (string, error) {
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO tenants (name, api_key, token_balance, rate_limit_rpm)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, name, apiKey, tokenBalance, rateLimitRPM).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("failed to create tenant: %w", err)
	}
	return id, nil
}

// UpdateTenant updates a tenant's mutable fields.
func UpdateTenant(ctx context.Context, pool *pgxpool.Pool, id, name string, tokenBalance int64, isActive bool, rateLimitRPM int) error {
	_, err := pool.Exec(ctx, `
		UPDATE tenants SET name = $2, token_balance = $3, is_active = $4, 
		       rate_limit_rpm = $5, updated_at = NOW()
		WHERE id = $1
	`, id, name, tokenBalance, isActive, rateLimitRPM)
	if err != nil {
		return fmt.Errorf("failed to update tenant: %w", err)
	}
	return nil
}

// DeleteTenant removes a tenant by ID.
func DeleteTenant(ctx context.Context, pool *pgxpool.Pool, id string) error {
	_, err := pool.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete tenant: %w", err)
	}
	return nil
}

// --- Model Pool Queries ---

// ModelPoolRow represents a model routing pool record.
type ModelPoolRow struct {
	ID             int
	ModelPattern   string
	Strategy       string
	FallbackPoolID *int
	Capabilities   []byte // Raw JSONB bytes — decoded by callers
	CreatedAt      string
}

// ListModelPools returns all model routing pools.
func ListModelPools(ctx context.Context, pool *pgxpool.Pool) ([]*ModelPoolRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, model_pattern, strategy, fallback_pool_id, capabilities, created_at::text
		FROM model_pools ORDER BY model_pattern
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list pools: %w", err)
	}
	defer rows.Close()

	var pools []*ModelPoolRow
	for rows.Next() {
		p := &ModelPoolRow{}
		if err := rows.Scan(&p.ID, &p.ModelPattern, &p.Strategy, &p.FallbackPoolID, &p.Capabilities, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan pool: %w", err)
		}
		pools = append(pools, p)
	}
	return pools, nil
}

// GetModelPool returns a single model pool by ID.
func GetModelPool(ctx context.Context, dbPool *pgxpool.Pool, id int) (*ModelPoolRow, error) {
	row := dbPool.QueryRow(ctx, `
		SELECT id, model_pattern, strategy, fallback_pool_id, capabilities, created_at::text
		FROM model_pools WHERE id = $1
	`, id)

	p := &ModelPoolRow{}
	err := row.Scan(&p.ID, &p.ModelPattern, &p.Strategy, &p.FallbackPoolID, &p.Capabilities, &p.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get pool: %w", err)
	}
	return p, nil
}

// CreateModelPool inserts a new model routing pool.
func CreateModelPool(ctx context.Context, pool *pgxpool.Pool, modelPattern, strategy string, fallbackPoolID *int) (int, error) {
	var id int
	err := pool.QueryRow(ctx, `
		INSERT INTO model_pools (model_pattern, strategy, fallback_pool_id)
		VALUES ($1, $2, $3)
		RETURNING id
	`, modelPattern, strategy, fallbackPoolID).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("failed to create pool: %w", err)
	}
	return id, nil
}

// UpdateModelPool updates a model pool's configuration.
func UpdateModelPool(ctx context.Context, pool *pgxpool.Pool, id int, modelPattern, strategy string, fallbackPoolID *int) error {
	_, err := pool.Exec(ctx, `
		UPDATE model_pools SET model_pattern = $2, strategy = $3, fallback_pool_id = $4
		WHERE id = $1
	`, id, modelPattern, strategy, fallbackPoolID)
	if err != nil {
		return fmt.Errorf("failed to update pool: %w", err)
	}
	return nil
}

// DeleteModelPool removes a model pool and its credentials (CASCADE).
func DeleteModelPool(ctx context.Context, pool *pgxpool.Pool, id int) error {
	_, err := pool.Exec(ctx, `DELETE FROM model_pools WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete pool: %w", err)
	}
	return nil
}

// --- Credential Queries ---

// CredentialRow represents a provider credential record.
type CredentialRow struct {
	ID           int
	PoolID       int
	Provider     string
	EncryptedKey string
	BaseURL      string
	Weight       int
	IsHealthy    bool
	LastError    *string
	CreatedAt    string
}

// ListCredentialsByPool returns all credentials for a given pool.
func ListCredentialsByPool(ctx context.Context, pool *pgxpool.Pool, poolID int) ([]*CredentialRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, pool_id, provider, encrypted_key, base_url, weight, 
		       is_healthy, last_error, created_at::text
		FROM credentials WHERE pool_id = $1 ORDER BY id
	`, poolID)
	if err != nil {
		return nil, fmt.Errorf("failed to list credentials: %w", err)
	}
	defer rows.Close()

	var creds []*CredentialRow
	for rows.Next() {
		c := &CredentialRow{}
		if err := rows.Scan(&c.ID, &c.PoolID, &c.Provider, &c.EncryptedKey, &c.BaseURL, &c.Weight, &c.IsHealthy, &c.LastError, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan credential: %w", err)
		}
		creds = append(creds, c)
	}
	return creds, nil
}

// CredentialWithPool extends CredentialRow with the model pattern from the
// associated pool, used by the management dashboard list view.
type CredentialWithPool struct {
	CredentialRow
	ModelPattern string
}

// ListAllCredentials returns all credentials across all pools, joined with
// the model_pools table to include the model_pattern string.
func ListAllCredentials(ctx context.Context, pool *pgxpool.Pool) ([]*CredentialWithPool, error) {
	rows, err := pool.Query(ctx, `
		SELECT c.id, c.pool_id, c.provider, c.encrypted_key, c.base_url, c.weight,
		       c.is_healthy, c.last_error, c.created_at::text,
		       COALESCE(mp.model_pattern, '') AS model_pattern
		FROM credentials c
		LEFT JOIN model_pools mp ON c.pool_id = mp.id
		ORDER BY c.id
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list all credentials: %w", err)
	}
	defer rows.Close()

	var creds []*CredentialWithPool
	for rows.Next() {
		c := &CredentialWithPool{}
		if err := rows.Scan(
			&c.ID, &c.PoolID, &c.Provider, &c.EncryptedKey, &c.BaseURL, &c.Weight,
			&c.IsHealthy, &c.LastError, &c.CreatedAt, &c.ModelPattern,
		); err != nil {
			return nil, fmt.Errorf("failed to scan credential: %w", err)
		}
		creds = append(creds, c)
	}
	return creds, nil
}

// GetCredential returns a single credential by ID.
func GetCredential(ctx context.Context, pool *pgxpool.Pool, id int) (*CredentialRow, error) {
	row := pool.QueryRow(ctx, `
		SELECT id, pool_id, provider, encrypted_key, base_url, weight,
		       is_healthy, last_error, created_at::text
		FROM credentials WHERE id = $1
	`, id)

	c := &CredentialRow{}
	err := row.Scan(&c.ID, &c.PoolID, &c.Provider, &c.EncryptedKey, &c.BaseURL, &c.Weight, &c.IsHealthy, &c.LastError, &c.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get credential: %w", err)
	}
	return c, nil
}

// CreateCredential inserts a new provider credential.
func CreateCredential(ctx context.Context, pool *pgxpool.Pool, poolID int, provider, encryptedKey, baseURL string, weight int) (int, error) {
	var id int
	err := pool.QueryRow(ctx, `
		INSERT INTO credentials (pool_id, provider, encrypted_key, base_url, weight)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, poolID, provider, encryptedKey, baseURL, weight).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("failed to create credential: %w", err)
	}
	return id, nil
}

// UpdateCredential updates a credential's mutable fields.
func UpdateCredential(ctx context.Context, pool *pgxpool.Pool, id int, provider, encryptedKey, baseURL string, weight int, isHealthy bool) error {
	_, err := pool.Exec(ctx, `
		UPDATE credentials SET provider = $2, encrypted_key = $3, base_url = $4, 
		       weight = $5, is_healthy = $6
		WHERE id = $1
	`, id, provider, encryptedKey, baseURL, weight, isHealthy)
	if err != nil {
		return fmt.Errorf("failed to update credential: %w", err)
	}
	return nil
}

// DeleteCredential removes a credential by ID.
func DeleteCredential(ctx context.Context, pool *pgxpool.Pool, id int) error {
	_, err := pool.Exec(ctx, `DELETE FROM credentials WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete credential: %w", err)
	}
	return nil
}

// --- Bulk Loading (for cache warm-up) ---

// LoadAllPoolsWithCredentials loads all model pools and their credentials
// for initial cache population at startup.
func LoadAllPoolsWithCredentials(ctx context.Context, pool *pgxpool.Pool) ([]*ModelPoolRow, map[int][]*CredentialRow, error) {
	pools, err := ListModelPools(ctx, pool)
	if err != nil {
		return nil, nil, err
	}

	credsByPool := make(map[int][]*CredentialRow)
	for _, p := range pools {
		creds, err := ListCredentialsByPool(ctx, pool, p.ID)
		if err != nil {
			return nil, nil, err
		}
		credsByPool[p.ID] = creds
	}

	return pools, credsByPool, nil
}

// --- Playground Conversation Queries ---

// ConversationRow represents a conversation history session stored in the database.
type ConversationRow struct {
	ID        string
	TenantID  string
	Title     string
	Messages  []byte // Raw JSONB bytes
	CreatedAt string
	UpdatedAt string
}

// ListConversations returns all conversations for a tenant sorted by last updated.
func ListConversations(ctx context.Context, pool *pgxpool.Pool, tenantID string) ([]*ConversationRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, tenant_id, title, messages, created_at::text, updated_at::text
		FROM conversations 
		WHERE tenant_id = $1
		ORDER BY updated_at DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to list conversations: %w", err)
	}
	defer rows.Close()

	var list []*ConversationRow
	for rows.Next() {
		c := &ConversationRow{}
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Title, &c.Messages, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan conversation: %w", err)
		}
		list = append(list, c)
	}
	return list, nil
}

// GetConversation fetches a single conversation by UUID and validates tenant access.
func GetConversation(ctx context.Context, pool *pgxpool.Pool, id string, tenantID string) (*ConversationRow, error) {
	row := pool.QueryRow(ctx, `
		SELECT id, tenant_id, title, messages, created_at::text, updated_at::text
		FROM conversations WHERE id = $1 AND tenant_id = $2
	`, id, tenantID)

	c := &ConversationRow{}
	err := row.Scan(&c.ID, &c.TenantID, &c.Title, &c.Messages, &c.CreatedAt, &c.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get conversation: %w", err)
	}
	return c, nil
}

// CreateConversation inserts a new conversation history session for a tenant.
func CreateConversation(ctx context.Context, pool *pgxpool.Pool, tenantID string, title string, messages []byte) (string, error) {
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO conversations (tenant_id, title, messages)
		VALUES ($1, $2, $3)
		RETURNING id
	`, tenantID, title, messages).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("failed to create conversation: %w", err)
	}
	return id, nil
}

// UpdateConversation updates the title and/or messages array of a conversation under tenant validation.
func UpdateConversation(ctx context.Context, pool *pgxpool.Pool, id string, tenantID string, title string, messages []byte) error {
	_, err := pool.Exec(ctx, `
		UPDATE conversations SET title = $3, messages = $4, updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
	`, id, tenantID, title, messages)
	if err != nil {
		return fmt.Errorf("failed to update conversation: %w", err)
	}
	return nil
}

// DeleteConversation deletes a conversation session from the database under tenant validation.
func DeleteConversation(ctx context.Context, pool *pgxpool.Pool, id string, tenantID string) error {
	_, err := pool.Exec(ctx, `DELETE FROM conversations WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	if err != nil {
		return fmt.Errorf("failed to delete conversation: %w", err)
	}
	return nil
}
