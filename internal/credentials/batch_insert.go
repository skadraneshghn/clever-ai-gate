package credentials

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

// BatchInsertDiscoveredModels takes a slice of DiscoveredModelItems collected from all
// provider HTTP APIs and persists them into PostgreSQL within a SINGLE transaction.
//
// Key Design Principles:
// 1. Zero Deadlocks: Model patterns are sorted alphabetically before insertion to guarantee
//    deterministic row locking order in PostgreSQL.
// 2. High Performance: Uses a single transaction and bulk batch statements.
// 3. Single Reload Notification: Sends NOTIFY config_change 'model_pools:reload' exactly once
//    at the end of the entire job, eliminating notification storms.
func BatchInsertDiscoveredModels(ctx context.Context, db *pgxpool.Pool, vault *Vault, items []DiscoveredModelItem) (int, int, error) {
	if len(items) == 0 {
		return 0, 0, nil
	}

	// 1. Group items by ModelPattern and deduplicate
	type modelGroup struct {
		pattern      string
		capabilities ModelCapabilities
		credentials  []DiscoveredModelItem
	}

	groups := make(map[string]*modelGroup)
	var patterns []string

	for _, item := range items {
		if item.ModelPattern == "" {
			continue
		}
		grp, exists := groups[item.ModelPattern]
		if !exists {
			grp = &modelGroup{
				pattern:      item.ModelPattern,
				capabilities: item.Capabilities,
			}
			groups[item.ModelPattern] = grp
			patterns = append(patterns, item.ModelPattern)
		} else {
			grp.capabilities.Reasoning = grp.capabilities.Reasoning || item.Capabilities.Reasoning
			grp.capabilities.Vision = grp.capabilities.Vision || item.Capabilities.Vision
			grp.capabilities.ImageGeneration = grp.capabilities.ImageGeneration || item.Capabilities.ImageGeneration
			grp.capabilities.Audio = grp.capabilities.Audio || item.Capabilities.Audio
			grp.capabilities.Video = grp.capabilities.Video || item.Capabilities.Video
			grp.capabilities.Code = grp.capabilities.Code || item.Capabilities.Code
			grp.capabilities.Embedding = grp.capabilities.Embedding || item.Capabilities.Embedding
		}
		grp.credentials = append(grp.credentials, item)
	}

	if len(patterns) == 0 {
		return 0, 0, nil
	}

	// 2. Sort model patterns alphabetically to prevent PostgreSQL deadlocks (SQLSTATE 40P01)
	sort.Strings(patterns)

	// 3. Encrypt raw API keys in memory (caching encrypted values by provider + raw key)
	encryptedKeyCache := make(map[string]string)
	for i := range items {
		raw := items[i].RawAPIKey
		if raw == "" {
			continue
		}
		cacheKey := items[i].Provider + ":" + raw
		if _, ok := encryptedKeyCache[cacheKey]; !ok {
			enc, err := vault.Encrypt(raw)
			if err != nil {
				return 0, 0, fmt.Errorf("vault encryption failed for provider %s: %w", items[i].Provider, err)
			}
			encryptedKeyCache[cacheKey] = enc
		}
	}

	// 4. Begin single database transaction
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to start batch discovery transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	totalSynced := len(patterns)
	newModelsCount := 0

	for _, pattern := range patterns {
		grp := groups[pattern]

		capsJSON, err := json.Marshal(grp.capabilities.ToMap())
		if err != nil {
			capsJSON = []byte("{}")
		}

		var poolID int

		// Upsert model pool
		err = tx.QueryRow(ctx,
			`INSERT INTO model_pools (model_pattern, strategy, capabilities)
			 VALUES ($1, 'round-robin', $2)
			 ON CONFLICT (model_pattern) DO UPDATE
			 SET capabilities = EXCLUDED.capabilities
			 RETURNING id`,
			pattern, capsJSON,
		).Scan(&poolID)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to upsert model pool for %s: %w", pattern, err)
		}

		// Bind credentials for this pool
		for _, credItem := range grp.credentials {
			cacheKey := credItem.Provider + ":" + credItem.RawAPIKey
			encKey := encryptedKeyCache[cacheKey]
			if encKey == "" {
				continue
			}

			weight := credItem.Weight
			if weight <= 0 {
				weight = 1
			}

			// Check if credential already bound to pool
			var existingID int
			err := tx.QueryRow(ctx,
				`SELECT id FROM credentials 
				 WHERE pool_id = $1 AND provider = $2 AND base_url = $3 AND COALESCE(prefix, '') = $4`,
				poolID, credItem.Provider, credItem.BaseURL, credItem.Prefix,
			).Scan(&existingID)

			if err != nil { // No existing credential found, insert it
				_, err = tx.Exec(ctx,
					`INSERT INTO credentials (pool_id, provider, encrypted_key, base_url, weight, is_healthy, prefix)
					 VALUES ($1, $2, $3, $4, $5, true, $6)`,
					poolID, credItem.Provider, encKey, credItem.BaseURL, weight, credItem.Prefix,
				)
				if err != nil {
					return 0, 0, fmt.Errorf("failed to bind credential to pool %s (%s): %w", pattern, credItem.Provider, err)
				}
				newModelsCount++
			}
		}
	}

	// 5. Trigger single config change notification for the entire job
	if _, err = tx.Exec(ctx, "NOTIFY config_change, 'model_pools:reload'"); err != nil {
		return 0, 0, fmt.Errorf("failed to broadcast config change notification: %w", err)
	}

	return totalSynced, newModelsCount, tx.Commit(ctx)
}
