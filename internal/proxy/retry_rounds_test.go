package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/skadraneshghn/clever-ai-gate/internal/cache"
	"github.com/skadraneshghn/clever-ai-gate/internal/config"
	"github.com/skadraneshghn/clever-ai-gate/internal/credentials"
	"go.uber.org/zap"
)

// ─────────────────────────────────────────────────────────────────────────────
// Retry-round policy tests
//
// Policy (see executeWithRetry): a pool is never abandoned on its first full
// failure. Every credential of the pool is retried in complete rounds (up to
// retryRoundsPerPool) before the gateway switches to the exact same model on
// another provider. Rounds are skipped when every failure in the last round
// was a non-retryable credential rejection (auth/quota/rate-limit/payload) —
// re-trying dead keys inside the same request would only stall the client.
// ─────────────────────────────────────────────────────────────────────────────

// setRetryRoundSettings pins the retry-round policy for one test and restores
// the defaults afterwards. The proxy test binary runs these tests sequentially
// (no t.Parallel in this package), so temporary mutation is race-free.
func setRetryRoundSettings(t *testing.T, rounds int, budget time.Duration) {
	t.Helper()
	oldRounds, oldBudget := retryRoundsPerPool, roundTransitionWaitBudget
	retryRoundsPerPool, roundTransitionWaitBudget = rounds, budget
	t.Cleanup(func() {
		retryRoundsPerPool, roundTransitionWaitBudget = oldRounds, oldBudget
	})
}

// roundTestJSONResponse builds a mock upstream JSON response.
func roundTestJSONResponse(status int, body string) *http.Response {
	resp := &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	resp.Header.Set("Content-Type", "application/json")
	return resp
}

// newRoundTestCred builds a runtime credential for a mocked pool.
func newRoundTestCred(id int, provider, key, baseURL string) *credentials.RuntimeCredential {
	return &credentials.RuntimeCredential{
		ID:       id,
		Provider: provider,
		APIKey:   key,
		BaseURL:  baseURL,
		Weight:   1,
	}
}

// serveRetryRoundsChat spins a gin router with the handler wired to the given
// pools (registered in both the cache store and the sync manager, exactly like
// production), routes all upstream traffic through roundTrip, executes one
// chat completion request, and returns the recorder.
func serveRetryRoundsChat(t *testing.T, roundTrip func(req *http.Request) (*http.Response, error), pools ...*credentials.BalancedChannelPool) *httptest.ResponseRecorder {
	t.Helper()

	logger := zap.NewNop()
	cfg := &config.Config{CacheMaxSizeMB: 10, CacheNumCounters: 100}
	cacheStore, err := cache.New(cfg, logger)
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	defer cacheStore.Close()

	poolMap := make(map[string]*credentials.BalancedChannelPool, len(pools))
	for _, p := range pools {
		cacheStore.Set(cache.PoolKey(p.ModelPattern), p, 1)
		poolMap[p.ModelPattern] = p
	}
	cacheStore.Wait()

	sm := credentials.NewSyncManager(nil, cacheStore, nil, logger)
	sm.SetPools(poolMap)

	h := NewHandler(&http.Client{Transport: &mockRoundTripper{roundTripFunc: roundTrip}}, cacheStore, nil, logger, nil, nil, nil)
	h.SetSyncManager(sm)

	router := gin.New()
	router.POST("/v1/chat/completions", h.Handle)

	payload := `{"model": "nvidia/meta/llama-3.3-70b-instruct", "messages": [{"role": "user", "content": "hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TestRetryRounds_RecoverInSecondRound_StaysOnSameProvider verifies that a
// transient 5xx does NOT trigger a provider switch: the same pool is retried
// in a second round and recovers, and the exact-model fallback pool is never
// touched.
func TestRetryRounds_RecoverInSecondRound_StaysOnSameProvider(t *testing.T) {
	setRetryRoundSettings(t, 3, 25*time.Millisecond)

	nvidiaPool := credentials.NewBalancedPool("nvidia/meta/llama-3.3-70b-instruct", "round-robin",
		[]*credentials.RuntimeCredential{newRoundTestCred(101, "nvidia", "nv-key", "https://integrate.api.nvidia.com")}, nil)
	orPool := credentials.NewBalancedPool("openrouter/meta-llama/llama-3.3-70b-instruct", "round-robin",
		[]*credentials.RuntimeCredential{newRoundTestCred(202, "openrouter", "or-key", "https://openrouter.ai/api")}, nil)

	nvidiaCalls := 0
	openrouterCalls := 0

	roundTrip := func(req *http.Request) (*http.Response, error) {
		bodyBytes, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		switch {
		case strings.Contains(req.URL.Host, "nvidia.com"):
			nvidiaCalls++
			if nvidiaCalls == 1 {
				// Round 1: transient 500 → must be retried, not a provider switch.
				return roundTestJSONResponse(http.StatusInternalServerError,
					`{"error": {"message": "transient blip", "type": "server_error"}}`), nil
			}
			return roundTestJSONResponse(http.StatusOK,
				`{"id":"ok","choices":[{"message":{"role":"assistant","content":"recovered on round two"}}]}`), nil
		case strings.Contains(req.URL.Host, "openrouter.ai"):
			openrouterCalls++
			return roundTestJSONResponse(http.StatusOK,
				`{"id":"or","choices":[{"message":{"role":"assistant","content":"openrouter answered"}}]}`), nil
		}
		return roundTestJSONResponse(http.StatusInternalServerError, `{"error":"unexpected host"}`), nil
	}

	w := serveRetryRoundsChat(t, roundTrip, nvidiaPool, orPool)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 after round-2 recovery, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "recovered on round two") {
		t.Fatalf("expected the answer from the SAME provider (nvidia), got: %s", w.Body.String())
	}
	if nvidiaCalls != 2 {
		t.Errorf("expected 2 nvidia attempts (round 1 fail + round 2 success), got %d", nvidiaCalls)
	}
	if openrouterCalls != 0 {
		t.Errorf("provider switch must not happen when the pool recovers in a retry round; openrouter was called %d time(s)", openrouterCalls)
	}
}

// TestRetryRounds_ThreeFailedRoundsThenExactModelFallback verifies the core
// policy: after retryRoundsPerPool complete rounds over the pool's credentials
// ALL fail, the gateway switches to the exact same model on another provider.
func TestRetryRounds_ThreeFailedRoundsThenExactModelFallback(t *testing.T) {
	setRetryRoundSettings(t, 3, 25*time.Millisecond)

	nvidiaPool := credentials.NewBalancedPool("nvidia/meta/llama-3.3-70b-instruct", "round-robin",
		[]*credentials.RuntimeCredential{newRoundTestCred(101, "nvidia", "nv-key", "https://integrate.api.nvidia.com")}, nil)
	orPool := credentials.NewBalancedPool("openrouter/meta-llama/llama-3.3-70b-instruct", "round-robin",
		[]*credentials.RuntimeCredential{newRoundTestCred(202, "openrouter", "or-key", "https://openrouter.ai/api")}, nil)

	var callLog []string

	roundTrip := func(req *http.Request) (*http.Response, error) {
		bodyBytes, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		switch {
		case strings.Contains(req.URL.Host, "nvidia.com"):
			callLog = append(callLog, "nvidia")
			return roundTestJSONResponse(http.StatusInternalServerError,
				`{"error": {"message": "nvidia down", "type": "server_error"}}`), nil
		case strings.Contains(req.URL.Host, "openrouter.ai"):
			callLog = append(callLog, "openrouter")
			return roundTestJSONResponse(http.StatusOK,
				`{"id":"or","choices":[{"message":{"role":"assistant","content":"openrouter answered"}}]}`), nil
		}
		return roundTestJSONResponse(http.StatusInternalServerError, `{"error":"unexpected host"}`), nil
	}

	w := serveRetryRoundsChat(t, roundTrip, nvidiaPool, orPool)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 from the fallback provider after 3 failed rounds, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "openrouter answered") {
		t.Fatalf("expected the openrouter answer, got: %s", w.Body.String())
	}
	want := []string{"nvidia", "nvidia", "nvidia", "openrouter"}
	if len(callLog) != len(want) || !reflect.DeepEqual(callLog, want) {
		t.Errorf("expected call sequence %v (3 complete retry rounds before switching providers), got %v", want, callLog)
	}
}

// TestRetryRounds_NonRetryableErrorsSkipRoundsAndSwitchImmediately verifies
// that quota/auth-style rejections (402 here — the Pollinations "not enough
// credits" class) do NOT burn retry rounds: re-trying the same dead keys inside
// the same request cannot succeed, so the gateway switches providers at once.
func TestRetryRounds_NonRetryableErrorsSkipRoundsAndSwitchImmediately(t *testing.T) {
	setRetryRoundSettings(t, 3, 25*time.Millisecond)

	nvidiaPool := credentials.NewBalancedPool("nvidia/meta/llama-3.3-70b-instruct", "round-robin",
		[]*credentials.RuntimeCredential{newRoundTestCred(101, "nvidia", "nv-key", "https://integrate.api.nvidia.com")}, nil)
	orPool := credentials.NewBalancedPool("openrouter/meta-llama/llama-3.3-70b-instruct", "round-robin",
		[]*credentials.RuntimeCredential{newRoundTestCred(202, "openrouter", "or-key", "https://openrouter.ai/api")}, nil)

	nvidiaCalls := 0
	openrouterCalls := 0

	roundTrip := func(req *http.Request) (*http.Response, error) {
		bodyBytes, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		switch {
		case strings.Contains(req.URL.Host, "nvidia.com"):
			nvidiaCalls++
			return roundTestJSONResponse(http.StatusPaymentRequired,
				`{"error": {"message": "upstream rejected the key", "type": "payment_required"}}`), nil
		case strings.Contains(req.URL.Host, "openrouter.ai"):
			openrouterCalls++
			return roundTestJSONResponse(http.StatusOK,
				`{"id":"or","choices":[{"message":{"role":"assistant","content":"openrouter answered"}}]}`), nil
		}
		return roundTestJSONResponse(http.StatusInternalServerError, `{"error":"unexpected host"}`), nil
	}

	w := serveRetryRoundsChat(t, roundTrip, nvidiaPool, orPool)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 from the fallback provider, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "openrouter answered") {
		t.Fatalf("expected the openrouter answer, got: %s", w.Body.String())
	}
	if nvidiaCalls != 1 {
		t.Errorf("non-retryable rejections must not burn retry rounds; expected 1 nvidia attempt, got %d", nvidiaCalls)
	}
	if openrouterCalls != 1 {
		t.Errorf("expected 1 openrouter attempt, got %d", openrouterCalls)
	}
}

// TestRetryRounds_FallbackAlsoFails_ReturnsMeaningfulError verifies the final
// requirement: when the current pool fails all its rounds AND the exact-model
// fallback provider fails too, the client receives a meaningful canonical
// error naming every pool, the rounds each consumed, and every credential
// attempt (with per-credential repeat counts).
func TestRetryRounds_FallbackAlsoFails_ReturnsMeaningfulError(t *testing.T) {
	setRetryRoundSettings(t, 3, 25*time.Millisecond)

	nvidiaPool := credentials.NewBalancedPool("nvidia/meta/llama-3.3-70b-instruct", "round-robin",
		[]*credentials.RuntimeCredential{newRoundTestCred(101, "nvidia", "nv-key", "https://integrate.api.nvidia.com")}, nil)
	orPool := credentials.NewBalancedPool("openrouter/meta-llama/llama-3.3-70b-instruct", "round-robin",
		[]*credentials.RuntimeCredential{newRoundTestCred(202, "openrouter", "or-key", "https://openrouter.ai/api")}, nil)

	roundTrip := func(req *http.Request) (*http.Response, error) {
		bodyBytes, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		switch {
		case strings.Contains(req.URL.Host, "nvidia.com"):
			return roundTestJSONResponse(http.StatusInternalServerError,
				`{"error": {"message": "nvidia down", "type": "server_error"}}`), nil
		case strings.Contains(req.URL.Host, "openrouter.ai"):
			return roundTestJSONResponse(http.StatusPaymentRequired,
				`{"error": {"message": "openrouter out of credits", "type": "insufficient_quota"}}`), nil
		}
		return roundTestJSONResponse(http.StatusInternalServerError, `{"error":"unexpected host"}`), nil
	}

	w := serveRetryRoundsChat(t, roundTrip, nvidiaPool, orPool)

	if w.Code == http.StatusOK {
		t.Fatalf("expected an error response after every provider failed, got HTTP 200: %s", w.Body.String())
	}
	body := w.Body.String()

	// Must be a canonical OpenAI error envelope.
	if !strings.Contains(body, `"error"`) {
		t.Errorf("expected OpenAI error envelope, got: %s", body)
	}

	// Must name every pool with its rounds, the aggregated attempts, and keep
	// the last upstream message as context.
	for _, want := range []string{
		"nvidia/meta/llama-3.3-70b-instruct (3 round(s))",
		"openrouter/meta-llama/llama-3.3-70b-instruct (1 round(s))",
		"cred#101(nvidia)→500 ×3",
		"cred#202(openrouter)→402",
		"openrouter out of credits",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("meaningful error missing %q:\n%s", want, body)
		}
	}
}

// TestIsNonRetryableCredentialStatus pins the classification that decides
// whether retry rounds are attempted at all.
func TestIsNonRetryableCredentialStatus(t *testing.T) {
	nonRetryable := []int{
		http.StatusBadRequest,          // invalid payload
		http.StatusUnauthorized,        // bad key
		http.StatusPaymentRequired,     // quota / billing (incl. sniffer 402s)
		http.StatusForbidden,           // wrong tier
		http.StatusNotFound,            // model not found (fast-fail)
		http.StatusUnprocessableEntity, // schema rejection
		http.StatusTooManyRequests,     // rate limit
	}
	for _, s := range nonRetryable {
		if !isNonRetryableCredentialStatus(s) {
			t.Errorf("status %d must be classified non-retryable", s)
		}
	}
	retryable := []int{0, 499, 500, 502, 503, 504, 520, 599}
	for _, s := range retryable {
		if isNonRetryableCredentialStatus(s) {
			t.Errorf("status %d must be classified retryable (transient)", s)
		}
	}
}

// TestPoolSoonestCooldownRemaining verifies the pacing signal used between
// retry rounds.
func TestPoolSoonestCooldownRemaining(t *testing.T) {
	credA := newRoundTestCred(1, "nvidia", "k", "https://example.com")
	credB := newRoundTestCred(2, "nvidia", "k", "https://example.com")
	pool := credentials.NewBalancedPool("nvidia/model", "round-robin",
		[]*credentials.RuntimeCredential{credA, credB}, nil)

	// Fresh pool: nothing cooling down.
	if got := poolSoonestCooldownRemaining(pool); got != 0 {
		t.Fatalf("fresh pool must report 0 remaining, got %v", got)
	}

	// credA cooling until +2s, credB until +5s → soonest is credA.
	now := time.Now().UnixNano()
	atomic.StoreInt64(&credA.CooldownUntil, now+2*time.Second.Nanoseconds())
	atomic.StoreInt64(&credB.CooldownUntil, now+5*time.Second.Nanoseconds())
	if got := poolSoonestCooldownRemaining(pool); got <= 0 || got > 2*time.Second {
		t.Fatalf("expected soonest remaining ≈2s, got %v", got)
	}

	// Nil pool must not panic.
	if got := poolSoonestCooldownRemaining(nil); got != 0 {
		t.Fatalf("nil pool must report 0 remaining, got %v", got)
	}
}

// TestBuildAttemptSummary_RoundsAndPools verifies the diagnostic message the
// client sees at total exhaustion.
func TestBuildAttemptSummary_RoundsAndPools(t *testing.T) {
	attempts := []attemptRecord{
		{provider: "nvidia", statusCode: 500, credID: 101},
		{provider: "nvidia", statusCode: 500, credID: 101},
		{provider: "nvidia", statusCode: 500, credID: 101},
		{provider: "openrouter", statusCode: 402, credID: 202},
	}
	roundsByPool := map[string]int{
		"nvidia/meta/llama-3.3-70b-instruct":           3,
		"openrouter/meta-llama/llama-3.3-70b-instruct": 1,
	}
	pools := []string{"nvidia/meta/llama-3.3-70b-instruct", "openrouter/meta-llama/llama-3.3-70b-instruct"}

	summary := buildAttemptSummary("nvidia/meta/llama-3.3-70b-instruct", attempts, roundsByPool, pools)

	for _, want := range []string{
		"all 4 credential attempt(s) exhausted",
		"nvidia/meta/llama-3.3-70b-instruct (3 round(s))",
		"openrouter/meta-llama/llama-3.3-70b-instruct (1 round(s))",
		"cred#101(nvidia)→500 ×3",
		"cred#202(openrouter)→402",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q:\n%s", want, summary)
		}
	}

	// Empty attempts must still produce a meaningful message.
	if got := buildAttemptSummary("m", nil, nil, nil); !strings.Contains(got, "exhausted with no successful response") {
		t.Errorf("unexpected empty-attempts summary: %s", got)
	}
}
