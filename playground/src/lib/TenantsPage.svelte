<script>
  import { 
    Users, Plus, RefreshCw, Shield, AlertTriangle, Trash2, 
    Pencil, Copy, Check, X, Activity 
  } from '@lucide/svelte';

  let { 
    adminKey = $bindable(''), 
    addToast 
  } = $props();

  // ─── Local State ──────────────────────────────────────────────────────────
  let tenants = $state([]);
  let loading = $state(false);
  let error = $state('');

  // Add modal state
  let showAddModal = $state(false);
  let addForm = $state({ name: '', token_balance: 1000000000, rate_limit_rpm: 60 });
  let addLoading = $state(false);

  // Success API Key Modal
  let showKeyModal = $state(false);
  let createdTenant = $state({ name: '', api_key: '' });
  let keyCopied = $state(false);

  // Edit modal state
  let showEditModal = $state(false);
  let editForm = $state({ id: '', name: '', token_balance: 1000000000, rate_limit_rpm: 60, is_active: true });
  let editLoading = $state(false);

  // Delete modal state
  let showDeleteConfirm = $state(false);
  let deleteTargetId = $state(null);
  let deleteLoading = $state(false);

  // Auto-fetch when adminKey changes
  $effect(() => {
    if (adminKey.trim()) {
      loadTenants();
    }
  });

  // ─── API Helper Headers ───────────────────────────────────────────────────
  function adminHeaders() {
    return {
      'Authorization': `Bearer ${adminKey.trim()}`,
      'Content-Type': 'application/json'
    };
  }

  async function loadTenants() {
    loading = true;
    error = '';
    try {
      const res = await fetch('/api/v1/admin/tenants', { headers: adminHeaders() });
      if (res.ok) {
        tenants = await res.json();
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
    addForm = { name: '', token_balance: 1000000000, rate_limit_rpm: 60 };
    showAddModal = true;
  }

  async function createTenant() {
    if (!addForm.name.trim()) {
      addToast('error', 'Name is required');
      return;
    }
    addLoading = true;
    try {
      const res = await fetch('/api/v1/admin/tenants', {
        method: 'POST',
        headers: adminHeaders(),
        body: JSON.stringify({
          name: addForm.name,
          token_balance: Number(addForm.token_balance),
          rate_limit_rpm: Number(addForm.rate_limit_rpm)
        })
      });
      if (res.status === 201 || res.ok) {
        const data = await res.json();
        createdTenant = { name: data.name, api_key: data.api_key };
        addToast('success', 'Tenant created successfully');
        showAddModal = false;
        showKeyModal = true;
        loadTenants();
      } else {
        const err = await res.json();
        addToast('error', err.details || err.error || 'Failed to create tenant');
      }
    } catch (e) {
      addToast('error', `Network error: ${e.message}`);
    } finally {
      addLoading = false;
    }
  }

  function copyTenantKey() {
    navigator.clipboard.writeText(createdTenant.api_key);
    keyCopied = true;
    addToast('success', 'Copied API key to clipboard');
    setTimeout(() => { keyCopied = false; }, 2000);
  }

  function openEditModal(tenant) {
    editForm = {
      id: tenant.id,
      name: tenant.name,
      token_balance: tenant.token_balance,
      rate_limit_rpm: tenant.rate_limit_rpm,
      is_active: tenant.is_active
    };
    showEditModal = true;
  }

  async function updateTenant() {
    if (!editForm.name.trim()) {
      addToast('error', 'Name is required');
      return;
    }
    editLoading = true;
    try {
      const res = await fetch(`/api/v1/admin/tenants/${editForm.id}`, {
        method: 'PUT',
        headers: adminHeaders(),
        body: JSON.stringify({
          name: editForm.name,
          token_balance: Number(editForm.token_balance),
          rate_limit_rpm: Number(editForm.rate_limit_rpm),
          is_active: editForm.is_active
        })
      });
      if (res.ok) {
        addToast('success', 'Tenant updated successfully');
        showEditModal = false;
        loadTenants();
      } else {
        const err = await res.json();
        addToast('error', err.details || err.error || 'Failed to update tenant');
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

  async function deleteTenantById() {
    deleteLoading = true;
    try {
      const res = await fetch(`/api/v1/admin/tenants/${deleteTargetId}`, {
        method: 'DELETE',
        headers: adminHeaders()
      });
      if (res.ok) {
        addToast('success', 'Tenant deleted successfully');
        showDeleteConfirm = false;
        deleteTargetId = null;
        loadTenants();
      } else {
        const err = await res.json();
        addToast('error', err.details || err.error || 'Failed to delete tenant');
      }
    } catch (e) {
      addToast('error', `Network error: ${e.message}`);
    } finally {
      deleteLoading = false;
    }
  }

  function formatBalance(num) {
    if (num >= 1e9) return (num / 1e9).toFixed(1) + 'B';
    if (num >= 1e6) return (num / 1e6).toFixed(1) + 'M';
    if (num >= 1e3) return (num / 1e3).toFixed(1) + 'K';
    return num.toString();
  }

  function connectAdminKey() {
    const key = adminKey.trim();
    if (!key) return;
    localStorage.setItem('cag_admin_key', key);
    loadTenants();
  }
</script>

<header class="header flex items-center justify-between px-6 py-3 border-b shrink-0">
  <div class="flex items-center gap-3">
    <Users size={18} class="text-[#f97316]" />
    <span class="font-bold text-sm">Tenant Accounts</span>
    <span class="text-[10px] font-bold text-secondary uppercase">{tenants.length} tenants</span>
  </div>
  <div class="flex items-center gap-2">
    {#if adminKey.trim()}
      <button class="log-action-btn log-btn-start" onclick={() => { loadTenants(); addToast('info', 'Refreshing tenants...'); }}>
        <RefreshCw size={12} />
        Refresh
      </button>
      <button class="log-action-btn" style="border-color: rgba(249,115,22,0.4); color: #f97316; background: rgba(249,115,22,0.06);" onclick={openAddModal}>
        <Plus size={12} />
        Create Tenant
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
      <p class="text-xs mb-4">Enter your Admin API Key to manage tenant accounts, virtual routing keys, and usage statistics.</p>
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
  <!-- Tenants data grid -->
  <div class="providers-grid-wrap">
    {#if loading}
      <div class="providers-loading">
        <div class="animate-spin text-[#f97316]" style="font-size:24px;">⟳</div>
        <p class="text-xs mt-2">Loading tenant accounts...</p>
      </div>
    {:else if error}
      <div class="providers-loading">
        <AlertTriangle size={32} class="text-red-500 mb-2" />
        <p class="text-red-500 text-xs">{error}</p>
        <button class="mt-3 px-4 py-2 rounded-lg text-white bg-[#f97316] font-semibold text-xs" onclick={loadTenants}>Retry</button>
      </div>
    {:else if tenants.length === 0}
      <div class="providers-loading">
        <Users size={40} class="opacity-20 mb-3" />
        <p class="opacity-40 text-xs">No tenants registered yet.</p>
        <button class="mt-4 px-4 py-2 rounded-lg text-white bg-[#f97316] font-semibold text-xs" onclick={openAddModal}>
          <span class="flex items-center gap-1.5"><Plus size={12} /> Create First Tenant</span>
        </button>
      </div>
    {:else}
      <div class="providers-table-container">
        <table class="providers-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Name</th>
              <th>Token Balance</th>
              <th>Rate Limit (RPM)</th>
              <th>Status</th>
              <th>API Key</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {#each tenants as tenant (tenant.id)}
              <tr class="provider-row">
                <td class="font-mono text-[9px] opacity-60" title={tenant.id}>#{tenant.id.slice(0, 8)}...</td>
                <td class="font-bold text-xs">{tenant.name}</td>
                <td class="font-mono text-xs">{formatBalance(tenant.token_balance)}</td>
                <td class="font-mono text-xs">{tenant.rate_limit_rpm || 'No Limit'}</td>
                <td>
                  <span class="provider-badge {tenant.is_active ? 'badge-openai' : 'badge-default'}">
                    {tenant.is_active ? 'Active' : 'Suspended'}
                  </span>
                </td>
                <td class="font-mono text-[10px] opacity-60">{tenant.api_key}</td>
                <td>
                  <div class="flex items-center gap-1">
                    <button class="icon-button" onclick={() => openEditModal(tenant)} title="Edit tenant">
                      <Pencil size={13} />
                    </button>
                    <button class="icon-button" onclick={() => confirmDelete(tenant.id)} title="Delete tenant">
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

<!-- ─── CREATE TENANT MODAL ────────────────────────────────────────────────── -->
{#if showAddModal}
  <div class="modal-backdrop fixed inset-0 flex items-center justify-center p-4 z-50 bg-black-trans backdrop-blur-sm">
    <div class="modal-content w-full max-w-sm rounded-xl border p-6 shadow-2xl relative">
      <div class="flex items-center justify-between mb-4">
        <h3 class="font-bold text-lg">Create Tenant</h3>
        <button class="icon-button" onclick={() => showAddModal = false}><X size={16} /></button>
      </div>

      <div class="flex flex-col gap-3 mb-5">
        <div class="form-group flex flex-col gap-1">
          <label class="text-[10px] font-bold uppercase tracking-wider" for="tenant-name-input">Tenant Name</label>
          <input 
            type="text" 
            id="tenant-name-input" 
            class="input-box w-full p-2.5 rounded-lg border text-sm" 
            placeholder="Acme Corp, Mobile Client..." 
            bind:value={addForm.name} 
          />
        </div>
        <div class="form-group flex flex-col gap-1">
          <label class="text-[10px] font-bold uppercase tracking-wider" for="tenant-balance-input">Token Balance</label>
          <input 
            type="number" 
            id="tenant-balance-input" 
            class="input-box w-full p-2.5 rounded-lg border text-sm" 
            bind:value={addForm.token_balance} 
          />
        </div>
        <div class="form-group flex flex-col gap-1">
          <label class="text-[10px] font-bold uppercase tracking-wider" for="tenant-rpm-input">Rate Limit (RPM)</label>
          <input 
            type="number" 
            id="tenant-rpm-input" 
            class="input-box w-full p-2.5 rounded-lg border text-sm" 
            bind:value={addForm.rate_limit_rpm} 
          />
        </div>
      </div>

      <div class="flex justify-end gap-2 text-xs">
        <button class="px-4 py-2 rounded-lg border" onclick={() => showAddModal = false}>Cancel</button>
        <button class="px-4 py-2 rounded-lg text-white bg-[#f97316] font-semibold flex items-center gap-1.5 min-w-[120px] justify-center" onclick={createTenant} disabled={addLoading}>
          {#if addLoading}
            <span class="animate-spin">🔄</span> Creating...
          {:else}
            Create Tenant
          {/if}
        </button>
      </div>
    </div>
  </div>
{/if}

<!-- ─── SUCCESS KEY MODAL ──────────────────────────────────────────────────── -->
{#if showKeyModal}
  <div class="modal-backdrop fixed inset-0 flex items-center justify-center p-4 z-50 bg-black-trans backdrop-blur-sm">
    <div class="modal-content w-full max-w-sm rounded-xl border p-6 shadow-2xl relative text-center">
      <Users size={32} class="text-[#04d361] mx-auto mb-3" />
      <h3 class="font-bold text-lg mb-1">Tenant Created!</h3>
      <p class="text-xs mb-4 opacity-75">Tenant <strong>{createdTenant.name}</strong> was created. Copy their API Key below. You will not be able to see it again.</p>

      <div class="flex gap-2 p-3 bg-zinc-900 border border-zinc-800 rounded-lg font-mono text-xs select-text justify-between items-center mb-5" style="background-color: #0c0c0f;">
        <span class="truncate text-green-500 font-bold pr-2">{createdTenant.api_key}</span>
        <button class="icon-button shrink-0 hover:bg-zinc-800" onclick={copyTenantKey}>
          {#if keyCopied}
            <Check size={14} class="text-green-500" />
          {:else}
            <Copy size={14} />
          {/if}
        </button>
      </div>

      <div class="flex justify-center text-xs">
        <button class="px-5 py-2 rounded-lg text-white bg-[#f97316] font-semibold" onclick={() => showKeyModal = false}>
          Done
        </button>
      </div>
    </div>
  </div>
{/if}

<!-- ─── EDIT TENANT MODAL ──────────────────────────────────────────────────── -->
{#if showEditModal}
  <div class="modal-backdrop fixed inset-0 flex items-center justify-center p-4 z-50 bg-black-trans backdrop-blur-sm">
    <div class="modal-content w-full max-w-sm rounded-xl border p-6 shadow-2xl relative">
      <div class="flex items-center justify-between mb-4">
        <h3 class="font-bold text-lg">Edit Tenant</h3>
        <button class="icon-button" onclick={() => showEditModal = false}><X size={16} /></button>
      </div>

      <div class="flex flex-col gap-3 mb-5">
        <div class="form-group flex flex-col gap-1">
          <label class="text-[10px] font-bold uppercase tracking-wider" for="edit-name-input">Tenant Name</label>
          <input type="text" id="edit-name-input" class="input-box w-full p-2.5 rounded-lg border text-sm" bind:value={editForm.name} />
        </div>
        <div class="form-group flex flex-col gap-1">
          <label class="text-[10px] font-bold uppercase tracking-wider" for="edit-balance-input">Token Balance</label>
          <input type="number" id="edit-balance-input" class="input-box w-full p-2.5 rounded-lg border text-sm" bind:value={editForm.token_balance} />
        </div>
        <div class="flex gap-3">
          <div class="form-group flex flex-col gap-1 flex-grow">
            <label class="text-[10px] font-bold uppercase tracking-wider" for="edit-rpm-input">Rate Limit (RPM)</label>
            <input type="number" id="edit-rpm-input" class="input-box w-full p-2.5 rounded-lg border text-sm" bind:value={editForm.rate_limit_rpm} />
          </div>
          <div class="form-group flex flex-col gap-1">
            <label class="text-[10px] font-bold uppercase tracking-wider" for="edit-status-toggle">Status</label>
            <label class="toggle-switch mt-1" id="edit-status-toggle">
              <input type="checkbox" bind:checked={editForm.is_active} />
              <span class="toggle-slider"></span>
            </label>
          </div>
        </div>
      </div>

      <div class="flex justify-end gap-2 text-xs">
        <button class="px-4 py-2 rounded-lg border" onclick={() => showEditModal = false}>Cancel</button>
        <button class="px-4 py-2 rounded-lg text-white bg-[#f97316] font-semibold flex items-center gap-1.5 min-w-[120px] justify-center" onclick={updateTenant} disabled={editLoading}>
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
      <h3 class="font-bold text-base mb-2">Delete Tenant?</h3>
      <p class="text-xs mb-5 opacity-75">All access virtual routing credentials, statistics, and playground histories associated with this tenant account will be deleted permanently.</p>
      <div class="flex justify-center gap-2 text-xs">
        <button class="px-4 py-2 rounded-lg border" onclick={() => { showDeleteConfirm = false; deleteTargetId = null; }}>Cancel</button>
        <button class="px-4 py-2 rounded-lg text-white bg-red-500 font-semibold flex items-center gap-1.5 min-w-[100px] justify-center" style="background-color: #ef4444;" onclick={deleteTenantById} disabled={deleteLoading}>
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
