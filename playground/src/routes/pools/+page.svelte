<script>
  import { onMount } from 'svelte';
  import { 
    Cpu, Plus, RefreshCw, Shield, AlertTriangle, Trash2, Pencil, X, Sparkles,
    ArrowLeft, Search, ChevronDown, ChevronUp, Play, CheckCircle, XCircle, Heart
  } from '@lucide/svelte';
  import { appState } from '$lib/state.svelte.js';
  import Button from '$lib/components/Button.svelte';
  import Input from '$lib/components/Input.svelte';
  import Card from '$lib/components/Card.svelte';
  import Modal from '$lib/components/Modal.svelte';

  const CAPABILITY_BADGES = {
    reasoning:        { label: 'Reasoning',  color: '#a78bfa', bg: 'rgba(167,139,250,0.12)', border: 'rgba(167,139,250,0.3)' },
    vision:           { label: 'Vision',     color: '#60a5fa', bg: 'rgba(96,165,250,0.12)',  border: 'rgba(96,165,250,0.3)'  },
    image_generation: { label: 'Image Gen',  color: '#f97316', bg: 'rgba(249,115,22,0.12)',  border: 'rgba(249,115,22,0.3)'  },
    audio:            { label: 'Audio',      color: '#34d399', bg: 'rgba(52,211,153,0.12)',   border: 'rgba(52,211,153,0.3)'  },
    code:             { label: 'Code',       color: '#fbbf24', bg: 'rgba(251,191,36,0.12)',   border: 'rgba(251,191,36,0.3)'  },
    embedding:        { label: 'Embed',      color: '#9ca3af', bg: 'rgba(156,163,175,0.12)',  border: 'rgba(156,163,175,0.3)' },
  };

  function capabilityKeys(caps) {
    if (!caps) return [];
    return Object.keys(caps).filter(k => caps[k] && CAPABILITY_BADGES[k]);
  }

  // ─── Local State ──────────────────────────────────────────────────────────
  let pools = $state([]);
  let loading = $state(false);
  let error = $state('');

  // Details View state
  let selectedPool = $state(null);
  let poolDetails = $state(null);
  let poolLogs = $state([]);
  let logsLoading = $state(false);
  let logsFilters = $state({
    tenant_id: '',
    status: '',
    search: '',
    semantic_query: '',
    use_semantic: false
  });
  let logsPage = $state(0);
  const logsLimit = 15;
  let testingCredId = $state(null);
  let expandedLogId = $state(null);

  // Add modal state
  let showAddModal = $state(false);
  let addForm = $state({ model_pattern: '', strategy: 'round-robin', fallback_pool_id: '' });
  let addLoading = $state(false);

  // Edit modal state
  let showEditModal = $state(false);
  let editForm = $state({ id: 0, model_pattern: '', strategy: 'round-robin', fallback_pool_id: '' });
  let editLoading = $state(false);

  // Delete modal state
  let showDeleteConfirm = $state(false);
  let deleteTargetId = $state(null);
  let deleteLoading = $state(false);

  // Auto-fetch when adminKey changes
  $effect(() => {
    if (appState.adminKey.trim() && !selectedPool) {
      loadPools();
    }
  });

  // ─── API Helper Headers ───────────────────────────────────────────────────
  function adminHeaders() {
    return {
      'Authorization': `Bearer ${appState.adminKey.trim()}`,
      'Content-Type': 'application/json'
    };
  }

  async function loadPools() {
    loading = true;
    error = '';
    try {
      const res = await fetch('/api/v1/admin/pools', { headers: adminHeaders() });
      if (res.ok) {
        pools = await res.json();
      } else {
        const err = await res.json();
        error = err.error || `Error ${res.status}`;
      }
    } catch (e) {
      error = `Network error: ${e.message}`;
    } finally {
      loading = false;
    }
  }

  // ─── Details View Methods ───────────────────────────────────────────────
  async function loadPoolDetails(poolId) {
    try {
      const res = await fetch(`/api/v1/admin/pools/${poolId}`, { headers: adminHeaders() });
      if (res.ok) {
        poolDetails = await res.json();
      } else {
        appState.addToast('error', 'Failed to load pool credentials');
      }
    } catch (e) {
      appState.addToast('error', `Error loading credentials: ${e.message}`);
    }
  }

  async function loadPoolLogs(poolId, append = false) {
    logsLoading = true;
    try {
      const params = new URLSearchParams();
      params.append('limit', String(logsLimit));
      params.append('offset', String(logsPage * logsLimit));
      if (logsFilters.tenant_id.trim()) params.append('tenant_id', logsFilters.tenant_id.trim());
      if (logsFilters.status) params.append('status', logsFilters.status);
      if (logsFilters.search.trim()) params.append('search', logsFilters.search.trim());
      if (logsFilters.use_semantic && logsFilters.semantic_query.trim()) {
        params.append('semantic_query', logsFilters.semantic_query.trim());
      }

      const res = await fetch(`/api/v1/admin/pools/${poolId}/logs?${params.toString()}`, { headers: adminHeaders() });
      if (res.ok) {
        const data = await res.json();
        if (append) {
          poolLogs = [...poolLogs, ...data];
        } else {
          poolLogs = data;
        }
      } else {
        appState.addToast('error', 'Failed to load pool logs');
      }
    } catch (e) {
      appState.addToast('error', `Error loading logs: ${e.message}`);
    } finally {
      logsLoading = false;
    }
  }

  async function openPoolDetails(pool) {
    selectedPool = pool;
    poolDetails = null;
    poolLogs = [];
    logsPage = 0;
    expandedLogId = null;
    logsFilters = { tenant_id: '', status: '', search: '', semantic_query: '', use_semantic: false };
    await Promise.all([
      loadPoolDetails(pool.id),
      loadPoolLogs(pool.id)
    ]);
  }

  async function testCredential(cred) {
    testingCredId = cred.id;
    appState.addToast('info', `Testing credential health for ${cred.provider}...`);
    try {
      const res = await fetch(`/api/v1/admin/pools/${selectedPool.id}/credentials/${cred.id}/test`, {
        method: 'POST',
        headers: adminHeaders()
      });
      if (res.ok) {
        const data = await res.json();
        if (data.is_healthy) {
          appState.addToast('success', `Credential check succeeded: ${cred.provider} is healthy!`);
        } else {
          appState.addToast('error', `Credential check failed: ${data.error || 'Rejected by provider'}`);
        }
        await loadPoolDetails(selectedPool.id);
      } else {
        const err = await res.json();
        appState.addToast('error', err.error || 'Failed to run health check');
      }
    } catch (e) {
      appState.addToast('error', `Network error: ${e.message}`);
    } finally {
      testingCredId = null;
    }
  }

  async function toggleCredentialHealth(cred) {
    try {
      const payload = {
        provider: cred.provider,
        base_url: cred.base_url,
        weight: cred.weight,
        is_healthy: !cred.is_healthy
      };
      const res = await fetch(`/api/v1/admin/credentials/${cred.id}`, {
        method: 'PUT',
        headers: adminHeaders(),
        body: JSON.stringify(payload)
      });
      if (res.ok) {
        appState.addToast('success', `Credential health toggled successfully`);
        await loadPoolDetails(selectedPool.id);
      } else {
        const err = await res.json();
        appState.addToast('error', err.error || 'Failed to update health');
      }
    } catch (e) {
      appState.addToast('error', `Network error: ${e.message}`);
    }
  }

  function handleFilterChange() {
    logsPage = 0;
    expandedLogId = null;
    loadPoolLogs(selectedPool.id);
  }

  function loadMoreLogs() {
    logsPage += 1;
    loadPoolLogs(selectedPool.id, true);
  }

  // ─── CRUD Methods ──────────────────────────────────────────────────────────
  function openAddModal() {
    addForm = { model_pattern: '', strategy: 'round-robin', fallback_pool_id: '' };
    showAddModal = true;
  }

  async function createPool() {
    if (!addForm.model_pattern.trim()) {
      appState.addToast('error', 'Model pattern is required');
      return;
    }
    addLoading = true;
    try {
      const payload = {
        model_pattern: addForm.model_pattern,
        strategy: addForm.strategy,
        fallback_pool_id: addForm.fallback_pool_id ? Number(addForm.fallback_pool_id) : undefined
      };
      const res = await fetch('/api/v1/admin/pools', {
        method: 'POST',
        headers: adminHeaders(),
        body: JSON.stringify(payload)
      });
      if (res.status === 201 || res.ok) {
        appState.addToast('success', 'Model pool created successfully');
        showAddModal = false;
        loadPools();
      } else {
        const err = await res.json();
        appState.addToast('error', err.details || err.error || 'Failed to create pool');
      }
    } catch (e) {
      appState.addToast('error', `Network error: ${e.message}`);
    } finally {
      addLoading = false;
    }
  }

  function openEditModal(pool, event) {
    event.stopPropagation();
    editForm = {
      id: pool.id,
      model_pattern: pool.model_pattern,
      strategy: pool.strategy,
      fallback_pool_id: pool.fallback_pool_id !== null && pool.fallback_pool_id !== undefined ? String(pool.fallback_pool_id) : ''
    };
    showEditModal = true;
  }

  async function updatePool() {
    if (!editForm.model_pattern.trim()) {
      appState.addToast('error', 'Model pattern is required');
      return;
    }
    editLoading = true;
    try {
      const payload = {
        model_pattern: editForm.model_pattern,
        strategy: editForm.strategy,
        fallback_pool_id: editForm.fallback_pool_id ? Number(editForm.fallback_pool_id) : null
      };
      const res = await fetch(`/api/v1/admin/pools/${editForm.id}`, {
        method: 'PUT',
        headers: adminHeaders(),
        body: JSON.stringify(payload)
      });
      if (res.ok) {
        appState.addToast('success', 'Model pool updated successfully');
        showEditModal = false;
        if (selectedPool && selectedPool.id === editForm.id) {
          selectedPool.model_pattern = editForm.model_pattern;
          selectedPool.strategy = editForm.strategy;
          selectedPool.fallback_pool_id = payload.fallback_pool_id;
        }
        loadPools();
      } else {
        const err = await res.json();
        appState.addToast('error', err.details || err.error || 'Failed to update pool');
      }
    } catch (e) {
      appState.addToast('error', `Network error: ${e.message}`);
    } finally {
      editLoading = false;
    }
  }

  function confirmDelete(id, event) {
    event.stopPropagation();
    deleteTargetId = id;
    showDeleteConfirm = true;
  }

  async function deletePoolById() {
    deleteLoading = true;
    try {
      const res = await fetch(`/api/v1/admin/pools/${deleteTargetId}`, {
        method: 'DELETE',
        headers: adminHeaders()
      });
      if (res.ok) {
        appState.addToast('success', 'Model pool deleted successfully');
        showDeleteConfirm = false;
        deleteTargetId = null;
        if (selectedPool && selectedPool.id === deleteTargetId) {
          selectedPool = null;
        }
        loadPools();
      } else {
        const err = await res.json();
        appState.addToast('error', err.details || err.error || 'Failed to delete pool');
      }
    } catch (e) {
      appState.addToast('error', `Network error: ${e.message}`);
    } finally {
      deleteLoading = false;
    }
  }

  function connectAdminKey() {
    const key = appState.adminKey.trim();
    if (!key) return;
    localStorage.setItem('cag_admin_key', key);
    loadPools();
  }

  onMount(() => {
    if (appState.adminKey.trim() && !selectedPool) {
      loadPools();
    }
  });
</script>

<header class="header flex items-center justify-between px-6 py-4 border-b shrink-0">
  <div class="flex items-center gap-3">
    {#if selectedPool}
      <Button variant="ghost" size="sm" onclick={() => { selectedPool = null; loadPools(); }} title="Back to pools list">
        <ArrowLeft size={16} />
      </Button>
      <Cpu size={20} class="text-[#f97316]" />
      <span class="font-bold text-base">Pool Details: <span class="text-[#f97316] font-mono">{selectedPool.model_pattern}</span></span>
    {:else}
      <Cpu size={20} class="text-[#f97316]" />
      <span class="font-bold text-base">Model Routing Pools</span>
      {#if appState.adminKey.trim()}
        <span class="text-xs font-bold text-secondary bg-gray-500/10 border border-gray-500/20 px-2.5 py-0.5 rounded-full uppercase">{pools.length} pools</span>
      {/if}
    {/if}
  </div>
  <div class="flex items-center gap-2">
    {#if appState.adminKey.trim()}
      {#if selectedPool}
        <Button variant="secondary" size="sm" onclick={() => { loadPoolDetails(selectedPool.id); loadPoolLogs(selectedPool.id); appState.addToast('success', 'Refreshed details and history logs'); }}>
          <RefreshCw size={14} />
          Refresh Details
        </Button>
      {:else}
        <Button variant="secondary" size="sm" onclick={() => { loadPools(); appState.addToast('info', 'Refreshing capabilities...'); }} title="Re-classify all model capabilities">
          <Sparkles size={14} />
          Refresh Capabilities
        </Button>
        <Button variant="secondary" size="sm" onclick={() => { loadPools(); appState.addToast('info', 'Refreshing pools list...'); }}>
          <RefreshCw size={14} />
          Refresh
        </Button>
        <Button variant="primary" size="sm" onclick={openAddModal}>
          <Plus size={14} />
          Create Pool
        </Button>
      {/if}
    {/if}
  </div>
</header>

{#if !appState.adminKey.trim()}
  <!-- Admin key prompt -->
  <div class="logs-key-prompt flex flex-col justify-center items-center flex-grow p-6">
    <Card variant="filled" padding="lg" class="logs-key-card flex flex-col items-center text-center">
      <Shield size={40} class="text-[#f97316] mb-4 animate-pulse" />
      <h2 class="font-bold text-lg mb-2 text-primary">Admin Key Required</h2>
      <p class="text-sm mb-6 text-secondary max-w-sm">Enter your Admin API Key to manage model routing pools, load-balancing strategy patterns, and fallback systems.</p>
      <div class="flex flex-col gap-3 w-full max-w-sm">
        <Input
          type="password"
          placeholder="Enter Admin API Key..."
          bind:value={appState.adminKey}
          onkeydown={(e) => { if (e.key === 'Enter') connectAdminKey(); }}
        />
        <Button variant="primary" size="md" onclick={connectAdminKey}>
          Connect
        </Button>
      </div>
      {#if error}
        <p class="text-red-500 text-sm font-semibold mt-4">{error}</p>
      {/if}
    </Card>
  </div>
{:else if selectedPool}
  <!-- POOL DETAILS & LOGS PAGE VIEW -->
  <div class="detail-page-container flex flex-col gap-6 p-6 overflow-y-auto w-full h-full">
    
    <!-- Row 1: Credentials / Pool Members Card -->
    <Card variant="filled" padding="lg" class="glass-card">
      <div class="flex flex-col md:flex-row md:items-center justify-between gap-4 mb-4">
        <div class="flex flex-col">
          <h3 class="font-bold text-base text-primary">Active Members ({poolDetails?.credentials?.length || 0})</h3>
          <p class="text-xs text-secondary mt-0.5">Individual keys assigned to this routing pool. Strategy: <span class="font-bold text-[#f97316]">{selectedPool.strategy}</span></p>
        </div>
        <div class="flex flex-wrap gap-1.5 animate-fade-in">
          {#each capabilityKeys(selectedPool.capabilities) as key}
            {@const badge = CAPABILITY_BADGES[key]}
            <span style="display:inline-block; padding:3px 8px; border-radius:6px; font-size:10px; font-weight:700; text-transform:uppercase; letter-spacing:0.04em; color:{badge.color}; background:{badge.bg}; border:1px solid {badge.border};">
              {badge.label}
            </span>
          {/each}
        </div>
      </div>

      {#if !poolDetails}
        <div class="flex items-center justify-center py-8 text-sm text-secondary opacity-60">
          <span class="animate-spin mr-2">⟳</span> Fetching pool keys...
        </div>
      {:else if poolDetails.credentials?.length === 0}
        <div class="flex flex-col items-center justify-center py-10 text-center border-2 border-dashed border-[var(--border-color)] rounded-xl">
          <Cpu size={32} class="opacity-20 mb-3" />
          <p class="text-sm font-semibold opacity-60">No API keys registered for this pool.</p>
          <p class="text-xs text-secondary mt-1 max-w-xs">Visit the "Credentials" tab to link API keys to pool ID #{selectedPool.id}.</p>
        </div>
      {:else}
        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {#each poolDetails.credentials as cred}
            <Card variant="filled" padding="sm" class="member-card relative hover:border-[#f97316] transition-all">
              <div class="flex items-center justify-between gap-4 mb-2">
                <span class="provider-badge text-xs font-bold py-1 px-2.5 rounded-lg {cred.provider === 'openai' ? 'badge-openai' : cred.provider === 'nvidia' ? 'badge-nvidia' : 'badge-default'}">
                  {cred.provider}
                </span>
                
                <div class="flex items-center gap-3">
                  <span 
                    class="health-dot {cred.is_healthy ? 'pulse-healthy' : 'health-unhealthy'}" 
                    title={cred.is_healthy ? 'Key status: Healthy' : `Unhealthy check: ${cred.last_error || 'No message'}`}
                  ></span>
                  
                  <label class="toggle-switch" title="Manually enable/disable key">
                    <input 
                      type="checkbox" 
                      checked={cred.is_healthy} 
                      onchange={() => toggleCredentialHealth(cred)}
                    />
                    <span class="toggle-slider"></span>
                  </label>
                </div>
              </div>

              <!-- Base URL and Weight -->
              <div class="flex flex-col gap-1 mt-2 font-mono text-xs text-secondary">
                <div class="truncate">Base: <span class="text-primary font-medium">{cred.base_url}</span></div>
                <div>Weight: <span class="text-primary font-bold">{cred.weight}</span></div>
              </div>

              <!-- Last Error Message if unhealthy -->
              {#if !cred.is_healthy && cred.last_error}
                <div class="text-xs text-red-500 bg-red-500/10 border border-red-500/20 p-2.5 rounded-lg leading-relaxed mt-2.5 flex items-start gap-1.5">
                  <AlertTriangle size={14} class="shrink-0 mt-0.5" />
                  <span class="break-all">{cred.last_error}</span>
                </div>
              {/if}

              <!-- Test health button -->
              <div class="flex justify-end mt-3 pt-3 border-t border-[var(--border-color)]">
                <Button 
                  variant="outline" 
                  size="sm"
                  onclick={() => testCredential(cred)}
                  disabled={testingCredId === cred.id}
                >
                  {#if testingCredId === cred.id}
                    <span class="animate-spin text-xs">⟳</span> Testing
                  {:else}
                    <Heart size={12} /> Test Health
                  {/if}
                </Button>
              </div>
            </Card>
          {/each}
        </div>
      {/if}
    </Card>

    <!-- Row 2: Logs and History Viewer -->
    <Card variant="filled" padding="lg" class="glass-card">
      <div class="flex flex-col gap-1 mb-4">
        <h3 class="font-bold text-base text-primary">Pool Request History & Telemetry</h3>
        <p class="text-xs text-secondary">Audit and monitor incoming calls routed to this model pool. Features full-text semantic visualizer.</p>
      </div>

      <!-- Filters Toolbar -->
      <div class="grid grid-cols-1 md:grid-cols-4 gap-4 bg-[var(--sidebar-bg)] p-4 border rounded-xl mb-4 text-primary">
        <Input 
          type="text" 
          label="Tenant Filter"
          placeholder="Tenant API key or UUID..."
          bind:value={logsFilters.tenant_id}
          onchange={handleFilterChange}
        />
        
        <Input 
          type="select" 
          label="Status Filter"
          bind:value={logsFilters.status}
          onchange={handleFilterChange}
        >
          <option value="">All Outcomes</option>
          <option value="success">Success (2xx)</option>
          <option value="error">Errors (4xx/5xx)</option>
        </Input>

        <Input 
          type="text" 
          label="Keyword Search"
          placeholder="Query string match..."
          bind:value={logsFilters.search}
          onchange={handleFilterChange}
        />
        
        <!-- Semantic vector search toggle -->
        <div class="flex flex-col gap-1">
          <div class="flex items-center justify-between mb-1">
            <span class="text-xs font-bold uppercase tracking-wider text-secondary">Semantic AI Search</span>
            <label class="flex items-center gap-1.5 cursor-pointer">
              <input 
                type="checkbox" 
                class="log-checkbox w-4 h-4 rounded border-gray-300 accent-orange-500" 
                bind:checked={logsFilters.use_semantic} 
                onchange={handleFilterChange}
              />
              <span class="text-xs font-bold uppercase tracking-wider text-[#a78bfa]">Enable</span>
            </label>
          </div>
          <div class="relative">
            <input 
              type="text" 
              class="input-box p-3 pl-9 text-sm rounded-xl border w-full {logsFilters.use_semantic ? 'border-[#a78bfa] focus:border-[#a78bfa]' : ''}" 
              placeholder="Search meaning..."
              bind:value={logsFilters.semantic_query}
              disabled={!logsFilters.use_semantic}
              onkeydown={(e) => { if (e.key === 'Enter') handleFilterChange(); }}
            />
            <Sparkles size={14} class="absolute left-3 top-3.5 {logsFilters.use_semantic ? 'text-[#a78bfa]' : 'opacity-30'}" />
          </div>
        </div>
      </div>

      <!-- Logs Data Table -->
      <div class="logs-table-wrapper border rounded-xl overflow-x-auto">
        <table class="providers-table w-full">
          <thead>
            <tr>
              <th class="w-20" style="font-size: 11px;">Status</th>
              <th style="font-size: 11px;">Time</th>
              <th style="font-size: 11px;">Tenant</th>
              <th style="font-size: 11px;">Provider Used</th>
              <th style="font-size: 11px;">Model Version</th>
              <th style="font-size: 11px;">Latency</th>
              <th style="font-size: 11px;">Tokens (P/C)</th>
              {#if logsFilters.use_semantic}
                <th class="w-28" style="font-size: 11px;">Similarity</th>
              {/if}
              <th class="w-16 text-center" style="font-size: 11px;">Inspect</th>
            </tr>
          </thead>
          <tbody>
            {#if logsLoading && poolLogs.length === 0}
              <tr>
                <td colspan={logsFilters.use_semantic ? 9 : 8} class="text-center py-10 text-sm text-secondary opacity-60">
                  <span class="animate-spin inline-block mr-2">⟳</span> Fetching logs...
                </td>
              </tr>
            {:else if poolLogs.length === 0}
              <tr>
                <td colspan={logsFilters.use_semantic ? 9 : 8} class="text-center py-10 text-sm opacity-50 text-secondary">
                  No request history matched the active filters.
                </td>
              </tr>
            {:else}
              {#each poolLogs as log (log.id)}
                <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <tr 
                  class="provider-row cursor-pointer select-text {expandedLogId === log.id ? 'bg-[#f97316]/5 border-l-2 border-l-[#f97316]' : ''}"
                  onclick={() => expandedLogId = expandedLogId === log.id ? null : log.id}
                >
                  <td>
                    {#if log.status_code >= 200 && log.status_code < 400}
                      <span class="flex items-center gap-1 text-[#10b981] font-bold text-xs">
                        <CheckCircle size={14} /> {log.status_code}
                      </span>
                    {:else}
                      <span class="flex items-center gap-1 text-[#ef4444] font-bold text-xs">
                        <XCircle size={14} /> {log.status_code}
                      </span>
                    {/if}
                  </td>
                  <td class="font-mono text-xs opacity-60 whitespace-nowrap">
                    {new Date(log.created_at).toLocaleTimeString()} {new Date(log.created_at).toLocaleDateString()}
                  </td>
                  <td class="font-bold text-xs truncate max-w-[130px]" title={log.tenant_id}>
                    {log.tenant_name || log.tenant_id || 'System'}
                  </td>
                  <td>
                    <span class="provider-badge text-[10px] font-bold py-1 px-2 rounded-lg {log.provider === 'openai' ? 'badge-openai' : log.provider === 'nvidia' ? 'badge-nvidia' : 'badge-default'}">
                      {log.provider}
                    </span>
                  </td>
                  <td class="font-mono text-xs text-secondary truncate max-w-[160px]" title={log.model}>
                    {log.model}
                  </td>
                  <td class="font-mono text-sm font-semibold text-primary">{log.latency_ms}ms</td>
                  <td class="font-mono text-xs text-secondary">
                    {log.prompt_tokens} / <span class="text-primary">{log.completion_tokens}</span>
                  </td>
                  {#if logsFilters.use_semantic}
                    <td>
                      {#if log.similarity !== undefined && log.similarity !== 0}
                        <span style="display:inline-block; padding:2px 8px; border-radius:6px; font-size:10px; font-weight:800; color:#a78bfa; background:rgba(167,139,250,0.1); border:1px solid rgba(167,139,250,0.3)">
                          {(log.similarity * 100).toFixed(1)}% match
                        </span>
                      {:else}
                        <span class="text-xs opacity-25">—</span>
                      {/if}
                    </td>
                  {/if}
                  <td class="text-center">
                    <Button variant="ghost" size="sm" class="p-1">
                      {#if expandedLogId === log.id}
                        <ChevronUp size={16} />
                      {:else}
                        <ChevronDown size={16} />
                      {/if}
                    </Button>
                  </td>
                </tr>

                <!-- Expanded Log disclosure box -->
                {#if expandedLogId === log.id}
                  <tr class="bg-[var(--sidebar-bg)] border-b">
                    <td colspan={logsFilters.use_semantic ? 9 : 8} class="p-5 select-text">
                      <div class="flex flex-col gap-4 max-w-full text-sm text-primary">
                        
                        <!-- Error block -->
                        {#if log.error_message}
                          <div class="flex flex-col gap-1.5 border border-red-500/20 bg-red-500/5 p-4 rounded-xl">
                            <span class="font-bold text-red-500 text-xs uppercase tracking-wider">Error Details</span>
                            <pre class="font-mono text-xs text-red-400 whitespace-pre-wrap break-all leading-normal">{log.error_message}</pre>
                          </div>
                        {/if}

                        <div class="grid grid-cols-1 md:grid-cols-2 gap-4 text-primary">
                          <!-- Prompt Panel -->
                          <div class="flex flex-col gap-2">
                            <span class="font-bold text-secondary text-xs uppercase tracking-wider">Prompt Payload</span>
                            <div class="bg-[var(--frame-bg)] border p-4 rounded-xl font-mono text-xs leading-relaxed break-words max-h-56 overflow-y-auto whitespace-pre-wrap">
                              {log.prompt_text || 'Empty prompt content / body.'}
                            </div>
                          </div>

                          <!-- Response Panel -->
                          <div class="flex flex-col gap-2">
                            <span class="font-bold text-secondary text-xs uppercase tracking-wider">Response Content</span>
                            <div class="bg-[var(--frame-bg)] border p-4 rounded-xl font-mono text-xs leading-relaxed break-words max-h-56 overflow-y-auto whitespace-pre-wrap">
                              {log.response_text || 'No response returned / streamed.'}
                            </div>
                          </div>
                        </div>

                      </div>
                    </td>
                  </tr>
                {/if}
              {/each}
            {/if}
          </tbody>
        </table>
      </div>

      <!-- Load more logs button -->
      {#if poolLogs.length > 0 && poolLogs.length % logsLimit === 0}
        <div class="flex justify-center mt-3 animate-fade-in">
          <Button 
            variant="outline" 
            onclick={loadMoreLogs}
            disabled={logsLoading}
          >
            {#if logsLoading}
              <span class="animate-spin text-sm">⟳</span> Loading
            {:else}
              Load More History Logs
            {/if}
          </Button>
        </div>
      {/if}

    </Card>
  </div>
{:else}
  <!-- Pools data grid -->
  <div class="providers-grid-wrap flex-grow overflow-auto p-6">
    {#if loading}
      <div class="providers-loading flex flex-col items-center justify-center h-64">
        <div class="animate-spin text-[#f97316] text-xl">⟳</div>
        <p class="text-sm mt-2 text-secondary">Loading routing pools...</p>
      </div>
    {:else if error}
      <div class="providers-loading flex flex-col items-center justify-center h-64">
        <AlertTriangle size={40} class="text-red-500 mb-2" />
        <p class="text-red-500 text-sm font-semibold">{error}</p>
        <Button variant="primary" class="mt-4" onclick={loadPools}>Retry</Button>
      </div>
    {:else if pools.length === 0}
      <div class="providers-loading flex flex-col items-center justify-center h-64">
        <Cpu size={48} class="opacity-20 mb-4" />
        <p class="opacity-50 text-sm text-secondary">No model pools registered yet.</p>
        <Button variant="primary" class="mt-4" onclick={openAddModal}>
          <Plus size={14} /> Create First Pool
        </Button>
      </div>
    {:else}
      <div class="providers-table-container">
        <table class="providers-table">
          <thead>
            <tr>
              <th style="font-size: 11px;">ID</th>
              <th style="font-size: 11px;">Model Pattern</th>
              <th style="font-size: 11px;">Capabilities</th>
              <th style="font-size: 11px;">Strategy</th>
              <th style="font-size: 11px;">Fallback Pool ID</th>
              <th style="font-size: 11px; text-align: center;">Credentials</th>
              <th style="font-size: 11px; text-align: center;">Actions</th>
            </tr>
          </thead>
          <tbody>
            {#each pools as pool (pool.id)}
              <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <tr class="provider-row cursor-pointer" onclick={() => openPoolDetails(pool)}>
                <td class="font-mono text-xs opacity-60">#{pool.id}</td>
                <td class="font-bold text-sm text-[#f97316]">{pool.model_pattern}</td>
                <td>
                  <div class="flex flex-wrap gap-1">
                    {#each capabilityKeys(pool.capabilities) as key}
                      {@const badge = CAPABILITY_BADGES[key]}
                      <span style="display:inline-block; padding:2px 7px; border-radius:5px; font-size:10px; font-weight:700; text-transform:uppercase; letter-spacing:0.04em; color:{badge.color}; background:{badge.bg}; border:1px solid {badge.border};">
                        {badge.label}
                      </span>
                    {/each}
                    {#if capabilityKeys(pool.capabilities).length === 0}
                      <span class="text-xs opacity-30">—</span>
                    {/if}
                  </div>
                </td>
                <td>
                  <span class="provider-badge {pool.strategy === 'round-robin' ? 'badge-openai' : 'badge-anthropic'}">
                    {pool.strategy}
                  </span>
                </td>
                <td class="font-mono text-sm">{pool.fallback_pool_id !== null && pool.fallback_pool_id !== undefined ? `#${pool.fallback_pool_id}` : '—'}</td>
                <td class="font-mono text-sm text-center">{pool.credential_count || 0} keys</td>
                <td>
                  <div class="flex items-center justify-center gap-1">
                    <Button variant="ghost" size="sm" onclick={(e) => openEditModal(pool, e)} title="Edit pool">
                      <Pencil size={15} />
                    </Button>
                    <Button variant="ghost" size="sm" onclick={(e) => confirmDelete(pool.id, e)} title="Delete pool">
                      <Trash2 size={15} class="text-red-500" />
                    </Button>
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </div>
{/if}

<!-- ─── CREATE POOL MODAL ──────────────────────────────────────────────────── -->
<Modal bind:show={showAddModal} title="Create Model Pool">
  <div class="flex flex-col gap-4 text-primary">
    <Input 
      type="text" 
      label="Model Pattern" 
      placeholder="e.g. gpt-4o, claude-3-5-sonnet*" 
      bind:value={addForm.model_pattern} 
    />
    
    <Input type="select" label="Routing Strategy" bind:value={addForm.strategy}>
      <option value="round-robin">round-robin</option>
      <option value="weighted-round-robin">weighted-round-robin</option>
      <option value="random">random</option>
    </Input>

    <Input type="select" label="Fallback Pool (Optional)" bind:value={addForm.fallback_pool_id} placeholder="None">
      {#each pools as otherPool}
        <option value={otherPool.id}>{otherPool.model_pattern} (ID: {otherPool.id})</option>
      {/each}
    </Input>
  </div>

  {#snippet footer()}
    <div class="flex justify-end gap-3 w-full">
      <Button variant="outline" onclick={() => showAddModal = false}>Cancel</Button>
      <Button variant="primary" onclick={createPool} disabled={addLoading}>
        {#if addLoading}
          <span class="animate-spin">⟳</span> Creating...
        {:else}
          Create Pool
        {/if}
      </Button>
    </div>
  {/snippet}
</Modal>

<!-- ─── EDIT POOL MODAL ────────────────────────────────────────────────────── -->
<Modal bind:show={showEditModal} title="Edit Model Pool">
  <div class="flex flex-col gap-4 text-primary">
    <Input 
      type="text" 
      label="Model Pattern" 
      bind:value={editForm.model_pattern} 
    />
    
    <Input type="select" label="Routing Strategy" bind:value={editForm.strategy}>
      <option value="round-robin">round-robin</option>
      <option value="weighted-round-robin">weighted-round-robin</option>
      <option value="random">random</option>
    </Input>

    <Input type="select" label="Fallback Pool (Optional)" bind:value={editForm.fallback_pool_id} placeholder="None">
      {#each pools as otherPool}
        {#if otherPool.id !== editForm.id}
          <option value={String(otherPool.id)}>{otherPool.model_pattern} (ID: {otherPool.id})</option>
        {/if}
      {/each}
    </Input>
  </div>

  {#snippet footer()}
    <div class="flex justify-end gap-3 w-full">
      <Button variant="outline" onclick={() => showEditModal = false}>Cancel</Button>
      <Button variant="primary" onclick={updatePool} disabled={editLoading}>
        {#if editLoading}
          <span class="animate-spin">⟳</span> Saving...
        {:else}
          Save Changes
        {/if}
      </Button>
    </div>
  {/snippet}
</Modal>

<!-- ─── DELETE CONFIRMATION DIALOG ─────────────────────────────────────────── -->
<Modal bind:show={showDeleteConfirm} title="Delete Model Pool?">
  <div class="flex flex-col items-center gap-4 text-center">
    <AlertTriangle size={48} class="text-red-500 mb-2" />
    <p class="text-sm text-secondary">
      All routing configurations and associated provider keys assigned to this model pool will be deleted. Upstream traffic will fallback or fail.
    </p>
    <p class="text-xs text-red-500 font-bold">This action is permanent and cannot be undone.</p>
  </div>

  {#snippet footer()}
    <div class="flex justify-center gap-3 w-full">
      <Button variant="outline" onclick={() => { showDeleteConfirm = false; deleteTargetId = null; }}>Cancel</Button>
      <Button variant="danger" onclick={deletePoolById} disabled={deleteLoading}>
        {#if deleteLoading}
          <span class="animate-spin">⟳</span>
        {:else}
          Delete Pool
        {/if}
      </Button>
    </div>
  {/snippet}
</Modal>

<style>
  .providers-table th {
    padding: 14px 18px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    font-weight: 700;
    color: var(--text-secondary);
    border-bottom: 2px solid var(--border-color);
  }
  .providers-table td {
    padding: 14px 18px;
    border-bottom: 1px solid var(--border-color);
  }

  .pulse-healthy {
    background-color: #10b981;
    box-shadow: 0 0 8px rgba(16, 185, 129, 0.6);
    animation: health-pulse 2.2s infinite;
  }
  
  .health-unhealthy {
    background-color: #ef4444;
    box-shadow: 0 0 8px rgba(239, 68, 68, 0.6);
  }

  .health-dot {
    display: inline-block;
    width: 10px;
    height: 10px;
    border-radius: 50%;
  }

  @keyframes health-pulse {
    0% {
      transform: scale(0.95);
      box-shadow: 0 0 0 0 rgba(16, 185, 129, 0.7);
    }
    70% {
      transform: scale(1);
      box-shadow: 0 0 0 8px rgba(16, 185, 129, 0);
    }
    100% {
      transform: scale(0.95);
      box-shadow: 0 0 0 0 rgba(16, 185, 129, 0);
    }
  }

  .logs-table-wrapper {
    max-height: 520px;
    overflow-y: auto;
    border: 1px solid var(--border-color);
  }
</style>
