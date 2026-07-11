<script>
  import { onMount } from 'svelte';
  import { 
    Cpu, Plus, RefreshCw, Shield, AlertTriangle, Trash2, Pencil, X, Sparkles,
    ArrowLeft, Search, ChevronDown, ChevronUp, Play, CheckCircle, XCircle, Heart,
    SlidersHorizontal, ArrowUpDown
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
  let totalCount = $state(0);
  let loading = $state(false);
  let error = $state('');

  // Pagination / lazy-loading
  const PAGE_SIZE = 100;
  let currentPage = $state(0);
  let hasMore = $state(false);
  let loadingMore = $state(false);

  // Filtering & Sorting
  let searchQuery = $state('');
  let filterStrategy = $state('');
  let filterCapabilities = $state([]);
  let filterFallback = $state('');
  let filterCredentials = $state('');
  let filterHealth = $state('');
  let sortBy = $state('model_pattern');
  let sortOrder = $state('asc');
  let showAdvancedFilters = $state(false);
  let searchTimer = null;

  function toggleCapabilityFilter(capKey) {
    if (filterCapabilities.includes(capKey)) {
      filterCapabilities = filterCapabilities.filter(k => k !== capKey);
    } else {
      filterCapabilities = [...filterCapabilities, capKey];
    }
    reloadPools();
  }

  function clearFilters() {
    searchQuery = '';
    filterStrategy = '';
    filterCapabilities = [];
    filterFallback = '';
    filterCredentials = '';
    filterHealth = '';
    sortBy = 'model_pattern';
    sortOrder = 'asc';
    reloadPools();
  }

  // Virtualization
  const ROW_HEIGHT = 45;
  const OVERSCAN = 8;
  let scrollTop = $state(0);
  let viewportHeight = $state(0);
  let vscrollEl;

  let visibleRange = $derived.by(() => {
    const loaded = pools.length;
    if (loaded === 0 || viewportHeight === 0) {
      return { start: 0, end: 0, padTop: 0, padBottom: 0 };
    }
    const start = Math.max(0, Math.floor(scrollTop / ROW_HEIGHT) - OVERSCAN);
    const visibleCount = Math.ceil(viewportHeight / ROW_HEIGHT) + OVERSCAN * 2;
    const end = Math.min(loaded, start + visibleCount);
    return {
      start,
      end,
      padTop: start * ROW_HEIGHT,
      padBottom: (loaded - end) * ROW_HEIGHT
    };
  });

  let visibleItems = $derived(pools.slice(visibleRange.start, visibleRange.end));

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

  // Bulk selection and deletion state
  let selectedPoolIds = $state([]);
  let showBulkDeletePoolsConfirm = $state(false);
  let bulkDeletePoolsLoading = $state(false);

  // Auto-fetch when adminKey changes
  $effect(() => {
    if (appState.adminKey.trim() && !selectedPool) {
      reloadPools();
    }
  });

  // Debounced search/filter → reload first page
  function onSearchInput() {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => reloadPools(), 300);
  }

  // ─── API Helper Headers ───────────────────────────────────────────────────
  function adminHeaders() {
    return {
      'Authorization': `Bearer ${appState.adminKey.trim()}`,
      'Content-Type': 'application/json'
    };
  }

  async function loadPoolsPage(page) {
    try {
      const params = new URLSearchParams();
      params.append('limit', String(PAGE_SIZE));
      params.append('offset', String(page * PAGE_SIZE));
      
      if (searchQuery.trim()) params.append('search', searchQuery.trim());
      if (filterStrategy) params.append('strategy', filterStrategy);
      
      if (filterFallback === 'has_fallback') {
        params.append('has_fallback', 'true');
      } else if (filterFallback === 'no_fallback') {
        params.append('has_fallback', 'false');
      }

      if (filterCredentials === 'has_keys') {
        params.append('has_credentials', 'true');
      } else if (filterCredentials === 'no_keys') {
        params.append('has_credentials', 'false');
      }

      if (filterHealth && filterHealth !== 'all') {
        params.append('health_status', filterHealth);
      }

      if (filterCapabilities.length > 0) {
        params.append('capabilities', filterCapabilities.join(','));
      }

      if (sortBy) params.append('sort_by', sortBy);
      if (sortOrder) params.append('sort_order', sortOrder);

      const res = await fetch(`/api/v1/admin/pools?${params.toString()}`, { headers: adminHeaders() });
      if (res.ok) {
        const data = await res.json();
        const rows = data.data ?? data;
        return { rows, total: data.total ?? rows.length };
      }
      const err = await res.json();
      return { error: err.error || `Error ${res.status}` };
    } catch (e) {
      return { error: `Network error: ${e.message}` };
    }
  }

  async function reloadPools() {
    loading = true;
    error = '';
    currentPage = 0;
    appState.apiLoading = true;
    try {
      const result = await loadPoolsPage(0);
      if (result.error) {
        error = result.error;
      } else {
        pools = result.rows;
        totalCount = result.total;
        hasMore = result.rows.length < result.total;
        selectedPoolIds = selectedPoolIds.filter(id => pools.some(p => p.id === id));
        if (vscrollEl) vscrollEl.scrollTop = 0;
      }
    } finally {
      loading = false;
      appState.apiLoading = false;
    }
  }

  async function loadMore() {
    if (loadingMore || !hasMore) return;
    loadingMore = true;
    appState.apiLoading = true;
    const nextPage = currentPage + 1;
    try {
      const result = await loadPoolsPage(nextPage);
      if (result.error) {
        appState.addToast('error', result.error);
      } else {
        pools = [...pools, ...result.rows];
        currentPage = nextPage;
        hasMore = pools.length < result.total;
      }
    } finally {
      loadingMore = false;
      appState.apiLoading = false;
    }
  }

  function onVScroll(e) {
    scrollTop = e.target.scrollTop;
    // Trigger lazy load when near the bottom of the loaded list
    const remaining = pools.length * ROW_HEIGHT - (scrollTop + viewportHeight);
    if (hasMore && !loadingMore && remaining < ROW_HEIGHT * 10) {
      loadMore();
    }
  }

  // ─── Details View Methods ───────────────────────────────────────────────
  async function loadPoolDetails(poolId) {
    appState.apiLoading = true;
    try {
      const res = await fetch(`/api/v1/admin/pools/${poolId}`, { headers: adminHeaders() });
      if (res.ok) {
        poolDetails = await res.json();
      } else {
        appState.addToast('error', 'Failed to load pool credentials');
      }
    } catch (e) {
      appState.addToast('error', `Error loading credentials: ${e.message}`);
    } finally {
      appState.apiLoading = false;
    }
  }

  async function loadPoolLogs(poolId, append = false) {
    logsLoading = true;
    appState.apiLoading = true;
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
      appState.apiLoading = false;
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
    appState.apiLoading = true;
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
      appState.apiLoading = false;
    }
  }

  async function toggleCredentialHealth(cred) {
    appState.apiLoading = true;
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
    } finally {
      appState.apiLoading = false;
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
    appState.apiLoading = true;
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
        reloadPools();
      } else {
        const err = await res.json();
        appState.addToast('error', err.details || err.error || 'Failed to create pool');
      }
    } catch (e) {
      appState.addToast('error', `Network error: ${e.message}`);
    } finally {
      addLoading = false;
      appState.apiLoading = false;
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
    appState.apiLoading = true;
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
        reloadPools();
      } else {
        const err = await res.json();
        appState.addToast('error', err.details || err.error || 'Failed to update pool');
      }
    } catch (e) {
      appState.addToast('error', `Network error: ${e.message}`);
    } finally {
      editLoading = false;
      appState.apiLoading = false;
    }
  }

  function confirmDelete(id, event) {
    event.stopPropagation();
    deleteTargetId = id;
    showDeleteConfirm = true;
  }

  async function deletePoolById() {
    deleteLoading = true;
    appState.apiLoading = true;
    try {
      const res = await fetch(`/api/v1/admin/pools/${deleteTargetId}`, {
        method: 'DELETE',
        headers: adminHeaders()
      });
      if (res.ok) {
        appState.addToast('success', 'Model pool deleted successfully');
        showDeleteConfirm = false;
        if (selectedPool && selectedPool.id === deleteTargetId) {
          selectedPool = null;
        }
        deleteTargetId = null;
        reloadPools();
      } else {
        const err = await res.json();
        appState.addToast('error', err.details || err.error || 'Failed to delete pool');
      }
    } catch (e) {
      appState.addToast('error', `Network error: ${e.message}`);
    } finally {
      deleteLoading = false;
      appState.apiLoading = false;
    }
  }

  function confirmBulkDeletePools() {
    showBulkDeletePoolsConfirm = true;
  }

  async function deletePoolsBulk() {
    bulkDeletePoolsLoading = true;
    appState.apiLoading = true;
    try {
      const res = await fetch('/api/v1/admin/pools/bulk-delete', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...adminHeaders()
        },
        body: JSON.stringify({ ids: selectedPoolIds })
      });
      if (res.ok) {
        appState.addToast('success', `${selectedPoolIds.length} model pools deleted successfully`);
        showBulkDeletePoolsConfirm = false;
        if (selectedPool && selectedPoolIds.includes(selectedPool.id)) {
          selectedPool = null;
        }
        selectedPoolIds = [];
        reloadPools();
      } else {
        const err = await res.json();
        appState.addToast('error', err.details || err.error || 'Failed to delete pools');
      }
    } catch (e) {
      appState.addToast('error', `Network error: ${e.message}`);
    } finally {
      bulkDeletePoolsLoading = false;
      appState.apiLoading = false;
    }
  }

  function connectAdminKey() {
    const key = appState.adminKey.trim();
    if (!key) return;
    localStorage.setItem('cag_admin_key', key);
    reloadPools();
  }

  onMount(() => {
    if (appState.adminKey.trim() && !selectedPool) {
      reloadPools();
    }
  });
</script>

<header class="header flex items-center justify-between px-6 py-4 border-b shrink-0">
  <div class="flex items-center gap-3">
    {#if selectedPool}
      <Button variant="ghost" size="sm" onclick={() => { selectedPool = null; reloadPools(); }} title="Back to pools list">
        <ArrowLeft size={16} />
      </Button>
      <Cpu size={20} class="text-[#f97316]" />
      <span class="font-bold text-base">Pool Details: <span class="text-[#f97316] font-mono">{selectedPool.model_pattern}</span></span>
    {:else}
      <Cpu size={20} class="text-[#f97316]" />
      <span class="font-bold text-base">Model Routing Pools</span>
      {#if appState.adminKey.trim()}
        <span class="text-xs font-bold text-secondary bg-gray-500/10 border border-gray-500/20 px-2.5 py-0.5 rounded-full uppercase">{totalCount} pools</span>
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
        {#if selectedPoolIds.length > 0}
          <Button variant="danger" size="sm" onclick={confirmBulkDeletePools} title="Delete selected pools">
            <Trash2 size={14} />
            Delete Selected ({selectedPoolIds.length})
          </Button>
        {/if}
        <Button variant="secondary" size="sm" onclick={() => { reloadPools(); appState.addToast('info', 'Refreshing capabilities...'); }} title="Re-classify all model capabilities">
          <Sparkles size={14} />
          Refresh Capabilities
        </Button>
        <Button variant="secondary" size="sm" onclick={() => { reloadPools(); appState.addToast('info', 'Refreshing pools list...'); }}>
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
  <div class="detail-page-container flex flex-col gap-6 p-6 overflow-y-auto w-full flex-grow">
    
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
                <span class="provider-badge text-xs font-bold py-1 px-2.5 rounded-lg badge-{cred.provider}">
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
                    <span class="provider-badge text-[10px] font-bold py-1 px-2 rounded-lg badge-{log.provider}">
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
  <!-- Pools data grid (virtualized + lazy-loaded) -->
  <div class="providers-grid-wrap flex flex-col flex-grow overflow-hidden">
    <!-- Filter / search toolbar -->
    <div class="flex flex-col border-b shrink-0 bg-[var(--card-bg)] shadow-sm">
      <!-- Main Row: Search & Action Buttons -->
      <div class="flex items-center gap-3 px-6 py-3 flex-wrap">
        <div class="relative flex-grow max-w-md">
          <Search size={15} class="absolute left-3 top-1/2 -translate-y-1/2 text-secondary opacity-50" />
          <input
            type="text"
            class="w-full pl-9 pr-3 py-1.5 text-sm rounded-lg bg-[var(--input-bg)] border border-[var(--border-color)] focus:border-[#f97316] focus:outline-none placeholder-gray-400 dark:placeholder-gray-500 transition-colors"
            placeholder="Search model pattern, strategy..."
            bind:value={searchQuery}
            oninput={onSearchInput}
          />
        </div>
        
        <Button 
          variant="outline" 
          size="sm" 
          onclick={() => showAdvancedFilters = !showAdvancedFilters}
          class="flex items-center gap-2 border-[var(--border-color)] hover:border-[#f97316]"
        >
          <SlidersHorizontal size={14} class={showAdvancedFilters ? 'text-[#f97316]' : ''} />
          <span>Advanced Filters</span>
          {#if showAdvancedFilters}
            <ChevronUp size={14} />
          {:else}
            <ChevronDown size={14} />
          {/if}
        </Button>

        {#if searchQuery || filterStrategy || filterFallback || filterCredentials || filterHealth || filterCapabilities.length > 0}
          <Button 
            variant="ghost" 
            size="sm" 
            onclick={clearFilters}
            class="text-xs text-secondary hover:text-red-500 flex items-center gap-1"
          >
            <X size={13} />
            Reset Filters
          </Button>
        {/if}

        <span class="text-xs text-secondary ml-auto font-medium">
          {#if totalCount > 0}
            Showing {pools.length} of {totalCount}
            {#if loadingMore}<span class="opacity-60"> · loading more…</span>{/if}
          {:else if searchQuery || filterStrategy || filterFallback || filterCredentials || filterHealth || filterCapabilities.length > 0}
            No matches found
          {/if}
        </span>
      </div>

      <!-- Expandable Advanced Filters Panel -->
      {#if showAdvancedFilters}
        <div 
          class="px-6 pb-4 pt-2 border-t border-[var(--border-color)] bg-[var(--sidebar-bg)]/40 flex flex-col gap-4 animate-fade-in text-primary"
        >
          <!-- Grid for dropdown selectors -->
          <div class="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-4 lg:grid-cols-6 gap-4">
            <!-- Strategy Filter -->
            <div class="flex flex-col gap-1.5">
              <label class="text-[10px] font-bold uppercase tracking-wider text-secondary">Strategy</label>
              <select 
                class="input-field select-field py-2 px-3 text-xs rounded-lg border border-[var(--border-color)] bg-[var(--frame-bg)] text-primary focus:border-[#f97316] focus:outline-none"
                bind:value={filterStrategy}
                onchange={reloadPools}
              >
                <option value="">All Strategies</option>
                <option value="round-robin">round-robin</option>
                <option value="weighted-round-robin">weighted-round-robin</option>
                <option value="random">random</option>
              </select>
            </div>

            <!-- Fallback Filter -->
            <div class="flex flex-col gap-1.5">
              <label class="text-[10px] font-bold uppercase tracking-wider text-secondary">Fallback Status</label>
              <select 
                class="input-field select-field py-2 px-3 text-xs rounded-lg border border-[var(--border-color)] bg-[var(--frame-bg)] text-primary focus:border-[#f97316] focus:outline-none"
                bind:value={filterFallback}
                onchange={reloadPools}
              >
                <option value="">All Pools</option>
                <option value="has_fallback">Has Fallback Pool</option>
                <option value="no_fallback">No Fallback Pool</option>
              </select>
            </div>

            <!-- Credentials Count Filter -->
            <div class="flex flex-col gap-1.5">
              <label class="text-[10px] font-bold uppercase tracking-wider text-secondary">API Keys Count</label>
              <select 
                class="input-field select-field py-2 px-3 text-xs rounded-lg border border-[var(--border-color)] bg-[var(--frame-bg)] text-primary focus:border-[#f97316] focus:outline-none"
                bind:value={filterCredentials}
                onchange={reloadPools}
              >
                <option value="">All Pools</option>
                <option value="has_keys">Active (Has Keys)</option>
                <option value="no_keys">Empty (No Keys)</option>
              </select>
            </div>

            <!-- Health Status Filter -->
            <div class="flex flex-col gap-1.5">
              <label class="text-[10px] font-bold uppercase tracking-wider text-secondary">Key Health Status</label>
              <select 
                class="input-field select-field py-2 px-3 text-xs rounded-lg border border-[var(--border-color)] bg-[var(--frame-bg)] text-primary focus:border-[#f97316] focus:outline-none"
                bind:value={filterHealth}
                onchange={reloadPools}
              >
                <option value="">All Health</option>
                <option value="healthy">All Keys Healthy</option>
                <option value="unhealthy">Has Unhealthy Key(s)</option>
                <option value="empty">No Keys</option>
              </select>
            </div>

            <!-- Sort By -->
            <div class="flex flex-col gap-1.5">
              <label class="text-[10px] font-bold uppercase tracking-wider text-secondary">Sort By</label>
              <select 
                class="input-field select-field py-2 px-3 text-xs rounded-lg border border-[var(--border-color)] bg-[var(--frame-bg)] text-primary focus:border-[#f97316] focus:outline-none"
                bind:value={sortBy}
                onchange={reloadPools}
              >
                <option value="model_pattern">Model Pattern</option>
                <option value="id">Pool ID</option>
                <option value="created_at">Date Created</option>
                <option value="credential_count">Keys Count</option>
              </select>
            </div>

            <!-- Sort Order -->
            <div class="flex flex-col gap-1.5">
              <label class="text-[10px] font-bold uppercase tracking-wider text-secondary">Sort Direction</label>
              <Button 
                variant="outline" 
                size="sm" 
                class="flex items-center justify-center gap-2 border-[var(--border-color)] hover:border-[#f97316] h-[34px] text-xs font-semibold text-primary bg-[var(--frame-bg)] w-full"
                onclick={() => { sortOrder = sortOrder === 'asc' ? 'desc' : 'asc'; reloadPools(); }}
              >
                <ArrowUpDown size={14} class="text-[#f97316]" />
                <span>{sortOrder === 'asc' ? 'Ascending' : 'Descending'}</span>
              </Button>
            </div>
          </div>

          <!-- Capability Badges Selection -->
          <div class="flex flex-col gap-2 border-t border-[var(--border-color)]/60 pt-3 text-primary">
            <span class="text-[10px] font-bold uppercase tracking-wider text-secondary">Filter by Model Capabilities</span>
            <div class="flex flex-wrap gap-2">
              {#each Object.entries(CAPABILITY_BADGES) as [key, badge]}
                {@const isSelected = filterCapabilities.includes(key)}
                <button
                  type="button"
                  onclick={() => toggleCapabilityFilter(key)}
                  style="display:flex; align-items:center; gap:6px; padding:6px 12px; border-radius:8px; font-size:11px; font-weight:700; text-transform:uppercase; letter-spacing:0.04em; cursor:pointer; transition:all 0.2s ease; border: 1px solid {isSelected ? badge.border : 'var(--border-color)'}; color: {isSelected ? badge.color : 'var(--text-secondary)'}; background: {isSelected ? badge.bg : 'var(--frame-bg)'};"
                  class="hover:-translate-y-0.5 hover:shadow-sm"
                >
                  <span class="w-1.5 h-1.5 rounded-full" style="background: {isSelected ? badge.color : 'var(--text-secondary)'}; opacity: {isSelected ? 1 : 0.4};"></span>
                  {badge.label}
                </button>
              {/each}
            </div>
          </div>
        </div>
      {/if}
    </div>

    {#if loading}
      <div class="providers-loading flex flex-col items-center justify-center flex-grow">
        <div class="animate-spin text-[#f97316] text-xl">⟳</div>
        <p class="text-sm mt-2 text-secondary">Loading routing pools...</p>
      </div>
    {:else if error}
      <div class="providers-loading flex flex-col items-center justify-center flex-grow">
        <AlertTriangle size={40} class="text-red-500 mb-2" />
        <p class="text-red-500 text-sm font-semibold">{error}</p>
        <Button variant="primary" class="mt-4" onclick={reloadPools}>Retry</Button>
      </div>
    {:else if pools.length === 0}
      <div class="providers-loading flex flex-col items-center justify-center flex-grow">
        <Cpu size={48} class="opacity-20 mb-4" />
        <p class="opacity-50 text-sm text-secondary">No model pools registered yet.</p>
        <Button variant="primary" class="mt-4" onclick={openAddModal}>
          <Plus size={14} /> Create First Pool
        </Button>
      </div>
    {:else}
      <!-- Virtualized table -->
      <div class="pools-table-container flex flex-col flex-grow overflow-hidden">
        <!-- Fixed header -->
        <div class="pools-table-header">
          <div class="pools-table-row pools-table-headrow">
            <div style="width: 40px; text-align: center;">
              <input
                type="checkbox"
                class="log-checkbox w-4 h-4 rounded border-gray-300 accent-orange-500 cursor-pointer"
                checked={selectedPoolIds.length === pools.length && pools.length > 0}
                onchange={(e) => {
                  if (e.target.checked) {
                    selectedPoolIds = pools.map(p => p.id);
                  } else {
                    selectedPoolIds = [];
                  }
                }}
              />
            </div>
            <div style="font-size: 11px;">ID</div>
            <div style="font-size: 11px;">Model Pattern</div>
            <div style="font-size: 11px;">Capabilities</div>
            <div style="font-size: 11px;">Strategy</div>
            <div style="font-size: 11px;">Fallback Pool ID</div>
            <div style="font-size: 11px; text-align: center;">Credentials</div>
            <div style="font-size: 11px; text-align: center;">Actions</div>
          </div>
        </div>
        <!-- Virtualized scroll body -->
        <div
          class="pools-table-body"
          bind:this={vscrollEl}
          bind:clientHeight={viewportHeight}
          onscroll={onVScroll}
        >
          <div style="height: {pools.length * ROW_HEIGHT}px; position: relative;">
            <div style="position: absolute; top: {visibleRange.padTop}px; left: 0; right: 0;">
              {#each visibleItems as pool (pool.id)}
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <!-- svelte-ignore a11y_no_static_element_interactions -->
                <div class="pools-table-row pool-row cursor-pointer" style="height: {ROW_HEIGHT}px;" onclick={() => openPoolDetails(pool)}>
                  <div style="text-align: center; width: 40px;" onclick={(e) => e.stopPropagation()}>
                    <input
                      type="checkbox"
                      class="log-checkbox w-4 h-4 rounded border-gray-300 accent-orange-500 cursor-pointer"
                      value={pool.id}
                      bind:group={selectedPoolIds}
                      onclick={(e) => e.stopPropagation()}
                    />
                  </div>
                  <div class="font-mono text-xs opacity-60">#{pool.id}</div>
                  <div class="font-bold text-sm text-[#f97316]">{pool.model_pattern}</div>
                  <div>
                    <div class="flex gap-1 overflow-hidden">
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
                  </div>
                  <div>
                    <span class="provider-badge {pool.strategy === 'round-robin' ? 'badge-openai' : 'badge-anthropic'}">
                      {pool.strategy}
                    </span>
                  </div>
                  <div class="font-mono text-sm">{pool.fallback_pool_id !== null && pool.fallback_pool_id !== undefined ? `#${pool.fallback_pool_id}` : '—'}</div>
                  <div class="font-mono text-sm text-center">{pool.credential_count || 0} keys</div>
                  <div onclick={(e) => e.stopPropagation()}>
                    <div class="flex items-center justify-center gap-1">
                      <Button variant="ghost" size="sm" onclick={(e) => openEditModal(pool, e)} title="Edit pool">
                        <Pencil size={15} />
                      </Button>
                      <Button variant="ghost" size="sm" onclick={(e) => confirmDelete(pool.id, e)} title="Delete pool">
                        <Trash2 size={15} class="text-red-500" />
                      </Button>
                    </div>
                  </div>
                </div>
              {/each}
            </div>
          </div>
          {#if loadingMore}
            <div class="flex items-center justify-center py-3 text-secondary text-sm">
              <span class="animate-spin mr-2">⟳</span> Loading more...
            </div>
          {/if}
        </div>
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

<!-- ─── BULK DELETE POOLS CONFIRMATION DIALOG ─────────────────────────────────────────── -->
<Modal bind:show={showBulkDeletePoolsConfirm} title="Delete Model Pools?">
  <div class="flex flex-col items-center gap-4 text-center">
    <AlertTriangle size={48} class="text-red-500 mb-2" />
    <p class="text-sm text-secondary">
      Are you sure you want to permanently delete the {selectedPoolIds.length} selected model pools?
      All routing configurations and associated provider keys assigned to these pools will be deleted.
    </p>
    <p class="text-xs text-red-500 font-bold">This action is permanent and cannot be undone.</p>
  </div>

  {#snippet footer()}
    <div class="flex justify-center gap-3 w-full">
      <Button variant="outline" onclick={() => { showBulkDeletePoolsConfirm = false; }}>Cancel</Button>
      <Button variant="danger" onclick={deletePoolsBulk} disabled={bulkDeletePoolsLoading}>
        {#if bulkDeletePoolsLoading}
          <span class="animate-spin">⟳</span>
        {:else}
          Delete ({selectedPoolIds.length})
        {/if}
      </Button>
    </div>
  {/snippet}
</Modal>

<style>
  .detail-page-container {
    min-height: 0;
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

  /* ─── Virtualized table ─── */
  .pools-table-header {
    flex-shrink: 0;
    overflow-x: auto;
    background-color: var(--card-bg);
    border-bottom: 2px solid var(--border-color);
  }
  .pools-table-body {
    flex-grow: 1;
    overflow-y: auto;
    overflow-x: auto;
    -webkit-overflow-scrolling: touch;
  }
  .pools-table-row {
    display: grid;
    grid-template-columns: 40px 70px 1fr 1.6fr 130px 120px 90px 110px;
    align-items: center;
    min-width: 900px;
  }
  .pools-table-headrow {
    padding: 0;
  }
  .pools-table-headrow > div {
    padding: 12px 16px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    font-weight: 700;
    color: var(--text-secondary);
    white-space: nowrap;
  }
  .pools-table-row.pool-row {
    border-bottom: 1px solid var(--border-color);
    transition: background-color 0.15s;
    background-color: var(--card-bg);
  }
  .pools-table-row.pool-row:hover {
    background-color: var(--item-hover);
  }
  .pools-table-row.pool-row > div {
    padding: 12px 16px;
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
