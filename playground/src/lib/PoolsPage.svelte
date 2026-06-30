<script>
  import { 
    Cpu, Plus, RefreshCw, Shield, AlertTriangle, Trash2, Pencil, X, Sparkles,
    ArrowLeft, Search, ChevronDown, ChevronUp, Play, CheckCircle, XCircle, Heart
  } from '@lucide/svelte';

  // Capability badge config — colour + label for each flag
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

  let { 
    adminKey = $bindable(''), 
    addToast 
  } = $props();

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
    if (adminKey.trim() && !selectedPool) {
      loadPools();
    }
  });

  // ─── API Helper Headers ───────────────────────────────────────────────────
  function adminHeaders() {
    return {
      'Authorization': `Bearer ${adminKey.trim()}`,
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
        addToast('error', 'Failed to load pool credentials');
      }
    } catch (e) {
      addToast('error', `Error loading credentials: ${e.message}`);
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
        addToast('error', 'Failed to load pool logs');
      }
    } catch (e) {
      addToast('error', `Error loading logs: ${e.message}`);
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
    addToast('info', `Testing credential health for ${cred.provider}...`);
    try {
      const res = await fetch(`/api/v1/admin/pools/${selectedPool.id}/credentials/${cred.id}/test`, {
        method: 'POST',
        headers: adminHeaders()
      });
      if (res.ok) {
        const data = await res.json();
        if (data.is_healthy) {
          addToast('success', `Credential check succeeded: ${cred.provider} is healthy!`);
        } else {
          addToast('error', `Credential check failed: ${data.error || 'Rejected by provider'}`);
        }
        await loadPoolDetails(selectedPool.id);
      } else {
        const err = await res.json();
        addToast('error', err.error || 'Failed to run health check');
      }
    } catch (e) {
      addToast('error', `Network error: ${e.message}`);
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
        addToast('success', `Credential health toggled successfully`);
        await loadPoolDetails(selectedPool.id);
      } else {
        const err = await res.json();
        addToast('error', err.error || 'Failed to update health');
      }
    } catch (e) {
      addToast('error', `Network error: ${e.message}`);
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
      addToast('error', 'Model pattern is required');
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
        addToast('success', 'Model pool created successfully');
        showAddModal = false;
        loadPools();
      } else {
        const err = await res.json();
        addToast('error', err.details || err.error || 'Failed to create pool');
      }
    } catch (e) {
      addToast('error', `Network error: ${e.message}`);
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
      addToast('error', 'Model pattern is required');
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
        addToast('success', 'Model pool updated successfully');
        showEditModal = false;
        if (selectedPool && selectedPool.id === editForm.id) {
          selectedPool.model_pattern = editForm.model_pattern;
          selectedPool.strategy = editForm.strategy;
          selectedPool.fallback_pool_id = payload.fallback_pool_id;
        }
        loadPools();
      } else {
        const err = await res.json();
        addToast('error', err.details || err.error || 'Failed to update pool');
      }
    } catch (e) {
      addToast('error', `Network error: ${e.message}`);
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
        addToast('success', 'Model pool deleted successfully');
        showDeleteConfirm = false;
        deleteTargetId = null;
        if (selectedPool && selectedPool.id === deleteTargetId) {
          selectedPool = null;
        }
        loadPools();
      } else {
        const err = await res.json();
        addToast('error', err.details || err.error || 'Failed to delete pool');
      }
    } catch (e) {
      addToast('error', `Network error: ${e.message}`);
    } finally {
      deleteLoading = false;
    }
  }

  function connectAdminKey() {
    const key = adminKey.trim();
    if (!key) return;
    localStorage.setItem('cag_admin_key', key);
    loadPools();
  }
</script>

<header class="header flex items-center justify-between px-6 py-3 border-b shrink-0">
  <div class="flex items-center gap-3">
    {#if selectedPool}
      <button class="icon-button" onclick={() => { selectedPool = null; loadPools(); }} title="Back to pools list">
        <ArrowLeft size={16} />
      </button>
      <Cpu size={18} class="text-[#f97316]" />
      <span class="font-bold text-sm">Pool Details: <span class="text-[#f97316] font-mono">{selectedPool.model_pattern}</span></span>
    {:else}
      <Cpu size={18} class="text-[#f97316]" />
      <span class="font-bold text-sm">Model Routing Pools</span>
      <span class="text-[10px] font-bold text-secondary uppercase">{pools.length} pools</span>
    {/if}
  </div>
  <div class="flex items-center gap-2">
    {#if adminKey.trim()}
      {#if selectedPool}
        <button class="log-action-btn log-btn-start" onclick={() => { loadPoolDetails(selectedPool.id); loadPoolLogs(selectedPool.id); addToast('success', 'Refreshed details and history logs'); }}>
          <RefreshCw size={12} />
          Refresh Details
        </button>
      {:else}
        <button class="log-action-btn" style="border-color: rgba(167,139,250,0.4); color: #a78bfa; background: rgba(167,139,250,0.06);" onclick={() => { loadPools(); addToast('info', 'Refreshing capabilities...'); }} title="Re-classify all model capabilities">
          <Sparkles size={12} />
          Refresh Capabilities
        </button>
        <button class="log-action-btn log-btn-start" onclick={() => { loadPools(); addToast('info', 'Refreshing pools list...'); }}>
          <RefreshCw size={12} />
          Refresh
        </button>
        <button class="log-action-btn" style="border-color: rgba(249,115,22,0.4); color: #f97316; background: rgba(249,115,22,0.06);" onclick={openAddModal}>
          <Plus size={12} />
          Create Pool
        </button>
      {/if}
    {/if}
  </div>
</header>

{#if !adminKey.trim()}
  <!-- Admin key prompt -->
  <div class="logs-key-prompt">
    <div class="logs-key-card">
      <Shield size={32} class="text-[#f97316] mb-3" />
      <h2 class="font-bold text-base mb-1">Admin Key Required</h2>
      <p class="text-xs mb-4">Enter your Admin API Key to manage model routing pools, load-balancing strategy patterns, and fallback systems.</p>
      <div class="flex gap-2 w-full max-w-sm">
        <input
          type="password"
          class="input-box flex-grow p-2.5 rounded-lg border text-sm"
          placeholder="Enter Admin API Key..."
          bind:value={adminKey}
          onkeydown={(e) => { if (e.key === 'Enter') connectAdminKey(); }}
        />
        <button class="px-4 py-2 rounded-lg text-white bg-[#f97316] font-semibold text-xs" onclick={connectAdminKey}>
          Connect
        </button>
      </div>
      {#if error}
        <p class="text-red-500 text-xs mt-3">{error}</p>
      {/if}
    </div>
  </div>
{:else if selectedPool}
  <!-- POOL DETAILS & LOGS PAGE VIEW -->
  <div class="detail-page-container flex flex-col gap-6 p-6 overflow-y-auto w-full h-full">
    
    <!-- Row 1: Credentials / Pool Members Card -->
    <div class="glass-card rounded-xl border p-5 flex flex-col gap-4">
      <div class="flex items-center justify-between">
        <div class="flex flex-col">
          <h3 class="font-bold text-sm text-primary">Active Members ({poolDetails?.credentials?.length || 0})</h3>
          <p class="text-[10px] text-secondary">Individual keys assigned to this routing pool. Strategy: <span class="font-bold text-[#f97316]">{selectedPool.strategy}</span></p>
        </div>
        <div class="flex gap-1.5">
          {#each capabilityKeys(selectedPool.capabilities) as key}
            {@const badge = CAPABILITY_BADGES[key]}
            <span style="display:inline-block; padding:1px 6px; border-radius:4px; font-size:9px; font-weight:700; text-transform:uppercase; letter-spacing:0.04em; color:{badge.color}; background:{badge.bg}; border:1px solid {badge.border};">
              {badge.label}
            </span>
          {/each}
        </div>
      </div>

      {#if !poolDetails}
        <div class="flex items-center justify-center py-6 text-xs text-secondary opacity-60">
          <span class="animate-spin mr-2">🔄</span> Fetching pool keys...
        </div>
      {:else if poolDetails.credentials?.length === 0}
        <div class="flex flex-col items-center justify-center py-8 text-center border-2 border-dashed border-[var(--border-color)] rounded-lg">
          <Cpu size={24} class="opacity-20 mb-2" />
          <p class="text-xs font-semibold opacity-50">No API keys registered for this pool.</p>
          <p class="text-[10px] text-secondary mt-1">Visit the "Credentials" tab to link keys to pool ID #{selectedPool.id}.</p>
        </div>
      {:else}
        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {#each poolDetails.credentials as cred}
            <div class="member-card rounded-lg border p-4 flex flex-col gap-2 relative bg-[var(--sidebar-bg)] hover:border-[#f97316] transition-all">
              <div class="flex items-center justify-between">
                <span class="provider-badge text-[10px] font-bold py-0.5 px-2 rounded {cred.provider === 'openai' ? 'badge-openai' : cred.provider === 'nvidia' ? 'badge-nvidia' : 'badge-default'}">
                  {cred.provider}
                </span>
                
                <div class="flex items-center gap-2">
                  <!-- Health indicator dot -->
                  <span 
                    class="health-dot {cred.is_healthy ? 'pulse-healthy' : 'health-unhealthy'}" 
                    title={cred.is_healthy ? 'Key status: Healthy' : `Unhealthy check: ${cred.last_error || 'No message'}`}
                  ></span>
                  
                  <!-- Switch toggle -->
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
              <div class="flex flex-col gap-0.5 mt-1 font-mono text-[10px] text-secondary">
                <div class="truncate">Base URL: <span class="text-primary font-medium">{cred.base_url}</span></div>
                <div>Routing Weight: <span class="text-primary font-bold">{cred.weight}</span></div>
              </div>

              <!-- Last Error Message if unhealthy -->
              {#if !cred.is_healthy && cred.last_error}
                <div class="text-[10px] text-red-500 bg-red-500/10 border border-red-500/20 p-2 rounded leading-relaxed mt-1 flex items-start gap-1">
                  <AlertTriangle size={12} class="shrink-0 mt-0.5" />
                  <span class="break-all">{cred.last_error}</span>
                </div>
              {/if}

              <!-- Test health buttons -->
              <div class="flex justify-end mt-2 pt-2 border-t border-[var(--border-color)]">
                <button 
                  class="test-health-btn flex items-center gap-1.5 text-[10px] font-bold text-[#f97316] py-1 px-2.5 rounded border border-[#f97316]/20 bg-[#f97316]/5 hover:bg-[#f97316]/15 transition-all"
                  onclick={() => testCredential(cred)}
                  disabled={testingCredId === cred.id}
                >
                  {#if testingCredId === cred.id}
                    <span class="animate-spin text-[10px]">⟳</span> Testing
                  {:else}
                    <Heart size={10} /> Test Health
                  {/if}
                </button>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </div>

    <!-- Row 2: Logs and History Viewer -->
    <div class="glass-card rounded-xl border p-5 flex flex-col gap-4">
      <div class="flex flex-col gap-1">
        <h3 class="font-bold text-sm text-primary">Pool Request History & Telemetry</h3>
        <p class="text-[10px] text-secondary">Audit and monitor incoming calls routed to this model pool. Features full-text semantic visualizer.</p>
      </div>

      <!-- Filters Toolbar -->
      <div class="grid grid-cols-1 md:grid-cols-4 gap-3 bg-[var(--sidebar-bg)] p-3 border rounded-lg">
        <div class="flex flex-col gap-1">
          <label class="text-[9px] font-bold uppercase tracking-wider text-secondary" for="filter-tenant-input">Tenant Filter</label>
          <input 
            type="text" 
            id="filter-tenant-input"
            class="input-box p-2 text-xs rounded border w-full" 
            placeholder="Tenant API key or UUID..."
            bind:value={logsFilters.tenant_id}
            onchange={handleFilterChange}
          />
        </div>
        <div class="flex flex-col gap-1">
          <label class="text-[9px] font-bold uppercase tracking-wider text-secondary" for="filter-status-select">Status Filter</label>
          <select 
            id="filter-status-select"
            class="input-box p-2 text-xs rounded border w-full"
            bind:value={logsFilters.status}
            onchange={handleFilterChange}
          >
            <option value="">All Outcomes</option>
            <option value="success">Success (2xx)</option>
            <option value="error">Errors (4xx/5xx)</option>
          </select>
        </div>
        <div class="flex flex-col gap-1">
          <label class="text-[9px] font-bold uppercase tracking-wider text-secondary" for="filter-search-input">Keyword Search</label>
          <input 
            type="text" 
            id="filter-search-input"
            class="input-box p-2 text-xs rounded border w-full" 
            placeholder="Query string match..."
            bind:value={logsFilters.search}
            onchange={handleFilterChange}
          />
        </div>
        
        <!-- Semantic vector search toggle -->
        <div class="flex flex-col gap-1">
          <div class="flex items-center justify-between">
            <label class="text-[9px] font-bold uppercase tracking-wider text-secondary" for="filter-semantic-input">Semantic AI Search</label>
            <label class="flex items-center gap-1 cursor-pointer">
              <input 
                type="checkbox" 
                class="log-checkbox w-3 h-3" 
                bind:checked={logsFilters.use_semantic} 
                onchange={handleFilterChange}
              />
              <span class="text-[8px] font-bold uppercase tracking-wider text-[#a78bfa]">Enable</span>
            </label>
          </div>
          <div class="relative">
            <input 
              type="text" 
              id="filter-semantic-input"
              class="input-box p-2 pl-7 text-xs rounded border w-full {logsFilters.use_semantic ? 'border-[#a78bfa]' : ''}" 
              placeholder="Search meaning..."
              bind:value={logsFilters.semantic_query}
              disabled={!logsFilters.use_semantic}
              onkeydown={(e) => { if (e.key === 'Enter') handleFilterChange(); }}
            />
            <Sparkles size={11} class="absolute left-2.5 top-2.5 {logsFilters.use_semantic ? 'text-[#a78bfa]' : 'opacity-30'}" />
          </div>
        </div>
      </div>

      <!-- Logs Data Table -->
      <div class="logs-table-wrapper border rounded-lg overflow-x-auto">
        <table class="providers-table w-full">
          <thead>
            <tr>
              <th class="w-16">Status</th>
              <th>Time</th>
              <th>Tenant</th>
              <th>Provider Used</th>
              <th>Model Version</th>
              <th>Latency</th>
              <th>Tokens (P/C)</th>
              {#if logsFilters.use_semantic}
                <th class="w-24">Similarity</th>
              {/if}
              <th class="w-12 text-center">Inspect</th>
            </tr>
          </thead>
          <tbody>
            {#if logsLoading && poolLogs.length === 0}
              <tr>
                <td colspan={logsFilters.use_semantic ? 9 : 8} class="text-center py-8 text-xs text-secondary opacity-60">
                  <span class="animate-spin inline-block mr-2">⟳</span> Fetching logs...
                </td>
              </tr>
            {:else if poolLogs.length === 0}
              <tr>
                <td colspan={logsFilters.use_semantic ? 9 : 8} class="text-center py-8 text-xs opacity-40">
                  No request history matched the active filters.
                </td>
              </tr>
            {:else}
              {#each poolLogs as log (log.id)}
                <tr 
                  class="provider-row cursor-pointer select-text {expandedLogId === log.id ? 'bg-[#f97316]/5 border-l-2 border-l-[#f97316]' : ''}"
                  onclick={() => expandedLogId = expandedLogId === log.id ? null : log.id}
                >
                  <td>
                    {#if log.status_code >= 200 && log.status_code < 400}
                      <span class="flex items-center gap-1 text-[#04d361] font-bold text-[10px]">
                        <CheckCircle size={12} /> {log.status_code}
                      </span>
                    {:else}
                      <span class="flex items-center gap-1 text-[#f74040] font-bold text-[10px]">
                        <XCircle size={12} /> {log.status_code}
                      </span>
                    {/if}
                  </td>
                  <td class="font-mono text-[10px] opacity-60 whitespace-nowrap">
                    {new Date(log.created_at).toLocaleTimeString()} {new Date(log.created_at).toLocaleDateString()}
                  </td>
                  <td class="font-medium text-xs truncate max-w-[120px]" title={log.tenant_id}>
                    {log.tenant_name || log.tenant_id || 'System'}
                  </td>
                  <td>
                    <span class="provider-badge text-[8px] py-0.5 px-1.5 rounded {log.provider === 'openai' ? 'badge-openai' : log.provider === 'nvidia' ? 'badge-nvidia' : 'badge-default'}">
                      {log.provider}
                    </span>
                  </td>
                  <td class="font-mono text-[10px] text-secondary truncate max-w-[150px]" title={log.model}>
                    {log.model}
                  </td>
                  <td class="font-mono text-xs font-semibold text-primary">{log.latency_ms}ms</td>
                  <td class="font-mono text-[10px] text-secondary">
                    {log.prompt_tokens} / <span class="text-primary">{log.completion_tokens}</span>
                  </td>
                  {#if logsFilters.use_semantic}
                    <td>
                      {#if log.similarity !== undefined && log.similarity !== 0}
                        <span style="display:inline-block; padding:1px 6px; border-radius:4px; font-size:9px; font-weight:800; color:#a78bfa; background:rgba(167,139,250,0.1); border:1px solid rgba(167,139,250,0.3)">
                          {(log.similarity * 100).toFixed(1)}% match
                        </span>
                      {:else}
                        <span class="text-[10px] opacity-25">—</span>
                      {/if}
                    </td>
                  {/if}
                  <td class="text-center">
                    <button class="icon-button p-1">
                      {#if expandedLogId === log.id}
                        <ChevronUp size={14} />
                      {:else}
                        <ChevronDown size={14} />
                      {/if}
                    </button>
                  </td>
                </tr>

                <!-- Expanded Log disclosure box -->
                {#if expandedLogId === log.id}
                  <tr class="bg-[var(--sidebar-bg)] border-b">
                    <td colspan={logsFilters.use_semantic ? 9 : 8} class="p-4 select-text">
                      <div class="flex flex-col gap-3 max-w-full text-xs">
                        
                        <!-- Error block -->
                        {#if log.error_message}
                          <div class="flex flex-col gap-1 border border-red-500/20 bg-red-500/5 p-3 rounded-lg">
                            <span class="font-bold text-red-500 text-[10px] uppercase tracking-wider">Error Details</span>
                            <pre class="font-mono text-[10px] text-red-400 whitespace-pre-wrap break-all leading-normal">{log.error_message}</pre>
                          </div>
                        {/if}

                        <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
                          <!-- Prompt Panel -->
                          <div class="flex flex-col gap-1.5">
                            <span class="font-bold text-secondary text-[9px] uppercase tracking-wider">Prompt Payload</span>
                            <div class="bg-[var(--frame-bg)] border p-3 rounded-lg font-mono text-[11px] leading-relaxed break-words max-h-48 overflow-y-auto whitespace-pre-wrap">
                              {log.prompt_text || 'Empty prompt content / body.'}
                            </div>
                          </div>

                          <!-- Response Panel -->
                          <div class="flex flex-col gap-1.5">
                            <span class="font-bold text-secondary text-[9px] uppercase tracking-wider">Response Content</span>
                            <div class="bg-[var(--frame-bg)] border p-3 rounded-lg font-mono text-[11px] leading-relaxed break-words max-h-48 overflow-y-auto whitespace-pre-wrap">
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
        <div class="flex justify-center mt-2">
          <button 
            class="px-4 py-2 rounded-lg border text-xs font-semibold text-secondary hover:text-primary hover:border-primary transition-all flex items-center gap-1.5"
            onclick={loadMoreLogs}
            disabled={logsLoading}
          >
            {#if logsLoading}
              <span class="animate-spin text-xs">⟳</span> Loading
            {:else}
              Load More History Logs
            {/if}
          </button>
        </div>
      {/if}

    </div>
  </div>
{:else}
  <!-- Pools data grid -->
  <div class="providers-grid-wrap">
    {#if loading}
      <div class="providers-loading">
        <div class="animate-spin text-[#f97316]" style="font-size:24px;">⟳</div>
        <p class="text-xs mt-2">Loading routing pools...</p>
      </div>
    {:else if error}
      <div class="providers-loading">
        <AlertTriangle size={32} class="text-red-500 mb-2" />
        <p class="text-red-500 text-xs">{error}</p>
        <button class="mt-3 px-4 py-2 rounded-lg text-white bg-[#f97316] font-semibold text-xs" onclick={loadPools}>Retry</button>
      </div>
    {:else if pools.length === 0}
      <div class="providers-loading">
        <Cpu size={40} class="opacity-20 mb-3" />
        <p class="opacity-40 text-xs">No model pools registered yet.</p>
        <button class="mt-4 px-4 py-2 rounded-lg text-white bg-[#f97316] font-semibold text-xs" onclick={openAddModal}>
          <span class="flex items-center gap-1.5"><Plus size={12} /> Create First Pool</span>
        </button>
      </div>
    {:else}
      <div class="providers-table-container">
        <table class="providers-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Model Pattern</th>
              <th>Capabilities</th>
              <th>Strategy</th>
              <th>Fallback Pool ID</th>
              <th>Credentials</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {#each pools as pool (pool.id)}
              <tr class="provider-row cursor-pointer" onclick={() => openPoolDetails(pool)}>
                <td class="font-mono text-[10px] opacity-60">#{pool.id}</td>
                <td class="font-bold text-xs text-[#f97316]">{pool.model_pattern}</td>
                <td>
                  <div class="flex flex-wrap gap-1">
                    {#each capabilityKeys(pool.capabilities) as key}
                      {@const badge = CAPABILITY_BADGES[key]}
                      <span style="display:inline-block; padding:1px 6px; border-radius:4px; font-size:9px; font-weight:700; text-transform:uppercase; letter-spacing:0.04em; color:{badge.color}; background:{badge.bg}; border:1px solid {badge.border};">
                        {badge.label}
                      </span>
                    {/each}
                    {#if capabilityKeys(pool.capabilities).length === 0}
                      <span class="text-[10px] opacity-30">—</span>
                    {/if}
                  </div>
                </td>
                <td>
                  <span class="provider-badge {pool.strategy === 'round-robin' ? 'badge-openai' : 'badge-anthropic'}">
                    {pool.strategy}
                  </span>
                </td>
                <td class="font-mono text-xs">{pool.fallback_pool_id !== null && pool.fallback_pool_id !== undefined ? `#${pool.fallback_pool_id}` : '—'}</td>
                <td class="font-mono text-xs text-center">{pool.credential_count || 0} keys</td>
                <td>
                  <div class="flex items-center gap-1">
                    <button class="icon-button" onclick={(e) => openEditModal(pool, e)} title="Edit pool">
                      <Pencil size={13} />
                    </button>
                    <button class="icon-button" onclick={(e) => confirmDelete(pool.id, e)} title="Delete pool">
                      <Trash2 size={13} class="text-red-500" />
                    </button>
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
{#if showAddModal}
  <div class="modal-backdrop fixed inset-0 flex items-center justify-center p-4 z-50 bg-black-trans backdrop-blur-sm">
    <div class="modal-content w-full max-w-sm rounded-xl border p-6 shadow-2xl relative">
      <div class="flex items-center justify-between mb-4">
        <h3 class="font-bold text-lg text-primary">Create Model Pool</h3>
        <button class="icon-button" onclick={() => showAddModal = false}><X size={16} /></button>
      </div>

      <div class="flex flex-col gap-3 mb-5 text-primary">
        <div class="form-group flex flex-col gap-1">
          <label class="text-[10px] font-bold uppercase tracking-wider text-secondary" for="add-model-input">Model Pattern</label>
          <input 
            type="text" 
            id="add-model-input"
            class="input-box w-full p-2.5 rounded-lg border text-sm" 
            placeholder="e.g. gpt-4o, claude-3-5-sonnet*" 
            bind:value={addForm.model_pattern} 
          />
        </div>
        <div class="form-group flex flex-col gap-1">
          <label class="text-[10px] font-bold uppercase tracking-wider text-secondary" for="add-strategy-select">Routing Strategy</label>
          <select id="add-strategy-select" class="input-box w-full p-2.5 rounded-lg border text-sm" bind:value={addForm.strategy}>
            <option value="round-robin">round-robin</option>
            <option value="weighted-round-robin">weighted-round-robin</option>
            <option value="random">random</option>
          </select>
        </div>
        <div class="form-group flex flex-col gap-1">
          <label class="text-[10px] font-bold uppercase tracking-wider text-secondary" for="add-fallback-select">Fallback Pool (Optional)</label>
          <select id="add-fallback-select" class="input-box w-full p-2.5 rounded-lg border text-sm" bind:value={addForm.fallback_pool_id}>
            <option value="">None</option>
            {#each pools as otherPool}
              <option value={otherPool.id}>{otherPool.model_pattern} (ID: {otherPool.id})</option>
            {/each}
          </select>
        </div>
      </div>

      <div class="flex justify-end gap-2 text-xs">
        <button class="px-4 py-2 rounded-lg border text-primary" onclick={() => showAddModal = false}>Cancel</button>
        <button class="px-4 py-2 rounded-lg text-white bg-[#f97316] font-semibold flex items-center gap-1.5 min-w-[120px] justify-center" onclick={createPool} disabled={addLoading}>
          {#if addLoading}
            <span class="animate-spin">🔄</span> Creating...
          {:else}
            Create Pool
          {/if}
        </button>
      </div>
    </div>
  </div>
{/if}

<!-- ─── EDIT POOL MODAL ────────────────────────────────────────────────────── -->
{#if showEditModal}
  <div class="modal-backdrop fixed inset-0 flex items-center justify-center p-4 z-50 bg-black-trans backdrop-blur-sm">
    <div class="modal-content w-full max-w-sm rounded-xl border p-6 shadow-2xl relative">
      <div class="flex items-center justify-between mb-4">
        <h3 class="font-bold text-lg text-primary">Edit Model Pool</h3>
        <button class="icon-button" onclick={() => showEditModal = false}><X size={16} /></button>
      </div>

      <div class="flex flex-col gap-3 mb-5 text-primary">
        <div class="form-group flex flex-col gap-1">
          <label class="text-[10px] font-bold uppercase tracking-wider text-secondary" for="edit-model-input">Model Pattern</label>
          <input type="text" id="edit-model-input" class="input-box w-full p-2.5 rounded-lg border text-sm" bind:value={editForm.model_pattern} />
        </div>
        <div class="form-group flex flex-col gap-1">
          <label class="text-[10px] font-bold uppercase tracking-wider text-secondary" for="edit-strategy-select">Routing Strategy</label>
          <select id="edit-strategy-select" class="input-box w-full p-2.5 rounded-lg border text-sm" bind:value={editForm.strategy}>
            <option value="round-robin">round-robin</option>
            <option value="weighted-round-robin">weighted-round-robin</option>
            <option value="random">random</option>
          </select>
        </div>
        <div class="form-group flex flex-col gap-1">
          <label class="text-[10px] font-bold uppercase tracking-wider text-secondary" for="edit-fallback-select">Fallback Pool (Optional)</label>
          <select id="edit-fallback-select" class="input-box w-full p-2.5 rounded-lg border text-sm" bind:value={editForm.fallback_pool_id}>
            <option value="">None</option>
            {#each pools as otherPool}
              {#if otherPool.id !== editForm.id}
                <option value={String(otherPool.id)}>{otherPool.model_pattern} (ID: {otherPool.id})</option>
              {/if}
            {/each}
          </select>
        </div>
      </div>

      <div class="flex justify-end gap-2 text-xs">
        <button class="px-4 py-2 rounded-lg border text-primary" onclick={() => showEditModal = false}>Cancel</button>
        <button class="px-4 py-2 rounded-lg text-white bg-[#f97316] font-semibold flex items-center gap-1.5 min-w-[120px] justify-center" onclick={updatePool} disabled={editLoading}>
          {#if editLoading}
            <span class="animate-spin">🔄</span> Saving...
          {:else}
            Save Changes
          {/if}
        </button>
      </div>
    </div>
  </div>
{/if}

<!-- ─── DELETE CONFIRMATION DIALOG ─────────────────────────────────────────── -->
{#if showDeleteConfirm}
  <div class="modal-backdrop fixed inset-0 flex items-center justify-center p-4 z-50 bg-black-trans backdrop-blur-sm">
    <div class="modal-content w-full max-w-xs rounded-xl border p-6 shadow-2xl relative text-center">
      <AlertTriangle size={32} class="text-red-500 mx-auto mb-3" />
      <h3 class="font-bold text-base mb-2 text-primary">Delete Model Pool?</h3>
      <p class="text-xs mb-5 opacity-75 text-secondary">All routing configurations and associated provider keys assigned to this model pool will be deleted. Upstream traffic will fallback or fail.</p>
      <div class="flex justify-center gap-2 text-xs">
        <button class="px-4 py-2 rounded-lg border text-primary" onclick={() => { showDeleteConfirm = false; deleteTargetId = null; }}>Cancel</button>
        <button class="px-4 py-2 rounded-lg text-white bg-red-500 font-semibold flex items-center gap-1.5 min-w-[100px] justify-center" style="background-color: #ef4444;" onclick={deletePoolById} disabled={deleteLoading}>
          {#if deleteLoading}
            <span class="animate-spin">🔄</span>
          {:else}
            Delete
          {/if}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  /* Premium Glassmorphic Card Theme */
  .glass-card {
    background: var(--frame-bg);
    border: 1px solid var(--border-color);
    box-shadow: 0 4px 20px var(--shadow-color);
    transition: transform 0.2s ease, box-shadow 0.2s ease;
  }
  
  .member-card {
    background: var(--card-bg);
    border: 1px solid var(--border-color);
    box-shadow: 0 2px 10px var(--shadow-color);
  }

  /* Pulse animation for active pool keys check */
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
    width: 8px;
    height: 8px;
    border-radius: 50%;
  }

  @keyframes health-pulse {
    0% {
      transform: scale(0.95);
      box-shadow: 0 0 0 0 rgba(16, 185, 129, 0.7);
    }
    70% {
      transform: scale(1);
      box-shadow: 0 0 0 6px rgba(16, 185, 129, 0);
    }
    100% {
      transform: scale(0.95);
      box-shadow: 0 0 0 0 rgba(16, 185, 129, 0);
    }
  }

  /* Custom logs visualizer adjustments */
  .logs-table-wrapper {
    max-height: 480px;
    overflow-y: auto;
    border: 1px solid var(--border-color);
  }
</style>
