package dto

// --- Request DTOs ---

// CreateTenantRequest represents the body for creating a tenant.
type CreateTenantRequest struct {
	Name         string `json:"name" binding:"required" example:"Acme Corp"`
	TokenBalance int64  `json:"token_balance,omitempty" example:"1000000000"`
	RateLimitRPM int    `json:"rate_limit_rpm,omitempty" example:"60"`
}

// UpdateTenantRequest represents the body for updating a tenant.
type UpdateTenantRequest struct {
	Name         string `json:"name" binding:"required" example:"Acme Corp Updated"`
	TokenBalance int64  `json:"token_balance" example:"2000000000"`
	IsActive     bool   `json:"is_active" example:"true"`
	RateLimitRPM int    `json:"rate_limit_rpm" example:"120"`
}

// CreatePoolRequest represents the body for creating a model routing pool.
type CreatePoolRequest struct {
	ModelPattern   string `json:"model_pattern" binding:"required" example:"gpt-4o"`
	Strategy       string `json:"strategy,omitempty" example:"round-robin"`
	FallbackPoolID *int   `json:"fallback_pool_id,omitempty" example:"2"`
}

// UpdatePoolRequest represents the body for updating a model routing pool.
type UpdatePoolRequest struct {
	ModelPattern   string `json:"model_pattern" binding:"required" example:"gpt-4o"`
	Strategy       string `json:"strategy" example:"weighted-round-robin"`
	FallbackPoolID *int   `json:"fallback_pool_id,omitempty"`
}

// CreateCredentialRequest represents the body for creating a provider credential.
type CreateCredentialRequest struct {
	PoolID   int    `json:"pool_id" binding:"required" example:"1"`
	Provider string `json:"provider" binding:"required" example:"openai"`
	APIKey   string `json:"api_key" binding:"required" example:"sk-..."`
	BaseURL  string `json:"base_url" binding:"required" example:"https://api.openai.com"`
	Weight   int    `json:"weight,omitempty" example:"1"`
}

// UpdateCredentialRequest represents the body for updating a provider credential.
type UpdateCredentialRequest struct {
	Provider  string `json:"provider" binding:"required" example:"openai"`
	APIKey    string `json:"api_key,omitempty" example:"sk-..."`
	BaseURL   string `json:"base_url" binding:"required" example:"https://api.openai.com"`
	Weight    int    `json:"weight" example:"1"`
	IsHealthy bool   `json:"is_healthy" example:"true"`
}
