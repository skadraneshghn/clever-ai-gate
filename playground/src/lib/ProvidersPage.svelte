<script>
  import { KeyRound, RefreshCw, Plus, Shield, AlertTriangle, Server, Pencil, Trash2, X } from '@lucide/svelte';
  import Button from './components/Button.svelte';
  import Input from './components/Input.svelte';
  import Card from './components/Card.svelte';
  import Modal from './components/Modal.svelte';

  let { 
    adminKey = $bindable(''), 
    apiKey, 
    loadModels, 
    addToast 
  } = $props();

  // ─── Local State ──────────────────────────────────────────────────────────
  let providerCredentials = $state([]);
  let providerPools = $state([]);
  let providerLoading = $state(false);
  let providerError = $state('');

  // Add/Edit modals
  let showAddProviderModal = $state(false);
  let addProviderTab = $state('standard'); // 'standard' | 'autodiscovery'
  let addProviderForm = $state({ pool_id: '', provider: 'openai', api_key: '', base_url: 'https://api.openai.com', weight: 1 });
  let addProviderLoading = $state(false);

  // Auto-discovery form
  let autoDiscoverForm = $state({ provider: 'nvidia', api_key: '', base_url: 'https://integrate.api.nvidia.com/v1', weight: 1, label: '' });
  let autoDiscoverLoading = $state(false);

  // Edit modal
  let showEditModal = $state(false);
  let editForm = $state({ id: 0, provider: '', api_key: '', base_url: '', weight: 1, is_healthy: true });
  let editLoading = $state(false);

  // Delete confirmation
  let showDeleteConfirm = $state(false);
  let deleteTargetId = $state(null);
  let deleteLoading = $state(false);

  // ─── Load state on adminKey change ─────────────────────────────────────────
  $effect(() => {
    if (adminKey.trim()) {
      loadCredentials();
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

  async function loadCredentials() {
    providerLoading = true;
    providerError = '';
    try {
      const res = await fetch('/api/v1/admin/credentials', { headers: adminHeaders() });
      if (res.ok) {
        providerCredentials = await res.json();
      } else {
        const err = await res.json();
        providerError = err.error || `Error ${res.status}`;
      }
    } catch (e) {
      providerError = `Network error: ${e.message}`;
    } finally {
      providerLoading = false;
    }
  }

  async function loadPools() {
    try {
      const res = await fetch('/api/v1/admin/pools', { headers: adminHeaders() });
      if (res.ok) {
        providerPools = await res.json();
      }
    } catch (e) {
      console.error('Failed to load pools', e);
    }
  }

  function openAddProviderModal() {
    addProviderForm = { pool_id: '', provider: 'openai', api_key: '', base_url: 'https://api.openai.com', weight: 1 };
    autoDiscoverForm = { provider: 'nvidia', api_key: '', base_url: 'https://integrate.api.nvidia.com/v1', weight: 1, label: '' };
    addProviderTab = 'standard';
    showAddProviderModal = true;
    loadPools();
  }

  async function createCredential() {
    addProviderLoading = true;
    try {
      const res = await fetch('/api/v1/admin/credentials', {
        method: 'POST',
        headers: adminHeaders(),
        body: JSON.stringify({
          pool_id: parseInt(addProviderForm.pool_id),
          provider: addProviderForm.provider,
          api_key: addProviderForm.api_key,
          base_url: addProviderForm.base_url,
          weight: addProviderForm.weight || 1
        })
      });
      if (res.ok || res.status === 201) {
        addToast('success', 'Credential created successfully');
        showAddProviderModal = false;
        loadCredentials();
      } else {
        const err = await res.json();
        addToast('error', err.details || err.error || 'Failed to create credential');
      }
    } catch (e) {
      addToast('error', `Network error: ${e.message}`);
    } finally {
      addProviderLoading = false;
    }
  }

  async function autoDiscoverProvider() {
    autoDiscoverLoading = true;
    let endpoint;
    if (autoDiscoverForm.provider === 'nvidia') {
      endpoint = '/api/v1/admin/providers/nvidia';
    } else if (autoDiscoverForm.provider === 'ollama') {
      endpoint = '/api/v1/admin/providers/ollama';
    } else {
      endpoint = '/api/v1/admin/providers/custom';
    }
    try {
      const payload = {
        provider: autoDiscoverForm.provider,
        api_key: autoDiscoverForm.api_key,
        base_url: autoDiscoverForm.base_url,
        weight: autoDiscoverForm.weight || 1
      };
      if (autoDiscoverForm.provider === 'custom' && autoDiscoverForm.label) {
        payload.label = autoDiscoverForm.label;
      }
      const res = await fetch(endpoint, {
        method: 'POST',
        headers: adminHeaders(),
        body: JSON.stringify(payload)
      });
      if (res.ok) {
        const data = await res.json();
        const displayName = autoDiscoverForm.provider === 'custom'
          ? (autoDiscoverForm.label || 'Custom')
          : autoDiscoverForm.provider.toUpperCase();
        addToast('success', `Successfully synchronized ${data.models_count || 0} ${displayName} models`);
        showAddProviderModal = false;
        loadCredentials();
        if (apiKey) loadModels();
      } else {
        const err = await res.json();
        addToast('error', err.details || err.error || 'Auto-discovery failed');
      }
    } catch (e) {
      addToast('error', `Network error: ${e.message}`);
    } finally {
      autoDiscoverLoading = false;
    }
  }

  function openEditModal(cred) {
    editForm = {
      id: cred.id,
      provider: cred.provider,
      api_key: '',
      base_url: cred.base_url,
      weight: cred.weight,
      is_healthy: cred.is_healthy
    };
    showEditModal = true;
  }

  async function updateCredential() {
    editLoading = true;
    try {
      const res = await fetch(`/api/v1/admin/credentials/${editForm.id}`, {
        method: 'PUT',
        headers: adminHeaders(),
        body: JSON.stringify({
          provider: editForm.provider,
          api_key: editForm.api_key || undefined,
          base_url: editForm.base_url,
          weight: editForm.weight,
          is_healthy: editForm.is_healthy
        })
      });
      if (res.ok) {
        addToast('success', 'Credential updated successfully');
        showEditModal = false;
        loadCredentials();
      } else {
        const err = await res.json();
        addToast('error', err.details || err.error || 'Failed to update credential');
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

  async function deleteCredentialById() {
    deleteLoading = true;
    try {
      const res = await fetch(`/api/v1/admin/credentials/${deleteTargetId}`, {
        method: 'DELETE',
        headers: adminHeaders()
      });
      if (res.ok) {
        addToast('success', 'Credential deleted successfully');
        showDeleteConfirm = false;
        deleteTargetId = null;
        loadCredentials();
      } else {
        const err = await res.json();
        addToast('error', err.details || err.error || 'Failed to delete credential');
      }
    } catch (e) {
      addToast('error', `Network error: ${e.message}`);
    } finally {
      deleteLoading = false;
    }
  }

  function providerBadgeClass(provider) {
    switch ((provider || '').toLowerCase()) {
      case 'openai': return 'badge-openai';
      case 'nvidia': return 'badge-nvidia';
      case 'ollama': return 'badge-ollama';
      case 'anthropic': return 'badge-anthropic';
      case 'custom': return 'badge-custom';
      default: return 'badge-default';
    }
  }

  function connectAdminKey() {
    const key = adminKey.trim();
    if (!key) return;
    localStorage.setItem('cag_admin_key', key);
    loadCredentials();
    loadPools();
  }
</script>

<header class="header flex items-center justify-between px-6 py-4 border-b shrink-0">
  <div class="flex items-center gap-3">
    <KeyRound size={20} class="text-[#f97316]" />
    <span class="font-bold text-base">Provider Credentials</span>
    {#if adminKey.trim()}
      <span class="text-xs font-bold text-secondary bg-gray-500/10 border border-gray-500/20 px-2.5 py-0.5 rounded-full uppercase">{providerCredentials.length} registered</span>
    {/if}
  </div>
  
  {#if adminKey.trim()}
    <div class="flex items-center gap-2">
      <Button variant="secondary" size="sm" onclick={() => { loadCredentials(); addToast('info', 'Refreshing credentials...'); }}>
        <RefreshCw size={14} />
        Refresh
      </Button>
      <Button variant="primary" size="sm" onclick={openAddProviderModal}>
        <Plus size={14} />
        Add Provider
      </Button>
    </div>
  {/if}
</header>

{#if !adminKey.trim()}
  <!-- Admin key prompt -->
  <div class="logs-key-prompt">
    <Card variant="filled" padding="lg" class="logs-key-card">
      <Shield size={40} class="text-[#f97316] mb-4" />
      <h2 class="font-bold text-lg mb-2 text-primary">Admin Key Required</h2>
      <p class="text-sm mb-6 text-secondary max-w-sm">Enter your Admin API Key to manage provider credentials, pools, and API keys.</p>
      
      <div class="flex flex-col gap-3 w-full max-w-sm">
        <Input
          type="password"
          placeholder="Enter Admin API Key..."
          bind:value={adminKey}
          onkeydown={(e) => { if (e.key === 'Enter') connectAdminKey(); }}
        />
        <Button variant="primary" size="md" onclick={connectAdminKey}>
          Connect
        </Button>
      </div>
      
      {#if providerError}
        <p class="text-red-500 text-sm font-semibold mt-4">{providerError}</p>
      {/if}
    </Card>
  </div>
{:else}
  <!-- Providers data grid -->
  <div class="providers-grid-wrap">
    {#if providerLoading}
      <div class="providers-loading">
        <div class="animate-spin text-[#f97316] text-xl">⟳</div>
        <p class="text-sm mt-2 text-secondary">Loading credentials...</p>
      </div>
    {:else if providerError}
      <div class="providers-loading">
        <AlertTriangle size={40} class="text-red-500 mb-2" />
        <p class="text-red-500 text-sm font-semibold">{providerError}</p>
        <Button variant="primary" class="mt-4" onclick={loadCredentials}>Retry</Button>
      </div>
    {:else if providerCredentials.length === 0}
      <div class="providers-loading">
        <Server size={48} class="opacity-20 mb-4" />
        <p class="opacity-50 text-sm text-secondary">No credentials registered yet.</p>
        <Button variant="primary" class="mt-4" onclick={openAddProviderModal}>
          <Plus size={14} /> Add First Provider
        </Button>
      </div>
    {:else}
      <div class="providers-table-container">
        <table class="providers-table">
          <thead>
            <tr>
              <th style="font-size: 11px;">ID</th>
              <th style="font-size: 11px;">Provider</th>
              <th style="font-size: 11px;">Model Pattern</th>
              <th style="font-size: 11px;">Base URL</th>
              <th style="font-size: 11px; text-align: center;">Weight</th>
              <th style="font-size: 11px; text-align: center;">Health</th>
              <th style="font-size: 11px;">Key</th>
              <th style="font-size: 11px; text-align: center;">Actions</th>
            </tr>
          </thead>
          <tbody>
            {#each providerCredentials as cred (cred.id)}
              <tr class="provider-row">
                <td class="font-mono text-xs opacity-60">#{cred.id}</td>
                <td>
                  <span class="provider-badge {providerBadgeClass(cred.provider)}">{cred.provider}</span>
                </td>
                <td class="font-mono text-sm">{cred.model_pattern || '—'}</td>
                <td class="text-sm truncate" style="max-width: 250px;" title={cred.base_url}>{cred.base_url}</td>
                <td class="text-center font-mono text-sm">{cred.weight}</td>
                <td class="text-center">
                  <span class="health-dot {cred.is_healthy ? 'healthy' : 'unhealthy'}" title={cred.is_healthy ? 'Healthy' : (cred.last_error || 'Unhealthy')}></span>
                </td>
                <td class="font-mono text-xs opacity-50">{cred.key_mask}</td>
                <td>
                  <div class="flex items-center justify-center gap-1">
                    <Button variant="ghost" size="sm" onclick={() => openEditModal(cred)} title="Edit credential">
                      <Pencil size={15} />
                    </Button>
                    <Button variant="ghost" size="sm" onclick={() => confirmDelete(cred.id)} title="Delete credential">
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

<!-- ─── ADD PROVIDER MODAL ────────────────────────────────────────────────── -->
<Modal bind:show={showAddProviderModal}>
  {#snippet header()}
    <div class="flex border-b text-xs w-full">
      <button 
        class="tab-btn px-6 py-3 flex-grow font-semibold text-center {addProviderTab === 'standard' ? 'active' : ''}" 
        onclick={() => addProviderTab = 'standard'}
      >
        Standard Provider
      </button>
      <button 
        class="tab-btn px-6 py-3 flex-grow font-semibold text-center {addProviderTab === 'autodiscovery' ? 'active' : ''}" 
        onclick={() => addProviderTab = 'autodiscovery'}
      >
        Auto-Discovery
      </button>
    </div>
  {/snippet}

  {#if addProviderTab === 'standard'}
    <div class="flex flex-col gap-4">
      <p class="text-sm text-secondary leading-relaxed">Add a single credential to an existing model pool.</p>
      
      <Input type="select" label="Pool" bind:value={addProviderForm.pool_id} placeholder="Select a pool...">
        {#each providerPools as pool}
          <option value={pool.id}>{pool.model_pattern} (ID: {pool.id})</option>
        {/each}
      </Input>

      <Input type="select" label="Provider" bind:value={addProviderForm.provider}>
        <option value="openai">OpenAI</option>
        <option value="anthropic">Anthropic</option>
        <option value="nvidia">NVIDIA</option>
        <option value="ollama">Ollama</option>
        <option value="google">Google</option>
        <option value="custom">Custom</option>
      </Input>

      <Input type="password" label="API Key" placeholder="sk-..." bind:value={addProviderForm.api_key} />
      
      <Input type="text" label="Base URL" placeholder="https://api.openai.com" bind:value={addProviderForm.base_url} />
      
      <Input type="number" label="Weight" min="1" bind:value={addProviderForm.weight} />
    </div>
  {:else}
    <div class="flex flex-col gap-4">
      <p class="text-sm text-secondary leading-relaxed">Auto-discover all models from an NVIDIA NIM, Ollama Cloud, or any OpenAI-compatible provider. Pools are created automatically.</p>
      
      <Input type="select" label="Provider Type" bind:value={autoDiscoverForm.provider} onchange={() => {
        if (autoDiscoverForm.provider === 'nvidia') {
          autoDiscoverForm.base_url = 'https://integrate.api.nvidia.com/v1';
        } else if (autoDiscoverForm.provider === 'ollama') {
          autoDiscoverForm.base_url = 'https://ollama.com';
        } else {
          autoDiscoverForm.base_url = '';
        }
        autoDiscoverForm.label = '';
      }}>
        <option value="nvidia">NVIDIA NIM</option>
        <option value="ollama">Ollama Cloud</option>
        <option value="custom">OpenAI-Compatible (Custom)</option>
      </Input>

      {#if autoDiscoverForm.provider === 'custom'}
        <Input type="text" label="Label (optional)" placeholder="e.g. Together AI, DeepInfra" bind:value={autoDiscoverForm.label} />
      {/if}

      <Input 
        type="password" 
        label="API Key" 
        placeholder={autoDiscoverForm.provider === 'nvidia' ? 'nvapi-...' : autoDiscoverForm.provider === 'ollama' ? 'Ollama Cloud API key...' : 'Bearer API key...'} 
        bind:value={autoDiscoverForm.api_key} 
      />
      
      <Input type="text" label="Base URL" placeholder={autoDiscoverForm.provider === 'custom' ? 'https://api.together.xyz/v1' : ''} bind:value={autoDiscoverForm.base_url} />
      
      <Input type="number" label="Weight" min="1" bind:value={autoDiscoverForm.weight} />
    </div>
  {/if}

  {#snippet footer()}
    <div class="flex justify-end gap-3 w-full">
      <Button variant="outline" onclick={() => showAddProviderModal = false}>Cancel</Button>
      {#if addProviderTab === 'standard'}
        <Button variant="primary" onclick={createCredential} disabled={addProviderLoading || !addProviderForm.pool_id}>
          {#if addProviderLoading}
            <span class="animate-spin">⟳</span> Creating...
          {:else}
            Create Credential
          {/if}
        </Button>
      {:else}
        <Button variant="primary" onclick={autoDiscoverProvider} disabled={autoDiscoverLoading}>
          {#if autoDiscoverLoading}
            <span class="animate-spin">⟳</span> Discovering...
          {:else}
            Discover & Register
          {/if}
        </Button>
      {/if}
    </div>
  {/snippet}
</Modal>

<!-- ─── EDIT CREDENTIAL MODAL ──────────────────────────────────────────────── -->
<Modal bind:show={showEditModal} title="Edit Credential">
  <div class="flex flex-col gap-4">
    <Input type="select" label="Provider" bind:value={editForm.provider}>
      <option value="openai">OpenAI</option>
      <option value="anthropic">Anthropic</option>
      <option value="nvidia">NVIDIA</option>
      <option value="ollama">Ollama</option>
      <option value="google">Google</option>
      <option value="custom">Custom</option>
    </Input>

    <Input type="password" label="New API Key" placeholder="Leave blank to keep current key" bind:value={editForm.api_key} />
    
    <Input type="text" label="Base URL" bind:value={editForm.base_url} />
    
    <div class="flex gap-4 items-end">
      <div class="flex-grow">
        <Input type="number" label="Weight" min="1" bind:value={editForm.weight} />
      </div>
      <div class="flex flex-col gap-2 shrink-0">
        <span class="text-xs font-bold uppercase tracking-wider text-secondary">Healthy</span>
        <label class="toggle-switch" style="margin-bottom: 9px;">
          <input type="checkbox" bind:checked={editForm.is_healthy} />
          <span class="toggle-slider"></span>
        </label>
      </div>
    </div>
  </div>

  {#snippet footer()}
    <div class="flex justify-end gap-3 w-full">
      <Button variant="outline" onclick={() => showEditModal = false}>Cancel</Button>
      <Button variant="primary" onclick={updateCredential} disabled={editLoading}>
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
<Modal bind:show={showDeleteConfirm} title="Delete Credential?">
  <div class="flex flex-col items-center gap-4 text-center">
    <AlertTriangle size={48} class="text-red-500 mb-2" />
    <p class="text-sm text-secondary">
      This credential will be removed from gateway routing immediately. Traffic will be routed to other healthy keys in the pool.
    </p>
    <p class="text-xs text-red-500 font-bold">This action is permanent and cannot be undone.</p>
  </div>

  {#snippet footer()}
    <div class="flex justify-center gap-3 w-full">
      <Button variant="outline" onclick={() => { showDeleteConfirm = false; deleteTargetId = null; }}>Cancel</Button>
      <Button variant="danger" onclick={deleteCredentialById} disabled={deleteLoading}>
        {#if deleteLoading}
          <span class="animate-spin">⟳</span>
        {:else}
          Delete
        {/if}
      </Button>
    </div>
  {/snippet}
</Modal>

<style>
  .tab-btn {
    border: none;
    color: var(--text-secondary);
    background: transparent;
    transition: all 0.2s;
    font-weight: 600;
    cursor: pointer;
    border-bottom: 2px solid transparent;
  }
  .tab-btn.active {
    color: #f97316;
    border-bottom: 2px solid #f97316;
  }

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
</style>
