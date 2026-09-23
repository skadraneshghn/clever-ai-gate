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
	return serveRetryRoundsChatModel(t, "nvidia/meta/llama-3.3-70b-instruct", roundTrip, pools...)
}

// serveRetryRoundsChatModel is serveRetryRoundsChat with a configurable
// request model (e.g. puter/* patterns for provider-specific scenarios).
func serveRetryRoundsChatModel(t *testing.T, model string, roundTrip func(req *http.Request) (*http.Response, error), pools ...*credentials.BalancedChannelPool) *httptest.ResponseRecorder {
	t.Helper()
	payload := `{"model": "` + model + `", "messages": [{"role": "user", "content": "hi"}]}`
	return serveRetryRoundsChatPayload(t, model, payload, roundTrip, pools...)
}

// serveRetryRoundsChatPayload is serveRetryRoundsChatModel with a fully
// custom JSON payload (e.g. requests carrying temperature/top_p).
func serveRetryRoundsChatPayload(t *testing.T, model, payload string, roundTrip func(req *http.Request) (*http.Response, error), pools ...*credentials.BalancedChannelPool) *httptest.ResponseRecorder {
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

// ─────────────────────────────────────────────────────────────────────────────
// Long-cooldown fast-fail tests
//
// Policy (see executeWithRetry acquisition path): a FRESH request (round 1,
// including the first round on a fallback pool) that finds every credential of
// the pool cooling down for longer than cooldownAcquireWaitBudget must NOT
// sleep 600ms per key probing doomed credentials — it fails over to the
// exact-model fallback immediately, or returns a meaningful 503 without a
// single upstream call. Brief penalties (≤ budget) are still waited out, and
// retry rounds 2+ keep probing their own transient cooldowns.
// ─────────────────────────────────────────────────────────────────────────────

// TestCooldownFastFail_AllKeysCooling_Returns503WithoutUpstreamCall reproduces
// the puter.com pathology from the production logs: every key of the pool was
// hard-rejected earlier (suspended account / exhausted quota) and carries a
// long cooldown. The gateway must answer instantly with a 503 that explains
// the cooldown situation instead of sleeping ~600ms per key.
func TestCooldownFastFail_AllKeysCooling_Returns503WithoutUpstreamCall(t *testing.T) {
	setRetryRoundSettings(t, 3, 25*time.Millisecond)

	puterPool := credentials.NewBalancedPool("puter/infron:xiaomi/mimo-v2.6-pro-ultraspeed", "round-robin",
		[]*credentials.RuntimeCredential{
			newRoundTestCred(330519, "puter", "puter-token-1", "https://api.puter.com"),
			newRoundTestCred(330520, "puter", "puter-token-2", "https://api.puter.com"),
		}, nil)

	// Simulate the production state: both puter keys were suspended minutes
	// ago and received long quota cooldowns.
	now := time.Now().UnixNano()
	atomic.StoreInt64(&puterPool.Credentials[0].CooldownUntil, now+15*time.Second.Nanoseconds())
	atomic.StoreInt64(&puterPool.Credentials[1].CooldownUntil, now+16*time.Second.Nanoseconds())

	upstreamCalls := 0
	roundTrip := func(req *http.Request) (*http.Response, error) {
		upstreamCalls++
		return roundTestJSONResponse(http.StatusForbidden, `{"error":"Account suspended"}`), nil
	}

	start := time.Now()
	w := serveRetryRoundsChatModel(t, "puter/infron:xiaomi/mimo-v2.6-pro-ultraspeed", roundTrip, puterPool)
	elapsed := time.Since(start)

	if upstreamCalls != 0 {
		t.Fatalf("no upstream request may be sent when the whole pool is cooling down, got %d call(s)", upstreamCalls)
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected HTTP 503 (temporary unavailability), got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		"temporarily unavailable",
		"cooling down",
		"no upstream request was sent",
		"soonest retry in ~",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected error body to contain %q:\n%s", want, body)
		}
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("fast-fail must be immediate, took %v", elapsed)
	}
}

// TestCooldownFastFail_FallsBackToHealthyExactModelPool verifies that the
// long-cooldown fast-fail feeds the exact-model cross-provider fallback: a
// fully rate-limited provider is skipped instantly (zero probes) and the same
// model is served by another provider.
func TestCooldownFastFail_FallsBackToHealthyExactModelPool(t *testing.T) {
	setRetryRoundSettings(t, 3, 25*time.Millisecond)

	nvidiaCred := newRoundTestCred(101, "nvidia", "nv-key", "https://integrate.api.nvidia.com")
	nvidiaPool := credentials.NewBalancedPool("nvidia/meta/llama-3.3-70b-instruct", "round-robin",
		[]*credentials.RuntimeCredential{nvidiaCred}, nil)
	orPool := credentials.NewBalancedPool("openrouter/meta-llama/llama-3.3-70b-instruct", "round-robin",
		[]*credentials.RuntimeCredential{newRoundTestCred(202, "openrouter", "or-key", "https://openrouter.ai/api")}, nil)

	// Whole primary pool rate-limited for 10s — longer than the acquire budget.
	now := time.Now().UnixNano()
	atomic.StoreInt64(&nvidiaCred.CooldownUntil, now+10*time.Second.Nanoseconds())

	nvidiaCalls, openrouterCalls := 0, 0
	roundTrip := func(req *http.Request) (*http.Response, error) {
		bodyBytes, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		switch {
		case strings.Contains(req.URL.Host, "nvidia.com"):
			nvidiaCalls++
			return roundTestJSONResponse(http.StatusTooManyRequests, `{"error":"rate limited"}`), nil
		case strings.Contains(req.URL.Host, "openrouter.ai"):
			openrouterCalls++
			return roundTestJSONResponse(http.StatusOK,
				`{"id":"or","choices":[{"message":{"role":"assistant","content":"openrouter answered"}}]}`), nil
		}
		return roundTestJSONResponse(http.StatusInternalServerError, `{"error":"unexpected host"}`), nil
	}

	start := time.Now()
	w := serveRetryRoundsChat(t, roundTrip, nvidiaPool, orPool)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 from the exact-model fallback, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "openrouter answered") {
		t.Fatalf("expected the openrouter answer, got: %s", w.Body.String())
	}
	if nvidiaCalls != 0 {
		t.Errorf("cooling-down primary pool must not be probed, got %d nvidia call(s)", nvidiaCalls)
	}
	if openrouterCalls != 1 {
		t.Errorf("expected exactly 1 openrouter attempt, got %d", openrouterCalls)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("failover must be immediate, took %v", elapsed)
	}
}

// TestCooldownFastFail_ShortCooldownStillWaitsAndServes verifies that brief
// contention penalties (≤ cooldownAcquireWaitBudget) are still waited out and
// served — the fast-fail only applies to long cooldowns.
func TestCooldownFastFail_ShortCooldownStillWaitsAndServes(t *testing.T) {
	setRetryRoundSettings(t, 3, 25*time.Millisecond)

	nvidiaCred := newRoundTestCred(101, "nvidia", "nv-key", "https://integrate.api.nvidia.com")
	nvidiaPool := credentials.NewBalancedPool("nvidia/meta/llama-3.3-70b-instruct", "round-robin",
		[]*credentials.RuntimeCredential{nvidiaCred}, nil)

	// Brief penalty from a concurrent request — well within the budget.
	now := time.Now().UnixNano()
	atomic.StoreInt64(&nvidiaCred.CooldownUntil, now+250*time.Millisecond.Nanoseconds())

	calls := 0
	roundTrip := func(req *http.Request) (*http.Response, error) {
		calls++
		return roundTestJSONResponse(http.StatusOK,
			`{"id":"ok","choices":[{"message":{"role":"assistant","content":"served after brief cooldown"}}]}`), nil
	}

	w := serveRetryRoundsChat(t, roundTrip, nvidiaPool)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 after waiting out the brief cooldown, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "served after brief cooldown") {
		t.Fatalf("expected the successful answer, got: %s", w.Body.String())
	}
	if calls != 1 {
		t.Errorf("expected exactly 1 upstream attempt, got %d", calls)
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

// ─────────────────────────────────────────────────────────────────────────────
// Claude "no sampling parameters" tests (see sampling_params.go)
//
// Production evidence (gateway-2026-09-22.log): the Claude ≥ Opus 4.5
// generation hard-rejects temperature with Cloudflare code 7003 —
// "`temperature` is not supported on this model. Remove it from your
// request." — and the inference provider returns opaque transient 400s
// ("The upstream provider returned an error while processing this
// request."). Both used to abort rotation as "payload schema error" even
// though every pool credential was healthy.
// ─────────────────────────────────────────────────────────────────────────────

// temperatureUnsupportedBody is the exact upstream 400 body observed in
// production for anthropic/claude-opus-5 (Cloudflare Workers AI, code 7003).
const temperatureUnsupportedBody = "{\"errors\":[{\"message\":\"Model execution failed (User Input Error): Validation error at temperature: `temperature` is not supported on this model. Remove it from your request. See https://docs.anthropic.com/en/docs/about-claude/models/migration-guide for details.\",\"code\":7003}],\"success\":false,\"result\":{},\"messages\":[]}"

// TestRetryRounds_ClaudeNoSamplingGeneration_ProactiveStrip verifies the
// claude-opus-5 generation never sees temperature/top_p/top_k: the gateway
// strips them before the first upstream call, so a strict Anthropic-schema
// endpoint accepts the request on the very first attempt.
func TestRetryRounds_ClaudeNoSamplingGeneration_ProactiveStrip(t *testing.T) {
	setRetryRoundSettings(t, 3, 25*time.Millisecond)

	infPool := credentials.NewBalancedPool("inference/claude-opus-5", "round-robin",
		[]*credentials.RuntimeCredential{newRoundTestCred(101, "inference", "inf-key", "https://api.inference.net")}, nil)
	orPool := credentials.NewBalancedPool("openrouter/claude-opus-5", "round-robin",
		[]*credentials.RuntimeCredential{newRoundTestCred(202, "openrouter", "or-key", "https://openrouter.ai/api")}, nil)

	upstreamCalls := 0
	roundTrip := func(req *http.Request) (*http.Response, error) {
		bodyBytes, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		switch {
		case strings.Contains(req.URL.Host, "inference.net"):
			upstreamCalls++
			body := string(bodyBytes)
			for _, param := range []string{"temperature", "top_p", "top_k"} {
				if strings.Contains(body, `"`+param+`"`) {
					return roundTestJSONResponse(http.StatusBadRequest, temperatureUnsupportedBody), nil
				}
			}
			return roundTestJSONResponse(http.StatusOK,
				`{"id":"inf","choices":[{"message":{"role":"assistant","content":"inference answered"}}]}`), nil
		case strings.Contains(req.URL.Host, "openrouter.ai"):
			return roundTestJSONResponse(http.StatusInternalServerError, `{"error":"unexpected fallback"}`), nil
		}
		return roundTestJSONResponse(http.StatusInternalServerError, `{"error":"unexpected host"}`), nil
	}

	payload := `{"model": "inference/claude-opus-5", "messages": [{"role": "user", "content": "hi"}], "temperature": 0.7, "top_p": 0.9}`
	w := serveRetryRoundsChatPayload(t, "inference/claude-opus-5", payload, roundTrip, infPool, orPool)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 on the first attempt (sampling params stripped before send), got %d: %s", w.Code, w.Body.String())
	}
	if upstreamCalls != 1 {
		t.Errorf("expected exactly 1 upstream call, got %d", upstreamCalls)
	}
	if !strings.Contains(w.Body.String(), "inference answered") {
		t.Errorf("expected the inference answer, got: %s", w.Body.String())
	}
}

// TestRetryRounds_UnsupportedSamplingParam_SelfHeals verifies the reactive
// path: a model OUTSIDE the proactive gate (a future generation the gateway
// does not know yet) rejects temperature with the explicit "not supported"
// error; the gateway strips the parameter and retries the SAME healthy
// credential — no rotation, no 400 fast-fail abort, request completes.
func TestRetryRounds_UnsupportedSamplingParam_SelfHeals(t *testing.T) {
	setRetryRoundSettings(t, 3, 25*time.Millisecond)

	futurePool := credentials.NewBalancedPool("newvendor/future-model-x", "round-robin",
		[]*credentials.RuntimeCredential{newRoundTestCred(101, "newvendor", "nv-key", "https://api.newvendor.org")}, nil)

	var bodies []string
	roundTrip := func(req *http.Request) (*http.Response, error) {
		bodyBytes, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		bodies = append(bodies, string(bodyBytes))
		if strings.Contains(string(bodyBytes), `"temperature"`) {
			return roundTestJSONResponse(http.StatusBadRequest, temperatureUnsupportedBody), nil
		}
		return roundTestJSONResponse(http.StatusOK,
			`{"id":"ok","choices":[{"message":{"role":"assistant","content":"self-healed"}}]}`), nil
	}

	payload := `{"model": "newvendor/future-model-x", "messages": [{"role": "user", "content": "hi"}], "temperature": 0.7}`
	w := serveRetryRoundsChatPayload(t, "newvendor/future-model-x", payload, roundTrip, futurePool)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 after self-healing the payload, got %d: %s", w.Code, w.Body.String())
	}
	if len(bodies) != 2 {
		t.Fatalf("expected exactly 2 upstream calls (reject → strip → retry same key), got %d", len(bodies))
	}
	if !strings.Contains(bodies[0], `"temperature"`) {
		t.Errorf("first upstream call should carry temperature, got: %s", bodies[0])
	}
	if strings.Contains(bodies[1], `"temperature"`) {
		t.Errorf("second upstream call must not carry temperature, got: %s", bodies[1])
	}
	if !strings.Contains(w.Body.String(), "self-healed") {
		t.Errorf("expected the retried answer, got: %s", w.Body.String())
	}
}

// TestRetryRounds_TransientUpstream400_RotatesAndFallsBack verifies that an
// opaque upstream-side 400 ("The upstream provider returned an error while
// processing this request." — observed from the inference provider on
// claude-opus-5) is NOT counted as a payload schema error: every pool key is
// tried and the exact-model fallback still runs. Before the fix, the second
// such 400 aborted rotation mid-pool — the third key was never tried.
func TestRetryRounds_TransientUpstream400_RotatesAndFallsBack(t *testing.T) {
	setRetryRoundSettings(t, 3, 25*time.Millisecond)

	infPool := credentials.NewBalancedPool("inference/claude-opus-5", "round-robin",
		[]*credentials.RuntimeCredential{
			newRoundTestCred(101, "inference", "inf-key-1", "https://api.inference.net"),
			newRoundTestCred(102, "inference", "inf-key-2", "https://api.inference.net"),
			newRoundTestCred(103, "inference", "inf-key-3", "https://api.inference.net"),
		}, nil)
	orPool := credentials.NewBalancedPool("openrouter/claude-opus-5", "round-robin",
		[]*credentials.RuntimeCredential{newRoundTestCred(202, "openrouter", "or-key", "https://openrouter.ai/api")}, nil)

	inferenceCalls := 0
	openrouterCalls := 0
	roundTrip := func(req *http.Request) (*http.Response, error) {
		bodyBytes, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		switch {
		case strings.Contains(req.URL.Host, "inference.net"):
			inferenceCalls++
			return roundTestJSONResponse(http.StatusBadRequest,
				`{"error":{"message":"The upstream provider returned an error while processing this request.","type":"upstream_error"}}`), nil
		case strings.Contains(req.URL.Host, "openrouter.ai"):
			openrouterCalls++
			return roundTestJSONResponse(http.StatusOK,
				`{"id":"or","choices":[{"message":{"role":"assistant","content":"openrouter answered"}}]}`), nil
		}
		return roundTestJSONResponse(http.StatusInternalServerError, `{"error":"unexpected host"}`), nil
	}

	payload := `{"model": "inference/claude-opus-5", "messages": [{"role": "user", "content": "hi"}]}`
	w := serveRetryRoundsChatPayload(t, "inference/claude-opus-5", payload, roundTrip, infPool, orPool)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 from the fallback provider, got %d: %s", w.Code, w.Body.String())
	}
	if inferenceCalls != 3 {
		t.Errorf("expected all 3 inference keys tried (no payload-schema abort after two transient 400s), got %d calls", inferenceCalls)
	}
	if openrouterCalls != 1 {
		t.Errorf("expected 1 openrouter attempt, got %d", openrouterCalls)
	}
	if !strings.Contains(w.Body.String(), "openrouter answered") {
		t.Errorf("expected the openrouter answer, got: %s", w.Body.String())
	}
}
