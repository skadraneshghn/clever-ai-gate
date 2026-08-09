package credentials

// DiscoveredModelItem holds in-memory model discovery metadata collected from provider HTTP APIs.
// Decoupling HTTP fetching from database operations prevents deadlocks and connection starvation.
type DiscoveredModelItem struct {
	ModelPattern string            `json:"model_pattern"`
	Provider     string            `json:"provider"`
	BaseURL      string            `json:"base_url"`
	RawAPIKey    string            `json:"raw_api_key"`
	Weight       int               `json:"weight"`
	Prefix       string            `json:"prefix"`
	Capabilities ModelCapabilities `json:"capabilities"`
}
