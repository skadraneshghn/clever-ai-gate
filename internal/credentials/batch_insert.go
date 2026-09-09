package credentials

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
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
	newPoolsCount := 0
	newBindingsCount := 0

	poolIDMap := make(map[string]int, len(patterns))
	var allPoolIDs []int

	// 4a. Batch-upsert model pools with sorted keys to avoid deadlocks
	batchPools := &pgx.Batch{}
	for _, pattern := range patterns {
		grp := groups[pattern]

		capsJSON, err := json.Marshal(grp.capabilities.ToMap())
		if err != nil {
			capsJSON = []byte("{}")
		}

		batchPools.Queue(
			`INSERT INTO model_pools (model_pattern, strategy, capabilities)
			 VALUES ($1, 'round-robin', $2)
			 ON CONFLICT (model_pattern) DO UPDATE
			 SET capabilities = EXCLUDED.capabilities
			 RETURNING id, (xmax = 0)`,
			pattern, capsJSON,
		)
	}

	brPools := tx.SendBatch(ctx, batchPools)
	for _, pattern := range patterns {
		var poolID int
		var isInserted bool
		if err := brPools.QueryRow().Scan(&poolID, &isInserted); err != nil {
			_ = brPools.Close()
			return 0, 0, fmt.Errorf("failed to upsert model pool for %s: %w", pattern, err)
		}
		if isInserted {
			newPoolsCount++
		}
		poolIDMap[pattern] = poolID
		allPoolIDs = append(allPoolIDs, poolID)
	}
	_ = brPools.Close()

	// 4b. Pre-query all existing credentials for these pools in a SINGLE query
	alreadyBoundMap := make(map[string]bool)
	decryptedKeyCache := make(map[string]string)

	if len(allPoolIDs) > 0 {
		rows, qErr := tx.Query(ctx,
			`SELECT pool_id, provider, encrypted_key, base_url, COALESCE(prefix, '')
			 FROM credentials
			 WHERE pool_id = ANY($1)`,
			allPoolIDs,
		)
		if qErr == nil {
			defer rows.Close()
			for rows.Next() {
				var pID int
				var prov, encKey, bURL, pref string
				if err := rows.Scan(&pID, &prov, &encKey, &bURL, &pref); err == nil {
					// Index by encrypted key
					alreadyBoundMap[fmt.Sprintf("%d|%s|%s|%s|%s", pID, prov, encKey, bURL, pref)] = true

					// Also index by decrypted key so matching plain keys are recognized
					plainKey, ok := decryptedKeyCache[encKey]
					if !ok && vault != nil {
						if dec, decErr := vault.Decrypt(encKey); decErr == nil {
							plainKey = dec
							decryptedKeyCache[encKey] = dec
						}
					}
					if plainKey != "" {
						alreadyBoundMap[fmt.Sprintf("%d|%s|%s|%s|%s", pID, prov, plainKey, bURL, pref)] = true
					}
				}
			}
			rows.Close()
		}
	}

	// 4c. Queue all missing credential insertions
	var rowsToInsert [][]any

	for _, pattern := range patterns {
		poolID := poolIDMap[pattern]
		grp := groups[pattern]

		for _, credItem := range grp.credentials {
			// Check if already bound
			lookupPlain := fmt.Sprintf("%d|%s|%s|%s|%s", poolID, credItem.Provider, credItem.RawAPIKey, credItem.BaseURL, credItem.Prefix)
			lookupEnc := ""
			if credItem.EncryptedKey != "" {
				lookupEnc = fmt.Sprintf("%d|%s|%s|%s|%s", poolID, credItem.Provider, credItem.EncryptedKey, credItem.BaseURL, credItem.Prefix)
			}

			if (credItem.RawAPIKey != "" && alreadyBoundMap[lookupPlain]) || (lookupEnc != "" && alreadyBoundMap[lookupEnc]) {
				continue // Already bound to this pool
			}

			// Determine encrypted key for storage: prefer existing EncryptedKey, else use cached encrypted value
			encKey := credItem.EncryptedKey
			if encKey == "" {
				cacheKey := credItem.Provider + ":" + credItem.RawAPIKey
				encKey = encryptedKeyCache[cacheKey]
			}
			if encKey == "" {
				continue
			}

			weight := credItem.Weight
			if weight <= 0 {
				weight = 1
			}

			rowsToInsert = append(rowsToInsert, []any{
				poolID, credItem.Provider, encKey, credItem.BaseURL, weight, true, credItem.Prefix,
			})

			alreadyBoundMap[lookupPlain] = true
			if lookupEnc != "" {
				alreadyBoundMap[lookupEnc] = true
			}
			newBindingsCount++
		}
	}

	if len(rowsToInsert) > 0 {
		_, err = tx.CopyFrom(
			ctx,
			pgx.Identifier{"credentials"},
			[]string{"pool_id", "provider", "encrypted_key", "base_url", "weight", "is_healthy", "prefix"},
			pgx.CopyFromRows(rowsToInsert),
		)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to bulk copy credentials: %w", err)
		}
	}

	// 5. Trigger reload notification if any pools were created or credentials were bound
	if newPoolsCount > 0 || newBindingsCount > 0 {
		if _, err = tx.Exec(ctx, "NOTIFY config_change, 'model_pools:reload'"); err != nil {
			return 0, 0, fmt.Errorf("failed to broadcast config change notification: %w", err)
		}
	}

	return totalSynced, newBindingsCount, tx.Commit(ctx)
}
