<script>
  import { onDestroy, onMount } from 'svelte';
  import { 
    HeartPulse, Play, RefreshCw, CheckCircle2, XCircle, Clock, Activity, 
    AlertTriangle, Server, ShieldCheck, Cpu, ArrowRight, ChevronRight, Zap
  } from '@lucide/svelte';
  import { appState } from '$lib/state.svelte.js';
  import Card from '$lib/components/Card.svelte';
  import Button from '$lib/components/Button.svelte';

  // State definitions
  let sessions = $state([]);
  let activeSession = $state(null);
  let liveItems = $state([]);
  let progress = $state(0);
  let isRunning = $state(false);
  let loadingSessions = $state(true);
  let selectedSessionDetails = $state(null);
  let selectedSessionId = $state(null);
  let loadingDetails = $state(false);
  let eventSource = null;

  // Filter & Search states
  let filterStatus = $state('all'); // all, healthy, failed
  let searchQuery = $state('');

  function adminHeaders() {
    return {
      'Authorization': `Bearer ${appState.getAdminKey()}`,
      'Content-Type': 'application/json'
    };
  }

  async function fetchSessions() {
    try {
      loadingSessions = true;
      const res = await fetch('/api/v1/admin/health-check/sessions', {
        headers: adminHeaders()
      });
      if (res.ok) {
        sessions = await res.json();
      }
    } catch (err) {
      console.error('Failed to fetch health check sessions:', err);
    } finally {
      loadingSessions = false;
    }
  }

  async function fetchSessionDetails(sessionId) {
    try {
      loadingDetails = true;
      selectedSessionId = sessionId;
      const res = await fetch(`/api/v1/admin/health-check/sessions/${sessionId}`, {
        headers: adminHeaders()
      });
      if (res.ok) {
        selectedSessionDetails = await res.json();
      }
    } catch (err) {
      console.error('Failed to fetch session details:', err);
    } finally {
      loadingDetails = false;
    }
  }

  async function triggerHealthCheck() {
    if (isRunning) return;
    try {
      isRunning = true;
      liveItems = [];
      progress = 0;
      selectedSessionDetails = null;
      selectedSessionId = null;

      const res = await fetch('/api/v1/admin/health-check/trigger', {
        method: 'POST',
        headers: adminHeaders()
      });

      if (!res.ok) {
        const err = await res.json();
        appState.addToast(err.error || 'Failed to trigger health check', 'error');
        isRunning = false;
      } else {
        appState.addToast('Exhaustive model health check initiated', 'success');
      }
    } catch (err) {
      appState.addToast('Connection error triggering health check', 'error');
      isRunning = false;
    }
  }

  function initSSE() {
    if (typeof window === 'undefined') return;
    
    // Connect to SSE stream
    eventSource = new EventSource('/api/v1/admin/health-check/stream');

    eventSource.onmessage = (event) => {
      try {
        const payload = JSON.parse(event.data);
        
        if (payload.event_type === 'start') {
          activeSession = payload.summary;
          liveItems = [];
          progress = 0;
          isRunning = true;
        } else if (payload.event_type === 'progress') {
          progress = payload.progress || 0;
          if (payload.item) {
            liveItems = [payload.item, ...liveItems];
          }
          if (payload.summary) {
            activeSession = payload.summary;
          }
        } else if (payload.event_type === 'complete') {
          activeSession = payload.summary;
          progress = 100;
          isRunning = false;
          fetchSessions();
        }
      } catch (err) {
        console.error('SSE JSON parse error:', err);
      }
    };

    eventSource.onerror = (err) => {
      console.warn('SSE connection warning:', err);
    };
  }

  onMount(() => {
    fetchSessions();
    initSSE();
  });

  onDestroy(() => {
    if (eventSource) {
      eventSource.close();
    }
  });

  // Filtered live feed or selected session item results
  let displayedItems = $derived(() => {
    let items = selectedSessionDetails ? selectedSessionDetails : liveItems;
    if (filterStatus === 'healthy') {
      items = items.filter(i => i.is_healthy);
    } else if (filterStatus === 'failed') {
      items = items.filter(i => !i.is_healthy);
    }

    if (searchQuery.trim()) {
      const q = searchQuery.toLowerCase();
      items = items.filter(i => 
        (i.pool_name && i.pool_name.toLowerCase().includes(q)) ||
        (i.model_pattern && i.model_pattern.toLowerCase().includes(q)) ||
        (i.provider_id && i.provider_id.toLowerCase().includes(q)) ||
        (i.error_message && i.error_message.toLowerCase().includes(q))
      );
    }
    return items;
  });
</script>

<div class="p-6 max-w-7xl mx-auto space-y-6 animate-fade-in">
  <!-- Header -->
  <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4 bg-[var(--card-bg)] p-6 rounded-2xl border border-[var(--border-color)] shadow-sm">
    <div class="flex items-center gap-3">
      <div class="p-3 bg-orange-500/10 text-orange-500 rounded-xl">
        <HeartPulse size={28} />
      </div>
      <div>
        <h1 class="text-2xl font-bold tracking-tight text-[var(--text-primary)]">Model Pool Health Monitor</h1>
        <p class="text-sm text-secondary">Real-time exhaustive (Pool × Credential) probe diagnostics and historical telemetry</p>
      </div>
    </div>
    
    <div class="flex items-center gap-3">
      <Button
        variant="ghost"
        size="md"
        onclick={fetchSessions}
        disabled={loadingSessions}
        title="Refresh History"
      >
        <RefreshCw size={16} class={loadingSessions ? 'animate-spin' : ''} />
        <span>Refresh</span>
      </Button>

      <Button
        variant="primary"
        size="md"
        onclick={triggerHealthCheck}
        disabled={isRunning}
        class="bg-gradient-to-r from-orange-500 to-amber-500 hover:from-orange-600 hover:to-amber-600 text-white shadow-md font-semibold"
      >
        {#if isRunning}
          <RefreshCw size={18} class="animate-spin" />
          <span>Probing Matrix ({progress.toFixed(0)}%)</span>
        {:else}
          <Play size={18} />
          <span>Run Full Health Check</span>
        {/if}
      </Button>
    </div>
  </div>

  <!-- Live Progress & Metrics Banner -->
  {#if activeSession || isRunning}
    <div class="bg-[var(--card-bg)] border border-[var(--border-color)] p-6 rounded-2xl shadow-sm space-y-4">
      <div class="flex items-center justify-between">
        <div class="flex items-center gap-2">
          <span class="relative flex h-3 w-3">
            <span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-orange-400 opacity-75"></span>
            <span class="relative inline-flex rounded-full h-3 w-3 bg-orange-500"></span>
          </span>
          <h2 class="font-semibold text-lg text-[var(--text-primary)]">
            Active Probe Session {activeSession?.id ? `(${activeSession.id.slice(0, 8)})` : ''}
          </h2>
        </div>
        <span class="text-sm font-mono font-bold text-orange-500 bg-orange-500/10 px-3 py-1 rounded-full">
          {progress.toFixed(1)}% Complete
        </span>
      </div>

      <!-- Animated Progress Bar -->
      <div class="w-full bg-gray-200 dark:bg-gray-700/50 rounded-full h-3 overflow-hidden p-0.5">
        <div 
          class="bg-gradient-to-r from-orange-500 to-amber-400 h-2 rounded-full transition-all duration-300 shadow-sm"
          style="width: {progress}%"
        ></div>
      </div>

      <!-- Quick Metrics Grid -->
      <div class="grid grid-cols-2 sm:grid-cols-4 gap-4 pt-2">
        <div class="bg-[var(--frame-bg)] p-3.5 rounded-xl border border-[var(--border-color)] text-center">
          <span class="text-xs text-secondary font-medium block uppercase tracking-wider">Total Matrix Tasks</span>
          <span class="text-2xl font-extrabold text-[var(--text-primary)]">{activeSession?.total_tasks || 0}</span>
        </div>
        <div class="bg-emerald-500/5 border border-emerald-500/20 p-3.5 rounded-xl text-center">
          <span class="text-xs text-emerald-600 dark:text-emerald-400 font-medium block uppercase tracking-wider">Passed Probes</span>
          <span class="text-2xl font-extrabold text-emerald-600 dark:text-emerald-400">{activeSession?.passed_count || 0}</span>
        </div>
        <div class="bg-rose-500/5 border border-rose-500/20 p-3.5 rounded-xl text-center">
          <span class="text-xs text-rose-600 dark:text-rose-400 font-medium block uppercase tracking-wider">Failed Probes</span>
          <span class="text-2xl font-extrabold text-rose-600 dark:text-rose-400">{activeSession?.failed_count || 0}</span>
        </div>
        <div class="bg-amber-500/5 border border-amber-500/20 p-3.5 rounded-xl text-center">
          <span class="text-xs text-amber-600 dark:text-amber-400 font-medium block uppercase tracking-wider">Avg Latency</span>
          <span class="text-2xl font-extrabold text-amber-600 dark:text-amber-400">{activeSession?.avg_latency_ms ? activeSession.avg_latency_ms.toFixed(0) : 0} ms</span>
        </div>
      </div>
    </div>
  {/if}

  <!-- Main View Grid: Left = Feed & Filter, Right = Session History -->
  <div class="grid grid-cols-1 lg:grid-cols-3 gap-6">
    <!-- Live Results Feed / Selected Session Details (2 cols) -->
    <div class="lg:col-span-2 space-y-4">
      <!-- Toolbar Filter -->
      <div class="flex flex-col sm:flex-row items-center justify-between gap-3 bg-[var(--card-bg)] p-4 rounded-xl border border-[var(--border-color)] shadow-sm">
        <div class="flex items-center gap-2 w-full sm:w-auto">
          <button
            class="px-3 py-1.5 rounded-lg text-xs font-semibold transition-all {filterStatus === 'all' ? 'bg-orange-500 text-white shadow-sm' : 'bg-[var(--frame-bg)] text-secondary hover:text-primary'}"
            onclick={() => filterStatus = 'all'}
          >
            All Results
          </button>
          <button
            class="px-3 py-1.5 rounded-lg text-xs font-semibold transition-all {filterStatus === 'healthy' ? 'bg-emerald-500 text-white shadow-sm' : 'bg-[var(--frame-bg)] text-secondary hover:text-primary'}"
            onclick={() => filterStatus = 'healthy'}
          >
            Healthy Only
          </button>
          <button
            class="px-3 py-1.5 rounded-lg text-xs font-semibold transition-all {filterStatus === 'failed' ? 'bg-rose-500 text-white shadow-sm' : 'bg-[var(--frame-bg)] text-secondary hover:text-primary'}"
            onclick={() => filterStatus = 'failed'}
          >
            Failed Only
          </button>
        </div>

        <input
          type="text"
          bind:value={searchQuery}
          placeholder="Filter by pool, model, provider..."
          class="filter-search-input w-full sm:w-64 text-xs px-3 py-1.5 rounded-lg bg-[var(--frame-bg)] border border-[var(--border-color)] outline-none focus:border-orange-500"
        />
      </div>

      <!-- Feed Table Card -->
      <div class="bg-[var(--card-bg)] rounded-xl border border-[var(--border-color)] shadow-sm overflow-hidden">
        <div class="px-5 py-3.5 border-b border-[var(--border-color)] bg-[var(--frame-bg)] flex items-center justify-between">
          <div class="flex items-center gap-2">
            <Activity size={16} class="text-orange-500" />
            <h3 class="font-bold text-sm text-[var(--text-primary)]">
              {#if selectedSessionId}
                Viewing Session {selectedSessionId.slice(0, 8)} Results ({displayedItems().length})
              {:else}
                Live Probe Feed ({displayedItems().length})
              {/if}
            </h3>
          </div>
          {#if selectedSessionId}
            <button 
              onclick={() => { selectedSessionId = null; selectedSessionDetails = null; }}
              class="text-xs text-orange-500 hover:underline font-medium"
            >
              Clear filter (Show live feed)
            </button>
          {/if}
        </div>

        <div class="overflow-x-auto max-h-[600px] overflow-y-auto">
          <table class="w-full text-left border-collapse">
            <thead class="sticky top-0 bg-[var(--frame-bg)] border-b border-[var(--border-color)] text-xs font-semibold text-secondary">
              <tr>
                <th class="py-2.5 px-4">Pool / Model</th>
                <th class="py-2.5 px-4">Provider</th>
                <th class="py-2.5 px-4">Status</th>
                <th class="py-2.5 px-4">Latency</th>
                <th class="py-2.5 px-4">Error Details</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-[var(--border-color)] text-xs">
              {#if loadingDetails}
                <tr>
                  <td colspan="5" class="py-8 text-center text-secondary">
                    <RefreshCw size={20} class="animate-spin mx-auto mb-2 text-orange-500" />
                    <span>Loading session details...</span>
                  </td>
                </tr>
              {:else if displayedItems().length === 0}
                <tr>
                  <td colspan="5" class="py-12 text-center text-secondary">
                    <Server size={28} class="mx-auto mb-2 opacity-40 text-orange-500" />
                    <p class="font-medium">No probe results available yet</p>
                    <p class="text-xs opacity-75 mt-1">Click "Run Full Health Check" to start a live matrix scan</p>
                  </td>
                </tr>
              {:else}
                {#each displayedItems() as item (item.id || item.credential_id + '-' + item.checked_at)}
                  <tr class="hover:bg-[var(--frame-bg)]/50 transition-colors">
                    <td class="py-2.5 px-4">
                      <div class="font-bold text-[var(--text-primary)]">{item.pool_name}</div>
                      <div class="font-mono text-[11px] text-secondary">{item.model_pattern}</div>
                    </td>
                    <td class="py-2.5 px-4 font-medium text-secondary">
                      <span class="px-2 py-0.5 rounded bg-[var(--frame-bg)] border border-[var(--border-color)]">
                        {item.provider_id}
                      </span>
                    </td>
                    <td class="py-2.5 px-4">
                      {#if item.is_healthy}
                        <span class="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-[11px] font-semibold bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border border-emerald-500/20">
                          <CheckCircle2 size={12} />
                          {item.status_code || 200} HEALTHY
                        </span>
                      {:else}
                        <span class="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-[11px] font-semibold bg-rose-500/10 text-rose-600 dark:text-rose-400 border border-rose-500/20">
                          <XCircle size={12} />
                          {item.status_code || 500} FAILED
                        </span>
                      {/if}
                    </td>
                    <td class="py-2.5 px-4 font-mono font-medium text-secondary">
                      {item.latency_ms} ms
                    </td>
                    <td class="py-2.5 px-4 max-w-xs truncate text-rose-500 font-mono text-[11px]" title={item.error_message}>
                      {item.error_message || '-'}
                    </td>
                  </tr>
                {/each}
              {/if}
            </tbody>
          </table>
        </div>
      </div>
    </div>

    <!-- Right Sidebar: Session Audit History (1 col) -->
    <div class="space-y-4">
      <div class="bg-[var(--card-bg)] rounded-xl border border-[var(--border-color)] shadow-sm overflow-hidden">
        <div class="px-5 py-3.5 border-b border-[var(--border-color)] bg-[var(--frame-bg)] flex items-center justify-between">
          <div class="flex items-center gap-2">
            <Clock size={16} class="text-orange-500" />
            <h3 class="font-bold text-sm text-[var(--text-primary)]">Past Health Sessions</h3>
          </div>
          <span class="text-xs font-semibold text-secondary">{sessions.length} Runs</span>
        </div>

        <div class="divide-y divide-[var(--border-color)] max-h-[600px] overflow-y-auto">
          {#if loadingSessions}
            <div class="p-6 text-center text-secondary">
              <RefreshCw size={18} class="animate-spin mx-auto mb-2 text-orange-500" />
              <span class="text-xs">Loading history...</span>
            </div>
          {:else if sessions.length === 0}
            <div class="p-6 text-center text-secondary text-xs">
              No health check sessions recorded yet.
            </div>
          {:else}
            {#each sessions as s (s.id)}
              <button
                class="w-full text-left p-3.5 hover:bg-[var(--frame-bg)] transition-colors flex items-center justify-between group {selectedSessionId === s.id ? 'bg-orange-500/5 border-l-4 border-orange-500' : ''}"
                onclick={() => fetchSessionDetails(s.id)}
              >
                <div class="space-y-1">
                  <div class="flex items-center gap-2">
                    <span class="font-mono font-bold text-xs text-[var(--text-primary)]">
                      {s.id.slice(0, 8)}
                    </span>
                    <span class="text-[10px] px-1.5 py-0.5 rounded font-semibold uppercase bg-[var(--frame-bg)] text-secondary border border-[var(--border-color)]">
                      {s.trigger_type}
                    </span>
                  </div>
                  <div class="text-[11px] text-secondary">
                    {new Date(s.started_at).toLocaleString()}
                  </div>
                  <div class="flex items-center gap-3 text-xs pt-1">
                    <span class="text-emerald-600 dark:text-emerald-400 font-semibold">{s.passed_count} passed</span>
                    <span class="text-rose-600 dark:text-rose-400 font-semibold">{s.failed_count} failed</span>
                    <span class="text-secondary">{s.avg_latency_ms ? s.avg_latency_ms.toFixed(0) : 0}ms</span>
                  </div>
                </div>
                <ChevronRight size={16} class="text-secondary group-hover:text-orange-500 transition-colors" />
              </button>
            {/each}
          {/if}
        </div>
      </div>
    </div>
  </div>
</div>
