package dto

// --- Response DTOs ---

// TenantResponse represents a tenant in API responses.
type TenantResponse struct {
	ID           string `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Name         string `json:"name" example:"Acme Corp"`
	APIKey       string `json:"api_key" example:"cag_xxxxxxxxxxxxxxxxxxxx"`
	TokenBalance int64  `json:"token_balance" example:"1000000000"`
	IsActive     bool   `json:"is_active" example:"true"`
	RateLimitRPM int    `json:"rate_limit_rpm" example:"60"`
	CreatedAt    string `json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt    string `json:"updated_at" example:"2024-01-15T10:30:00Z"`
}

// PoolResponse represents a model routing pool in API responses.
type PoolResponse struct {
	ID              int                  `json:"id" example:"1"`
	ModelPattern    string               `json:"model_pattern" example:"gpt-4o"`
	Strategy        string               `json:"strategy" example:"round-robin"`
	FallbackPoolID  *int                 `json:"fallback_pool_id,omitempty" example:"2"`
	Capabilities    map[string]bool      `json:"capabilities,omitempty"`
	CredentialCount int                  `json:"credential_count,omitempty" example:"3"`
	Credentials     []CredentialResponse `json:"credentials,omitempty"`
	CreatedAt       string               `json:"created_at" example:"2024-01-15T10:30:00Z"`
}

// CredentialResponse represents a provider credential in API responses.
// The API key is always masked for security.
type CredentialResponse struct {
	ID           int     `json:"id" example:"1"`
	PoolID       int     `json:"pool_id" example:"1"`
	Provider     string  `json:"provider" example:"openai"`
	BaseURL      string  `json:"base_url" example:"https://api.openai.com"`
	Weight       int     `json:"weight" example:"1"`
	IsHealthy    bool    `json:"is_healthy" example:"true"`
	LastError    *string `json:"last_error,omitempty"`
	KeyMask      string  `json:"key_mask" example:"sk-...xxxx"`
	ModelPattern string  `json:"model_pattern,omitempty" example:"gpt-4o"`
	CreatedAt    string  `json:"created_at" example:"2024-01-15T10:30:00Z"`
}

// TenantInfoResponse represents detailed info about the tenant, returned on /playground/tenant.
type TenantInfoResponse struct {
	ID           string `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Name         string `json:"name" example:"Acme Corp"`
	TokenBalance int64  `json:"token_balance" example:"1000000000"`
	RateLimitRPM int    `json:"rate_limit_rpm" example:"60"`
}

// ErrorResponse represents an API error.
type ErrorResponse struct {
	Error   string `json:"error" example:"resource not found"`
	Details string `json:"details,omitempty" example:"tenant with id 'xxx' does not exist"`
}

// SuccessResponse represents a generic success message.
type SuccessResponse struct {
	Message string `json:"message" example:"resource deleted successfully"`
}

// MetricsResponse represents system metrics.
type MetricsResponse struct {
	Uptime         string  `json:"uptime" example:"24h30m15s"`
	TotalRequests  int64   `json:"total_requests" example:"1500000"`
	CacheHitRate   float64 `json:"cache_hit_rate" example:"99.2"`
	ActivePools    int     `json:"active_pools" example:"12"`
	ActiveTenants  int     `json:"active_tenants" example:"45"`
	QueueDepth     int     `json:"telemetry_queue_depth" example:"150"`
}

// MaskAPIKey returns a masked version of an API key showing only prefix and last 4 chars.
func MaskAPIKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	// Show first 3 chars and last 4 chars
	return key[:3] + "..." + key[len(key)-4:]
}
