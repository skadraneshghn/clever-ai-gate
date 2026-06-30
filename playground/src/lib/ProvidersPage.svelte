<script>
  import { KeyRound, RefreshCw, Plus, Shield, AlertTriangle, Server, Pencil, Trash2, X } from '@lucide/svelte';

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
      // Include label for custom providers
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

<header class="header flex items-center justify-between px-6 py-3 border-b shrink-0">
  <div class="flex items-center gap-3">
    <KeyRound size={18} class="text-[#f97316]" />
    <span class="font-bold text-sm">Provider Credentials</span>
    <span class="text-[10px] font-bold text-secondary uppercase">{providerCredentials.length} registered</span>
  </div>
  <div class="flex items-center gap-2">
    {#if adminKey.trim()}
      <button class="log-action-btn log-btn-start" onclick={() => { loadCredentials(); addToast('info', 'Refreshing credentials...'); }}>
        <RefreshCw size={12} />
        Refresh
      </button>
      <button class="log-action-btn" style="border-color: rgba(249,115,22,0.4); color: #f97316; background: rgba(249,115,22,0.06);" onclick={openAddProviderModal}>
        <Plus size={12} />
        Add Provider
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
      <p class="text-xs mb-4">Enter your Admin API Key to manage provider credentials, pools, and API keys.</p>
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
      {#if providerError}
        <p class="text-red-500 text-xs mt-3">{providerError}</p>
      {/if}
    </div>
  </div>
{:else}
  <!-- Providers data grid -->
  <div class="providers-grid-wrap">
    {#if providerLoading}
      <div class="providers-loading">
        <div class="animate-spin text-[#f97316]" style="font-size:24px;">⟳</div>
        <p class="text-xs mt-2">Loading credentials...</p>
      </div>
    {:else if providerError}
      <div class="providers-loading">
        <AlertTriangle size={32} class="text-red-500 mb-2" />
        <p class="text-red-500 text-xs">{providerError}</p>
        <button class="mt-3 px-4 py-2 rounded-lg text-white bg-[#f97316] font-semibold text-xs" onclick={loadCredentials}>Retry</button>
      </div>
    {:else if providerCredentials.length === 0}
      <div class="providers-loading">
        <Server size={40} class="opacity-20 mb-3" />
        <p class="opacity-40 text-xs">No credentials registered yet.</p>
        <button class="mt-4 px-4 py-2 rounded-lg text-white bg-[#f97316] font-semibold text-xs" onclick={openAddProviderModal}>
          <span class="flex items-center gap-1.5"><Plus size={12} /> Add First Provider</span>
        </button>
      </div>
    {:else}
      <div class="providers-table-container">
        <table class="providers-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Provider</th>
              <th>Model Pattern</th>
              <th>Base URL</th>
              <th>Weight</th>
              <th>Health</th>
              <th>Key</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {#each providerCredentials as cred (cred.id)}
              <tr class="provider-row">
                <td class="font-mono text-[10px] opacity-60">#{cred.id}</td>
                <td>
                  <span class="provider-badge {providerBadgeClass(cred.provider)}">{cred.provider}</span>
                </td>
                <td class="font-mono text-xs">{cred.model_pattern || '—'}</td>
                <td class="text-xs truncate" style="max-width: 200px;" title={cred.base_url}>{cred.base_url}</td>
                <td class="text-center font-mono text-xs">{cred.weight}</td>
                <td class="text-center">
                  <span class="health-dot {cred.is_healthy ? 'healthy' : 'unhealthy'}" title={cred.is_healthy ? 'Healthy' : (cred.last_error || 'Unhealthy')}></span>
                </td>
                <td class="font-mono text-[10px] opacity-50">{cred.key_mask}</td>
                <td>
                  <div class="flex items-center gap-1">
                    <button class="icon-button" onclick={() => openEditModal(cred)} title="Edit credential">
                      <Pencil size={13} />
                    </button>
                    <button class="icon-button" onclick={() => confirmDelete(cred.id)} title="Delete credential">
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

<!-- ═══════════════════════════════════════════════════════════════════════ -->
<!-- ADD PROVIDER MODAL                                                      -->
<!-- ═══════════════════════════════════════════════════════════════════════ -->
{#if showAddProviderModal}
  <div class="modal-backdrop fixed inset-0 flex items-center justify-center p-4 z-50 bg-black-trans backdrop-blur-sm">
    <div class="modal-content w-full max-w-md rounded-xl border p-6 shadow-2xl relative">
      <div class="flex items-center justify-between mb-4">
        <h3 class="font-bold text-lg">Add Provider</h3>
        <button class="icon-button" onclick={() => showAddProviderModal = false}><X size={16} /></button>
      </div>

      <!-- Tabs -->
      <div class="flex border-b text-[10px] mb-4">
        <button class="tab-btn px-4 py-2 flex-grow font-semibold text-center {addProviderTab === 'standard' ? 'active' : ''}" onclick={() => addProviderTab = 'standard'}>
          Standard Provider
        </button>
        <button class="tab-btn px-4 py-2 flex-grow font-semibold text-center {addProviderTab === 'autodiscovery' ? 'active' : ''}" onclick={() => addProviderTab = 'autodiscovery'}>
          Auto-Discovery
        </button>
      </div>

      {#if addProviderTab === 'standard'}
        <p class="text-xs mb-4">Add a single credential to an existing model pool.</p>
        <div class="flex flex-col gap-3 mb-5">
          <div class="form-group flex flex-col gap-1">
            <label class="text-[10px] font-bold uppercase tracking-wider" for="pool-select">Pool</label>
            <select id="pool-select" class="input-box w-full p-2.5 rounded-lg border text-sm" bind:value={addProviderForm.pool_id}>
              <option value="">Select a pool...</option>
              {#each providerPools as pool}
                <option value={pool.id}>{pool.model_pattern} (ID: {pool.id})</option>
              {/each}
            </select>
          </div>
          <div class="form-group flex flex-col gap-1">
            <label class="text-[10px] font-bold uppercase tracking-wider" for="provider-select">Provider</label>
            <select id="provider-select" class="input-box w-full p-2.5 rounded-lg border text-sm" bind:value={addProviderForm.provider}>
              <option value="openai">OpenAI</option>
              <option value="anthropic">Anthropic</option>
              <option value="nvidia">NVIDIA</option>
              <option value="ollama">Ollama</option>
              <option value="google">Google</option>
              <option value="custom">Custom</option>
            </select>
          </div>
          <div class="form-group flex flex-col gap-1">
            <label class="text-[10px] font-bold uppercase tracking-wider" for="api-key-input">API Key</label>
            <input type="password" id="api-key-input" class="input-box w-full p-2.5 rounded-lg border text-sm" placeholder="sk-..." bind:value={addProviderForm.api_key} />
          </div>
          <div class="form-group flex flex-col gap-1">
            <label class="text-[10px] font-bold uppercase tracking-wider" for="base-url-input">Base URL</label>
            <input type="text" id="base-url-input" class="input-box w-full p-2.5 rounded-lg border text-sm" placeholder="https://api.openai.com" bind:value={addProviderForm.base_url} />
          </div>
          <div class="form-group flex flex-col gap-1">
            <label class="text-[10px] font-bold uppercase tracking-wider" for="weight-input">Weight</label>
            <input type="number" id="weight-input" class="input-box w-full p-2.5 rounded-lg border text-sm" min="1" bind:value={addProviderForm.weight} />
          </div>
        </div>
        <div class="flex justify-end gap-2 text-xs">
          <button class="px-4 py-2 rounded-lg border" onclick={() => showAddProviderModal = false}>Cancel</button>
          <button class="px-4 py-2 rounded-lg text-white bg-[#f97316] font-semibold flex items-center gap-1.5 min-w-[120px] justify-center" onclick={createCredential} disabled={addProviderLoading || !addProviderForm.pool_id}>
            {#if addProviderLoading}
              <span class="animate-spin">🔄</span> Creating...
            {:else}
              Create Credential
            {/if}
          </button>
        </div>
      {:else}
        <p class="text-xs mb-4">Auto-discover all models from an NVIDIA NIM, Ollama Cloud, or any OpenAI-compatible provider. Pools are created automatically.</p>
        <div class="flex flex-col gap-3 mb-5">
          <div class="form-group flex flex-col gap-1">
            <label class="text-[10px] font-bold uppercase tracking-wider" for="auto-provider-select">Provider Type</label>
            <select id="auto-provider-select" class="input-box w-full p-2.5 rounded-lg border text-sm" bind:value={autoDiscoverForm.provider} onchange={() => {
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
            </select>
          </div>
          {#if autoDiscoverForm.provider === 'custom'}
            <div class="form-group flex flex-col gap-1">
              <label class="text-[10px] font-bold uppercase tracking-wider" for="auto-label-input">Label <span class="opacity-50">(optional — e.g. "Together AI", "DeepInfra")</span></label>
              <input type="text" id="auto-label-input" class="input-box w-full p-2.5 rounded-lg border text-sm" placeholder="e.g. together-ai, deepinfra, vllm" bind:value={autoDiscoverForm.label} />
            </div>
          {/if}
          <div class="form-group flex flex-col gap-1">
            <label class="text-[10px] font-bold uppercase tracking-wider" for="auto-api-key-input">API Key {autoDiscoverForm.provider === 'ollama' ? '(required for cloud)' : ''}</label>
            <input type="password" id="auto-api-key-input" class="input-box w-full p-2.5 rounded-lg border text-sm" placeholder={autoDiscoverForm.provider === 'nvidia' ? 'nvapi-...' : autoDiscoverForm.provider === 'ollama' ? 'Ollama Cloud API key...' : 'Bearer API key...'} bind:value={autoDiscoverForm.api_key} />
          </div>
          <div class="form-group flex flex-col gap-1">
            <label class="text-[10px] font-bold uppercase tracking-wider" for="auto-base-url-input">Base URL</label>
            <input type="text" id="auto-base-url-input" class="input-box w-full p-2.5 rounded-lg border text-sm" placeholder={autoDiscoverForm.provider === 'custom' ? 'https://api.together.xyz/v1' : ''} bind:value={autoDiscoverForm.base_url} />
          </div>
          <div class="form-group flex flex-col gap-1">
            <label class="text-[10px] font-bold uppercase tracking-wider" for="auto-weight-input">Weight</label>
            <input type="number" id="auto-weight-input" class="input-box w-full p-2.5 rounded-lg border text-sm" min="1" bind:value={autoDiscoverForm.weight} />
          </div>
        </div>
        <div class="flex justify-end gap-2 text-xs">
          <button class="px-4 py-2 rounded-lg border" onclick={() => showAddProviderModal = false}>Cancel</button>
          <button class="px-4 py-2 rounded-lg text-white bg-[#f97316] font-semibold flex items-center gap-1.5 min-w-[120px] justify-center" onclick={autoDiscoverProvider} disabled={autoDiscoverLoading}>
            {#if autoDiscoverLoading}
              <span class="animate-spin">🔄</span> Discovering...
            {:else}
              Discover & Register
            {/if}
          </button>
        </div>
      {/if}
    </div>
  </div>
{/if}

<!-- ═══════════════════════════════════════════════════════════════════════ -->
<!-- EDIT CREDENTIAL MODAL                                                   -->
<!-- ═══════════════════════════════════════════════════════════════════════ -->
{#if showEditModal}
  <div class="modal-backdrop fixed inset-0 flex items-center justify-center p-4 z-50 bg-black-trans backdrop-blur-sm">
    <div class="modal-content w-full max-w-sm rounded-xl border p-6 shadow-2xl relative">
      <div class="flex items-center justify-between mb-4">
        <h3 class="font-bold text-lg">Edit Credential #{editForm.id}</h3>
        <button class="icon-button" onclick={() => showEditModal = false}><X size={16} /></button>
      </div>

      <div class="flex flex-col gap-3 mb-5">
        <div class="form-group flex flex-col gap-1">
          <label class="text-[10px] font-bold uppercase tracking-wider" for="edit-provider-select">Provider</label>
          <select id="edit-provider-select" class="input-box w-full p-2.5 rounded-lg border text-sm" bind:value={editForm.provider}>
            <option value="openai">OpenAI</option>
            <option value="anthropic">Anthropic</option>
            <option value="nvidia">NVIDIA</option>
            <option value="ollama">Ollama</option>
            <option value="google">Google</option>
            <option value="custom">Custom</option>
          </select>
        </div>
        <div class="form-group flex flex-col gap-1">
          <label class="text-[10px] font-bold uppercase tracking-wider" for="edit-api-key-input">New API Key <span class="opacity-50">(leave blank to keep current)</span></label>
          <input type="password" id="edit-api-key-input" class="input-box w-full p-2.5 rounded-lg border text-sm" placeholder="Leave blank to keep current key" bind:value={editForm.api_key} />
        </div>
        <div class="form-group flex flex-col gap-1">
          <label class="text-[10px] font-bold uppercase tracking-wider" for="edit-base-url-input">Base URL</label>
          <input type="text" id="edit-base-url-input" class="input-box w-full p-2.5 rounded-lg border text-sm" bind:value={editForm.base_url} />
        </div>
        <div class="flex gap-3">
          <div class="form-group flex flex-col gap-1 flex-grow">
            <label class="text-[10px] font-bold uppercase tracking-wider" for="edit-weight-input">Weight</label>
            <input type="number" id="edit-weight-input" class="input-box w-full p-2.5 rounded-lg border text-sm" min="1" bind:value={editForm.weight} />
          </div>
          <div class="form-group flex flex-col gap-1">
            <label class="text-[10px] font-bold uppercase tracking-wider" for="edit-healthy-toggle">Healthy</label>
            <label class="toggle-switch mt-1" id="edit-healthy-toggle">
              <input type="checkbox" bind:checked={editForm.is_healthy} />
              <span class="toggle-slider"></span>
            </label>
          </div>
        </div>
      </div>

      <div class="flex justify-end gap-2 text-xs">
        <button class="px-4 py-2 rounded-lg border" onclick={() => showEditModal = false}>Cancel</button>
        <button class="px-4 py-2 rounded-lg text-white bg-[#f97316] font-semibold flex items-center gap-1.5 min-w-[120px] justify-center" onclick={updateCredential} disabled={editLoading}>
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

<!-- ═══════════════════════════════════════════════════════════════════════ -->
<!-- DELETE CONFIRMATION DIALOG                                              -->
<!-- ═══════════════════════════════════════════════════════════════════════ -->
{#if showDeleteConfirm}
  <div class="modal-backdrop fixed inset-0 flex items-center justify-center p-4 z-50 bg-black-trans backdrop-blur-sm">
    <div class="modal-content w-full max-w-xs rounded-xl border p-6 shadow-2xl relative text-center">
      <AlertTriangle size={32} class="text-red-500 mx-auto mb-3" />
      <h3 class="font-bold text-base mb-2">Delete Credential?</h3>
      <p class="text-xs mb-5 opacity-75">This action is permanent and cannot be undone. The provider key will be removed from routing.</p>
      <div class="flex justify-center gap-2 text-xs">
        <button class="px-4 py-2 rounded-lg border" onclick={() => { showDeleteConfirm = false; deleteTargetId = null; }}>Cancel</button>
        <button class="px-4 py-2 rounded-lg text-white bg-red-500 font-semibold flex items-center gap-1.5 min-w-[100px] justify-center" style="background-color: #ef4444;" onclick={deleteCredentialById} disabled={deleteLoading}>
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
