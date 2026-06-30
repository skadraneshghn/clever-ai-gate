<script>
  import { 
    Cpu, Plus, RefreshCw, Shield, AlertTriangle, Trash2, Pencil, X, Sparkles
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
    if (adminKey.trim()) {
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

  function openEditModal(pool) {
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

  function confirmDelete(id) {
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
    <Cpu size={18} class="text-[#f97316]" />
    <span class="font-bold text-sm">Model Routing Pools</span>
    <span class="text-[10px] font-bold text-secondary uppercase">{pools.length} pools</span>
  </div>
  <div class="flex items-center gap-2">
    {#if adminKey.trim()}
      <button class="log-action-btn" style="border-color: rgba(167,139,250,0.4); color: #a78bfa; background: rgba(167,139,250,0.06);" onclick={() => { loadPools(); addToast('info', 'Refreshing capabilities...'); }} title="Re-classify all model capabilities">
        <Sparkles size={12} />
        Refresh Capabilities
      </button>
      <button class="log-action-btn log-btn-start" onclick={() => { loadPools(); addToast('info', 'Refreshing pools...'); }}>
        <RefreshCw size={12} />
        Refresh
      </button>
      <button class="log-action-btn" style="border-color: rgba(249,115,22,0.4); color: #f97316; background: rgba(249,115,22,0.06);" onclick={openAddModal}>
        <Plus size={12} />
        Create Pool
      </button>
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
              <tr class="provider-row">
                <td class="font-mono text-[10px] opacity-60">#{pool.id}</td>
                <td class="font-bold text-xs">{pool.model_pattern}</td>
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
                <td class="font-mono text-xs text-center">{pool.credential_count || 0}</td>
                <td>
                  <div class="flex items-center gap-1">
                    <button class="icon-button" onclick={() => openEditModal(pool)} title="Edit pool">
                      <Pencil size={13} />
                    </button>
                    <button class="icon-button" onclick={() => confirmDelete(pool.id)} title="Delete pool">
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
        <h3 class="font-bold text-lg">Create Model Pool</h3>
        <button class="icon-button" onclick={() => showAddModal = false}><X size={16} /></button>
      </div>

      <div class="flex flex-col gap-3 mb-5">
        <div class="form-group flex flex-col gap-1">
          <label class="text-[10px] font-bold uppercase tracking-wider" for="pool-pattern-input">Model Pattern</label>
          <input 
            type="text" 
            id="pool-pattern-input" 
            class="input-box w-full p-2.5 rounded-lg border text-sm" 
            placeholder="e.g. gpt-4o, claude-3-5-sonnet*" 
            bind:value={addForm.model_pattern} 
          />
        </div>
        <div class="form-group flex flex-col gap-1">
          <label class="text-[10px] font-bold uppercase tracking-wider" for="pool-strategy-select">Routing Strategy</label>
          <select id="pool-strategy-select" class="input-box w-full p-2.5 rounded-lg border text-sm" bind:value={addForm.strategy}>
            <option value="round-robin">round-robin</option>
            <option value="weighted-round-robin">weighted-round-robin</option>
            <option value="random">random</option>
          </select>
        </div>
        <div class="form-group flex flex-col gap-1">
          <label class="text-[10px] font-bold uppercase tracking-wider" for="pool-fallback-select">Fallback Pool (Optional)</label>
          <select id="pool-fallback-select" class="input-box w-full p-2.5 rounded-lg border text-sm" bind:value={addForm.fallback_pool_id}>
            <option value="">None</option>
            {#each pools as otherPool}
              <option value={otherPool.id}>{otherPool.model_pattern} (ID: {otherPool.id})</option>
            {/each}
          </select>
        </div>
      </div>

      <div class="flex justify-end gap-2 text-xs">
        <button class="px-4 py-2 rounded-lg border" onclick={() => showAddModal = false}>Cancel</button>
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
        <h3 class="font-bold text-lg">Edit Model Pool</h3>
        <button class="icon-button" onclick={() => showEditModal = false}><X size={16} /></button>
      </div>

      <div class="flex flex-col gap-3 mb-5">
        <div class="form-group flex flex-col gap-1">
          <label class="text-[10px] font-bold uppercase tracking-wider" for="edit-pattern-input">Model Pattern</label>
          <input type="text" id="edit-pattern-input" class="input-box w-full p-2.5 rounded-lg border text-sm" bind:value={editForm.model_pattern} />
        </div>
        <div class="form-group flex flex-col gap-1">
          <label class="text-[10px] font-bold uppercase tracking-wider" for="edit-strategy-select">Routing Strategy</label>
          <select id="edit-strategy-select" class="input-box w-full p-2.5 rounded-lg border text-sm" bind:value={editForm.strategy}>
            <option value="round-robin">round-robin</option>
            <option value="weighted-round-robin">weighted-round-robin</option>
            <option value="random">random</option>
          </select>
        </div>
        <div class="form-group flex flex-col gap-1">
          <label class="text-[10px] font-bold uppercase tracking-wider" for="edit-fallback-select">Fallback Pool (Optional)</label>
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
        <button class="px-4 py-2 rounded-lg border" onclick={() => showEditModal = false}>Cancel</button>
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
      <h3 class="font-bold text-base mb-2">Delete Model Pool?</h3>
      <p class="text-xs mb-5 opacity-75">All routing configurations and associated provider keys assigned to this model pool will be deleted. Upstream traffic will fallback or fail.</p>
      <div class="flex justify-center gap-2 text-xs">
        <button class="px-4 py-2 rounded-lg border" onclick={() => { showDeleteConfirm = false; deleteTargetId = null; }}>Cancel</button>
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
