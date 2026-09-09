package credentials

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func TestNvidiaLiveAPI(t *testing.T) {
	_ = godotenv.Load("../../.env")
	dbURL := os.Getenv("DATABASE_URL")
	encKey := os.Getenv("MASTER_ENCRYPTION_KEY")

	if dbURL == "" || encKey == "" {
		t.Skip("DATABASE_URL or MASTER_ENCRYPTION_KEY not set")
	}

	vault, err := NewVault(encKey)
	if err != nil {
		t.Fatalf("vault: %v", err)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	defer pool.Close()

	var encryptedKey, baseURL string
	err = pool.QueryRow(ctx, "SELECT encrypted_key, base_url FROM credentials WHERE provider = 'nvidia' LIMIT 1").Scan(&encryptedKey, &baseURL)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	apiKey, err := vault.Decrypt(encryptedKey)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	t.Logf("BaseURL: %s, APIKey: %s...", baseURL, apiKey[:10])

	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://integrate.api.nvidia.com/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	t.Logf("HTTP Status: %d, body len: %d", resp.StatusCode, len(body))

	var modelList struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &modelList); err != nil {
		t.Fatalf("unmarshal err: %v\nbody: %s", err, string(body))
	}

	t.Logf("Models count from API: %d", len(modelList.Data))
	for i, m := range modelList.Data {
		t.Logf("  [%d] %s", i, m.ID)
	}
}

func TestNvidiaProviderFetch(t *testing.T) {
	_ = godotenv.Load("../../.env")
	dbURL := os.Getenv("DATABASE_URL")
	encKey := os.Getenv("MASTER_ENCRYPTION_KEY")

	if dbURL == "" || encKey == "" {
		t.Skip("DATABASE_URL or MASTER_ENCRYPTION_KEY not set")
	}

	vault, err := NewVault(encKey)
	if err != nil {
		t.Fatalf("vault: %v", err)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	defer pool.Close()

	var encryptedKey, baseURL string
	err = pool.QueryRow(ctx, "SELECT encrypted_key, base_url FROM credentials WHERE provider = 'nvidia' LIMIT 1").Scan(&encryptedKey, &baseURL)
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	apiKey, err := vault.Decrypt(encryptedKey)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	// 1. Test as predefined nvidia provider
	accNvidia := providerAccount{
		Provider:     "nvidia",
		EncryptedKey: encryptedKey,
		BaseURL:      baseURL,
		Prefix:       "",
		Weight:       1,
	}
	itemsNvidia, errNvidia := fetchProviderDiscoveredModels(ctx, accNvidia, apiKey, 1)
	t.Logf("fetchProviderDiscoveredModels (nvidia): err=%v, items=%d", errNvidia, len(itemsNvidia))
	if errNvidia != nil || len(itemsNvidia) == 0 {
		t.Fatalf("expected nvidia items, got err=%v, count=%d", errNvidia, len(itemsNvidia))
	}
	for _, item := range itemsNvidia {
		if strings.HasPrefix(item.ModelPattern, "nvidia/nvidia/") {
			t.Errorf("found unwanted double prefix pattern: %s", item.ModelPattern)
		}
		if item.EncryptedKey == "" {
			t.Errorf("expected EncryptedKey to be populated on item %s", item.ModelPattern)
		}
	}

	// 2. Test as custom openai-compatible provider pointing to nvidia
	accCustom := providerAccount{
		Provider:     "custom",
		EncryptedKey: encryptedKey,
		BaseURL:      baseURL,
		Prefix:       "custom-nim",
		Weight:       1,
	}
	itemsCustom, errCustom := fetchProviderDiscoveredModels(ctx, accCustom, apiKey, 1)
	t.Logf("fetchProviderDiscoveredModels (custom pointing to nvidia): err=%v, items=%d", errCustom, len(itemsCustom))
	if errCustom != nil || len(itemsCustom) == 0 {
		t.Fatalf("expected custom items, got err=%v, count=%d", errCustom, len(itemsCustom))
	}
	for _, item := range itemsCustom {
		if strings.HasPrefix(item.ModelPattern, "custom-nim/custom-nim/") {
			t.Errorf("found unwanted double prefix pattern: %s", item.ModelPattern)
		}
		if item.EncryptedKey == "" {
			t.Errorf("expected EncryptedKey to be populated on custom item %s", item.ModelPattern)
		}
	}
}

func TestSimulateReDiscovery(t *testing.T) {
	_ = godotenv.Load("../../.env")
	dbURL := os.Getenv("DATABASE_URL")
	encKey := os.Getenv("MASTER_ENCRYPTION_KEY")

	if dbURL == "" || encKey == "" {
		t.Skip("DATABASE_URL or MASTER_ENCRYPTION_KEY not set")
	}

	vault, err := NewVault(encKey)
	if err != nil {
		t.Fatalf("vault: %v", err)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	defer pool.Close()

	existingPatterns, err := snapshotModelPatterns(ctx, pool)
	if err != nil {
		t.Fatalf("snapshotModelPatterns: %v", err)
	}
	t.Logf("Total existing patterns in model_pools: %d", len(existingPatterns))

	accounts, err := queryDistinctAccounts(ctx, pool)
	if err != nil {
		t.Fatalf("queryDistinctAccounts: %v", err)
	}
	t.Logf("Total distinct accounts: %d", len(accounts))

	var nvidiaAccounts []providerAccount
	for _, acc := range accounts {
		if acc.Provider == "nvidia" || strings.Contains(acc.BaseURL, "nvidia") {
			nvidiaAccounts = append(nvidiaAccounts, acc)
		}
	}
	t.Logf("Distinct NVIDIA accounts: %d", len(nvidiaAccounts))

	// Check how many of the live models are in existingPatterns
	var encryptedKey, baseURL string
	err = pool.QueryRow(ctx, "SELECT encrypted_key, base_url FROM credentials WHERE provider = 'nvidia' LIMIT 1").Scan(&encryptedKey, &baseURL)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	apiKey, _ := vault.Decrypt(encryptedKey)

	items, _ := fetchProviderDiscoveredModels(ctx, providerAccount{
		Provider: "nvidia",
		BaseURL:  baseURL,
	}, apiKey, 1)

	newPatterns := 0
	existingInPools := 0
	for _, item := range items {
		if existingPatterns[item.ModelPattern] {
			existingInPools++
		} else {
			newPatterns++
			t.Logf("  TRULY NEW PATTERN: %s", item.ModelPattern)
		}
	}
	t.Logf("Nvidia items: total=%d, existingInPools=%d, newPatterns=%d", len(items), existingInPools, newPatterns)

	// Check how many of the 160 items are actually bound to each of the 35 nvidia accounts in credentials table!
	unboundCount := 0
	for _, acc := range nvidiaAccounts {
		var count int
		_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM credentials WHERE provider = $1 AND base_url = $2 AND encrypted_key = $3", acc.Provider, acc.BaseURL, acc.EncryptedKey).Scan(&count)
		t.Logf("Nvidia account (%s) is bound to %d pools", acc.EncryptedKey[:10], count)
		if count < len(items)/2 {
			unboundCount++
		}
	}
	t.Logf("Nvidia accounts with fewer than 80 pools: %d / %d", unboundCount, len(nvidiaAccounts))

	// Check distinct decrypted keys vs distinct encrypted keys
	distinctPlainKeys := make(map[string]int)
	for _, acc := range nvidiaAccounts {
		plain, err := vault.Decrypt(acc.EncryptedKey)
		if err == nil {
			distinctPlainKeys[plain]++
		}
	}
	t.Logf("Distinct plain API keys among 35 accounts: %d", len(distinctPlainKeys))
	for k, count := range distinctPlainKeys {
		t.Logf("  Key %s... appears in %d encrypted_key accounts", k[:10], count)
	}
}

func TestNvidiaChatModelFormat(t *testing.T) {
	_ = godotenv.Load("../../.env")
	dbURL := os.Getenv("DATABASE_URL")
	encKey := os.Getenv("MASTER_ENCRYPTION_KEY")

	if dbURL == "" || encKey == "" {
		t.Skip("DATABASE_URL or MASTER_ENCRYPTION_KEY not set")
	}

	vault, err := NewVault(encKey)
	if err != nil {
		t.Fatalf("vault: %v", err)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	defer pool.Close()

	var encryptedKey string
	err = pool.QueryRow(ctx, "SELECT encrypted_key FROM credentials WHERE provider = 'nvidia' LIMIT 1").Scan(&encryptedKey)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	apiKey, _ := vault.Decrypt(encryptedKey)

	client := &http.Client{Timeout: 15 * time.Second}

	testPayload := func(modelName string) (int, string) {
		payload := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hi"}],"max_tokens":5}`, modelName)
		req, _ := http.NewRequestWithContext(ctx, "POST", "https://integrate.api.nvidia.com/v1/chat/completions", strings.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	code1, body1 := testPayload("nvidia/llama-3.1-nemotron-70b-instruct")
	t.Logf("Calling with 'nvidia/llama-3.1-nemotron-70b-instruct': Status=%d, Body=%s", code1, body1[:min(200, len(body1))])

	code2, body2 := testPayload("llama-3.1-nemotron-70b-instruct")
	t.Logf("Calling with 'llama-3.1-nemotron-70b-instruct': Status=%d, Body=%s", code2, body2[:min(200, len(body2))])

	code3, body3 := testPayload("meta/llama-3.2-11b-vision-instruct")
	t.Logf("Calling with 'meta/llama-3.2-11b-vision-instruct': Status=%d, Body=%s", code3, body3[:min(200, len(body3))])
}

func TestCheckProviders(t *testing.T) {
	_ = godotenv.Load("../../.env")
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	defer pool.Close()

	rows, err := pool.Query(ctx, `
		SELECT provider, COUNT(DISTINCT encrypted_key), COUNT(*)
		FROM credentials
		GROUP BY provider
		ORDER BY COUNT(*) DESC
	`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var provider string
		var keys, total int
		if err := rows.Scan(&provider, &keys, &total); err == nil {
			t.Logf("Provider: %-15s | Unique Encrypted Keys: %-4d | Total Credentials: %-6d", provider, keys, total)
		}
	}
}

func TestBatchInsertMultipleAccountsAndIdempotency(t *testing.T) {
	_ = godotenv.Load("../../.env")
	dbURL := os.Getenv("DATABASE_URL")
	encKey := os.Getenv("MASTER_ENCRYPTION_KEY")

	if dbURL == "" || encKey == "" {
		t.Skip("DATABASE_URL or MASTER_ENCRYPTION_KEY not set")
	}

	vault, err := NewVault(encKey)
	if err != nil {
		t.Fatalf("vault: %v", err)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	defer pool.Close()

	testPattern := fmt.Sprintf("test-nvidia-rediscovery-%d", time.Now().UnixNano())

	// Clean up after test
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM model_pools WHERE model_pattern = $1", testPattern)
	}()

	key1 := "nvapi-testkey-1111111111111111111111111111111111111111111111111111111111111111"
	key2 := "nvapi-testkey-2222222222222222222222222222222222222222222222222222222222222222"

	enc1, err := vault.Encrypt(key1)
	if err != nil {
		t.Fatalf("encrypt key1: %v", err)
	}
	enc2, err := vault.Encrypt(key2)
	if err != nil {
		t.Fatalf("encrypt key2: %v", err)
	}

	items := []DiscoveredModelItem{
		{
			ModelPattern: testPattern,
			Provider:     "nvidia",
			BaseURL:      "https://integrate.api.nvidia.com/v1",
			RawAPIKey:    key1,
			EncryptedKey: enc1,
			Weight:       1,
			Capabilities: ModelCapabilities{Reasoning: true},
		},
		{
			ModelPattern: testPattern,
			Provider:     "nvidia",
			BaseURL:      "https://integrate.api.nvidia.com/v1",
			RawAPIKey:    key2,
			EncryptedKey: enc2,
			Weight:       2,
			Capabilities: ModelCapabilities{Reasoning: true},
		},
	}

	// 1. First run: should insert 1 pool and bind 2 credentials
	totalSynced, newBindings, err := BatchInsertDiscoveredModels(ctx, pool, vault, items)
	if err != nil {
		t.Fatalf("first BatchInsertDiscoveredModels failed: %v", err)
	}
	if totalSynced != 1 {
		t.Errorf("expected 1 pool synced, got %d", totalSynced)
	}
	if newBindings != 2 {
		t.Errorf("expected 2 credentials bound, got %d", newBindings)
	}

	// Verify both credentials exist in the pool
	var credCount int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM credentials c
		JOIN model_pools p ON c.pool_id = p.id
		WHERE p.model_pattern = $1
	`, testPattern).Scan(&credCount)
	if err != nil {
		t.Fatalf("query credCount: %v", err)
	}
	if credCount != 2 {
		t.Errorf("expected 2 credentials in DB, got %d", credCount)
	}

	// 2. Second run: exact same items. Should be 100% idempotent: 0 new bindings!
	totalSynced2, newBindings2, err2 := BatchInsertDiscoveredModels(ctx, pool, vault, items)
	if err2 != nil {
		t.Fatalf("second BatchInsertDiscoveredModels failed: %v", err2)
	}
	if totalSynced2 != 1 {
		t.Errorf("expected 1 pool synced, got %d", totalSynced2)
	}
	if newBindings2 != 0 {
		t.Errorf("expected 0 new bindings on duplicate run, got %d", newBindings2)
	}

	// Verify count is still exactly 2
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM credentials c
		JOIN model_pools p ON c.pool_id = p.id
		WHERE p.model_pattern = $1
	`, testPattern).Scan(&credCount)
	if err != nil {
		t.Fatalf("query credCount second time: %v", err)
	}
	if credCount != 2 {
		t.Errorf("expected 2 credentials in DB after idempotent run, got %d", credCount)
	}
}

func TestNvidiaReDiscoveryLive(t *testing.T) {
	_ = godotenv.Load("../../.env")
	dbURL := os.Getenv("DATABASE_URL")
	encKey := os.Getenv("MASTER_ENCRYPTION_KEY")

	if dbURL == "" || encKey == "" {
		t.Skip("DATABASE_URL or MASTER_ENCRYPTION_KEY not set")
	}

	vault, err := NewVault(encKey)
	if err != nil {
		t.Fatalf("vault: %v", err)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("pgxpool: %v", err)
	}
	defer pool.Close()

	allAccounts, err := queryDistinctAccounts(ctx, pool)
	if err != nil {
		t.Fatalf("queryDistinctAccounts: %v", err)
	}

	var nvidiaAccounts []providerAccount
	for _, acc := range allAccounts {
		if acc.Provider == "nvidia" {
			nvidiaAccounts = append(nvidiaAccounts, acc)
		}
	}
	t.Logf("Found %d distinct NVIDIA accounts", len(nvidiaAccounts))
	if len(nvidiaAccounts) == 0 {
		t.Fatal("no nvidia accounts in database")
	}

	// 1. Fetch live models using the first working key
	var discoveredItems []DiscoveredModelItem
	for _, acc := range nvidiaAccounts {
		key, decErr := vault.Decrypt(acc.EncryptedKey)
		if decErr != nil {
			continue
		}
		items, fetchErr := fetchProviderDiscoveredModels(ctx, acc, key, acc.Weight)
		if fetchErr == nil && len(items) > 0 {
			discoveredItems = items
			break
		}
	}
	if len(discoveredItems) == 0 {
		t.Fatal("failed to discover models from nvidia live endpoint")
	}
	t.Logf("Discovered %d model patterns from live NVIDIA endpoint", len(discoveredItems))

	// 2. Expand across all 35 NVIDIA accounts
	var allItems []DiscoveredModelItem
	for _, acc := range nvidiaAccounts {
		key, decErr := vault.Decrypt(acc.EncryptedKey)
		if decErr != nil {
			continue
		}
		for _, baseItem := range discoveredItems {
			allItems = append(allItems, DiscoveredModelItem{
				ModelPattern: baseItem.ModelPattern,
				Provider:     acc.Provider,
				BaseURL:      acc.BaseURL,
				RawAPIKey:    key,
				EncryptedKey: acc.EncryptedKey,
				Weight:       acc.Weight,
				Prefix:       acc.Prefix,
				Capabilities: baseItem.Capabilities,
			})
		}
	}
	t.Logf("Total expanded items for all nvidia accounts: %d", len(allItems))

	// 3. Sync into PostgreSQL via BatchInsertDiscoveredModels
	totalSynced, newBindings, err := BatchInsertDiscoveredModels(ctx, pool, vault, allItems)
	if err != nil {
		t.Fatalf("BatchInsertDiscoveredModels failed: %v", err)
	}
	t.Logf("First sync result: totalSynced=%d, newBindings=%d", totalSynced, newBindings)

	// 4. Second sync immediately after — must be 100% idempotent
	totalSynced2, newBindings2, err2 := BatchInsertDiscoveredModels(ctx, pool, vault, allItems)
	if err2 != nil {
		t.Fatalf("Second BatchInsertDiscoveredModels failed: %v", err2)
	}
	t.Logf("Second sync result (idempotency check): totalSynced=%d, newBindings=%d", totalSynced2, newBindings2)
	if newBindings2 != 0 {
		t.Errorf("expected 0 new bindings on duplicate run, got %d", newBindings2)
	}

	// 5. Verify that each distinct plain API key is bound to all 160 pools!
	distinctPlainKeys := make(map[string]bool)
	for _, acc := range nvidiaAccounts {
		key, decErr := vault.Decrypt(acc.EncryptedKey)
		if decErr == nil {
			distinctPlainKeys[key] = true
		}
	}
	t.Logf("Verifying all %d distinct plain API keys...", len(distinctPlainKeys))

	unboundKeys := 0
	for plainKey := range distinctPlainKeys {
		// Find all poolIDs this plainKey is bound to
		rows, qErr := pool.Query(ctx, "SELECT DISTINCT pool_id, encrypted_key FROM credentials WHERE provider = 'nvidia'")
		if qErr != nil {
			t.Fatalf("query credentials: %v", qErr)
		}
		boundPools := make(map[int]bool)
		decCache := make(map[string]string)
		for rows.Next() {
			var pID int
			var enc string
			if err := rows.Scan(&pID, &enc); err == nil {
				dec, ok := decCache[enc]
				if !ok {
					decrypted, decErr := vault.Decrypt(enc)
					if decErr == nil {
						dec = decrypted
						decCache[enc] = dec
					}
				}
				if dec == plainKey {
					boundPools[pID] = true
				}
			}
		}
		rows.Close()

		if len(boundPools) < len(discoveredItems) {
			unboundKeys++
			t.Logf("Plain key %s... is bound to %d pools (expected %d)", plainKey[:10], len(boundPools), len(discoveredItems))
		}
	}

	if unboundKeys > 0 {
		t.Errorf("expected 0 distinct plain keys with missing pools, got %d", unboundKeys)
	} else {
		t.Logf("SUCCESS: ALL %d distinct plain NVIDIA API keys are bound to all %d discovered model pools!", len(distinctPlainKeys), len(discoveredItems))
	}
}

