<script>
  import { onMount } from 'svelte';
  import { KeyRound, RefreshCw, Plus, Shield, AlertTriangle, Server, Pencil, Trash2, X } from '@lucide/svelte';
  import { appState } from '$lib/state.svelte.js';
  import Button from '$lib/components/Button.svelte';
  import Input from '$lib/components/Input.svelte';
  import Card from '$lib/components/Card.svelte';
  import Modal from '$lib/components/Modal.svelte';

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
  let autoDiscoverForm = $state({ provider: 'openrouter', api_key: '', base_url: 'https://openrouter.ai/api/v1', weight: 1, label: '', account_id: '', api_token: '' });
  let autoDiscoverLoading = $state(false);

  // Edit modal
  let showEditModal = $state(false);
  let editForm = $state({ id: 0, provider: '', api_key: '', base_url: '', weight: 1, is_healthy: true });
  let editLoading = $state(false);

  // Delete confirmation
  let showDeleteConfirm = $state(false);
  let deleteTargetId = $state(null);
  let deleteLoading = $state(false);

  // Bulk selection and deletion
  let selectedIds = $state([]);
  let showBulkDeleteConfirm = $state(false);
  let bulkDeleteLoading = $state(false);

  // Refresh all providers
  let refreshLoading = $state(false);

  // ─── Load state on adminKey change ─────────────────────────────────────────
  $effect(() => {
    if (appState.adminKey.trim()) {
      loadCredentials();
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

  async function loadCredentials() {
    providerLoading = true;
    providerError = '';
    appState.apiLoading = true;
    try {
      const res = await fetch('/api/v1/admin/credentials', { headers: adminHeaders() });
      if (res.ok) {
        providerCredentials = await res.json();
        selectedIds = selectedIds.filter(id => providerCredentials.some(c => c.id === id));
      } else {
        const err = await res.json();
        providerError = err.error || `Error ${res.status}`;
      }
    } catch (e) {
      providerError = `Network error: ${e.message}`;
    } finally {
      providerLoading = false;
      appState.apiLoading = false;
    }
  }

  async function loadPools() {
    appState.apiLoading = true;
    try {
      const res = await fetch('/api/v1/admin/pools', { headers: adminHeaders() });
      if (res.ok) {
        providerPools = await res.json();
      }
    } catch (e) {
      console.error('Failed to load pools', e);
    } finally {
      appState.apiLoading = false;
    }
  }

  function openAddProviderModal() {
    addProviderForm = { pool_id: '', provider: 'openai', api_key: '', base_url: 'https://api.openai.com', weight: 1 };
    autoDiscoverForm = { provider: 'nvidia', api_key: '', base_url: 'https://integrate.api.nvidia.com/v1', weight: 1, label: '', account_id: '', api_token: '' };
    addProviderTab = 'standard';
    showAddProviderModal = true;
    loadPools();
  }

  async function createCredential() {
    addProviderLoading = true;
    appState.apiLoading = true;
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
        appState.addToast('success', 'Credential created successfully');
        showAddProviderModal = false;
        loadCredentials();
      } else {
        const err = await res.json();
        appState.addToast('error', err.details || err.error || 'Failed to create credential');
      }
    } catch (e) {
      appState.addToast('error', `Network error: ${e.message}`);
    } finally {
      addProviderLoading = false;
      appState.apiLoading = false;
    }
  }

  async function autoDiscoverProvider() {
    autoDiscoverLoading = true;
    appState.apiLoading = true;
    let endpoint;
    if (autoDiscoverForm.provider === 'nvidia') {
      endpoint = '/api/v1/admin/providers/nvidia';
    } else if (autoDiscoverForm.provider === 'ollama') {
      endpoint = '/api/v1/admin/providers/ollama';
    } else if (autoDiscoverForm.provider === 'openrouter') {
      endpoint = '/api/v1/admin/providers/openrouter';
    } else if (autoDiscoverForm.provider === '1minai') {
      endpoint = '/api/v1/admin/providers/1minai';
    } else if (autoDiscoverForm.provider === 'cloudflare') {
      endpoint = '/api/v1/admin/providers/cloudflare';
    } else if (autoDiscoverForm.provider === 'sarvam') {
      endpoint = '/api/v1/admin/providers/sarvam';
    } else if (autoDiscoverForm.provider === 'puter') {
      endpoint = '/api/v1/admin/providers/puter';
    } else {
      endpoint = '/api/v1/admin/providers/custom';
    }
    try {
      let payload;
      if (autoDiscoverForm.provider === 'cloudflare') {
        // Cloudflare uses a dedicated DTO with account_id + api_token
        payload = {
          account_id: autoDiscoverForm.account_id,
          api_token: autoDiscoverForm.api_token,
          weight: autoDiscoverForm.weight || 1
        };
      } else {
        payload = {
          provider: autoDiscoverForm.provider,
          api_key: autoDiscoverForm.api_key,
          base_url: autoDiscoverForm.base_url,
          weight: autoDiscoverForm.weight || 1
        };
        if (autoDiscoverForm.provider === 'custom' && autoDiscoverForm.label) {
          payload.label = autoDiscoverForm.label;
        }
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
          : autoDiscoverForm.provider === '1minai' ? '1min.ai'
          : autoDiscoverForm.provider === 'cloudflare' ? 'Cloudflare Workers AI'
          : autoDiscoverForm.provider === 'sarvam' ? 'Sarvam AI'
          : autoDiscoverForm.provider === 'puter' ? 'Puter.com'
          : autoDiscoverForm.provider.toUpperCase();
        appState.addToast('success', `Successfully synchronized ${data.models_count || 0} ${displayName} models`);
        showAddProviderModal = false;
        loadCredentials();
        if (appState.apiKey) appState.loadModels();
      } else {
        const err = await res.json();
        appState.addToast('error', err.details || err.error || 'Auto-discovery failed');
      }
    } catch (e) {
      appState.addToast('error', `Network error: ${e.message}`);
    } finally {
      autoDiscoverLoading = false;
      appState.apiLoading = false;
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
    appState.apiLoading = true;
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
        appState.addToast('success', 'Credential updated successfully');
        showEditModal = false;
        loadCredentials();
      } else {
        const err = await res.json();
        appState.addToast('error', err.details || err.error || 'Failed to update credential');
      }
    } catch (e) {
      appState.addToast('error', `Network error: ${e.message}`);
    } finally {
      editLoading = false;
      appState.apiLoading = false;
    }
  }

  function confirmDelete(id) {
    deleteTargetId = id;
    showDeleteConfirm = true;
  }

  async function deleteCredentialById() {
    deleteLoading = true;
    appState.apiLoading = true;
    try {
      const res = await fetch(`/api/v1/admin/credentials/${deleteTargetId}`, {
        method: 'DELETE',
        headers: adminHeaders()
      });
      if (res.ok) {
        appState.addToast('success', 'Credential deleted successfully');
        showDeleteConfirm = false;
        deleteTargetId = null;
        loadCredentials();
      } else {
        const err = await res.json();
        appState.addToast('error', err.details || err.error || 'Failed to delete credential');
      }
    } catch (e) {
      appState.addToast('error', `Network error: ${e.message}`);
    } finally {
      deleteLoading = false;
      appState.apiLoading = false;
    }
  }

  function confirmBulkDelete() {
    showBulkDeleteConfirm = true;
  }

  async function deleteCredentialsBulk() {
    bulkDeleteLoading = true;
    appState.apiLoading = true;
    try {
      const res = await fetch('/api/v1/admin/credentials/bulk-delete', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...adminHeaders()
        },
        body: JSON.stringify({ ids: selectedIds })
      });
      if (res.ok) {
        appState.addToast('success', `${selectedIds.length} credentials deleted successfully`);
        showBulkDeleteConfirm = false;
        selectedIds = [];
        loadCredentials();
        loadPools();
        if (appState.apiKey) appState.loadModels();
      } else {
        const err = await res.json();
        appState.addToast('error', err.details || err.error || 'Failed to delete credentials');
      }
    } catch (e) {
      appState.addToast('error', `Network error: ${e.message}`);
    } finally {
      bulkDeleteLoading = false;
      appState.apiLoading = false;
    }
  }

  function providerBadgeClass(provider) {
    switch ((provider || '').toLowerCase()) {
      case 'openai': return 'badge-openai';
      case 'nvidia': return 'badge-nvidia';
      case 'ollama': return 'badge-ollama';
      case 'anthropic': return 'badge-anthropic';
      case 'openrouter': return 'badge-openrouter';
      case '1minai': return 'badge-1minai';
      case 'cloudflare': return 'badge-cloudflare';
      case 'sarvam': return 'badge-sarvam';
      case 'puter': return 'badge-puter';
      case 'custom': return 'badge-custom';
      default: return 'badge-default';
    }
  }

  function connectAdminKey() {
    const key = appState.adminKey.trim();
    if (!key) return;
    localStorage.setItem('cag_admin_key', key);
    loadCredentials();
    loadPools();
  }

  async function refreshAllProviders() {
    refreshLoading = true;
    appState.apiLoading = true;
    try {
      const res = await fetch('/api/v1/admin/providers/refresh', {
        method: 'POST',
        headers: adminHeaders()
      });
      if (res.ok) {
        const data = await res.json();
        appState.addToast('success', data.message || `Re-synced ${data.models_count ?? 0} model pools`);
        await loadCredentials();
        if (appState.apiKey) appState.loadModels();
      } else {
        const err = await res.json();
        appState.addToast('error', err.details || err.error || 'Refresh failed');
      }
    } catch (e) {
      appState.addToast('error', `Network error: ${e.message}`);
    } finally {
      refreshLoading = false;
      appState.apiLoading = false;
    }
  }

  onMount(() => {
    if (appState.adminKey.trim()) {
      loadCredentials();
      loadPools();
    }
  });
</script>

<header class="header flex items-center justify-between px-6 py-4 border-b shrink-0">
  <div class="flex items-center gap-3">
    <KeyRound size={20} class="text-[#f97316]" />
    <span class="font-bold text-base">Provider Credentials</span>
    {#if appState.adminKey.trim()}
      <span class="text-xs font-bold text-secondary bg-gray-500/10 border border-gray-500/20 px-2.5 py-0.5 rounded-full uppercase">{providerCredentials.length} registered</span>
    {/if}
  </div>
  
  {#if appState.adminKey.trim()}
    <div class="flex items-center gap-2 animate-fade-in">
      {#if selectedIds.length > 0}
        <Button variant="danger" size="sm" onclick={confirmBulkDelete} title="Delete selected credentials">
          <Trash2 size={14} />
          Delete Selected ({selectedIds.length})
        </Button>
      {/if}
      <Button variant="secondary" size="sm" onclick={refreshAllProviders} disabled={refreshLoading} title="Re-run discovery for all stored provider keys and provision any missing alias pools">
        <RefreshCw size={14} class={refreshLoading ? 'animate-spin' : ''} />
        {refreshLoading ? 'Refreshing...' : 'Refresh'}
      </Button>
      <Button variant="primary" size="sm" onclick={openAddProviderModal}>
        <Plus size={14} />
        Add Provider
      </Button>
    </div>
  {/if}
</header>

{#if !appState.adminKey.trim()}
  <!-- Admin key prompt -->
  <div class="logs-key-prompt flex flex-col justify-center items-center flex-grow p-6">
    <Card variant="filled" padding="lg" class="logs-key-card flex flex-col items-center text-center">
      <Shield size={40} class="text-[#f97316] mb-4 animate-pulse" />
      <h2 class="font-bold text-lg mb-2 text-primary">Admin Key Required</h2>
      <p class="text-sm mb-6 text-secondary max-w-sm">Enter your Admin API Key to manage provider credentials, pools, and API keys.</p>
      
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
      
      {#if providerError}
        <p class="text-red-500 text-sm font-semibold mt-4">{providerError}</p>
      {/if}
    </Card>
  </div>
{:else}
  <!-- Providers data grid -->
  <div class="providers-grid-wrap flex-grow overflow-auto p-6">
    {#if providerLoading}
      <div class="providers-loading flex flex-col items-center justify-center h-64">
        <div class="animate-spin text-[#f97316] text-xl">⟳</div>
        <p class="text-sm mt-2 text-secondary">Loading credentials...</p>
      </div>
    {:else if providerError}
      <div class="providers-loading flex flex-col items-center justify-center h-64">
        <AlertTriangle size={40} class="text-red-500 mb-2" />
        <p class="text-red-500 text-sm font-semibold">{providerError}</p>
        <Button variant="primary" class="mt-4" onclick={loadCredentials}>Retry</Button>
      </div>
    {:else if providerCredentials.length === 0}
      <div class="providers-loading flex flex-col items-center justify-center h-64">
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
              <th style="width: 40px; text-align: center;">
                <input
                  type="checkbox"
                  class="log-checkbox w-4 h-4 rounded border-gray-300 accent-orange-500 cursor-pointer"
                  checked={selectedIds.length === providerCredentials.length && providerCredentials.length > 0}
                  onchange={(e) => {
                    if (e.target.checked) {
                      selectedIds = providerCredentials.map(c => c.id);
                    } else {
                      selectedIds = [];
                    }
                  }}
                />
              </th>
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
                <td style="text-align: center; width: 40px;">
                  <input
                    type="checkbox"
                    class="log-checkbox w-4 h-4 rounded border-gray-300 accent-orange-500 cursor-pointer"
                    value={cred.id}
                    bind:group={selectedIds}
                  />
                </td>
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
        <option value="openrouter">OpenRouter</option>
        <option value="1minai">1min.ai</option>
        <option value="cloudflare">Cloudflare Workers AI</option>
        <option value="sarvam">Sarvam AI</option>
        <option value="puter">Puter.com</option>
        <option value="google">Google</option>
        <option value="custom">Custom</option>
      </Input>

      <Input type="password" label="API Key" placeholder="sk-..." bind:value={addProviderForm.api_key} />
      
      <Input type="text" label="Base URL" placeholder="https://api.openai.com" bind:value={addProviderForm.base_url} />
      
      <Input type="number" label="Weight" min="1" bind:value={addProviderForm.weight} />
    </div>
  {:else}
    <div class="flex flex-col gap-4">
      <p class="text-sm text-secondary leading-relaxed">Auto-discover models from NVIDIA NIM, Ollama Cloud, OpenRouter (free models only), or any OpenAI-compatible provider. Pools are created automatically.</p>
      
      <Input type="select" label="Provider Type" bind:value={autoDiscoverForm.provider} onchange={() => {
        if (autoDiscoverForm.provider === 'nvidia') {
          autoDiscoverForm.base_url = 'https://integrate.api.nvidia.com/v1';
        } else if (autoDiscoverForm.provider === 'ollama') {
          autoDiscoverForm.base_url = 'https://ollama.com';
        } else if (autoDiscoverForm.provider === 'openrouter') {
          autoDiscoverForm.base_url = 'https://openrouter.ai/api/v1';
        } else if (autoDiscoverForm.provider === '1minai') {
          autoDiscoverForm.base_url = 'https://api.1min.ai';
        } else if (autoDiscoverForm.provider === 'cloudflare') {
          autoDiscoverForm.base_url = '';
        } else if (autoDiscoverForm.provider === 'sarvam') {
          autoDiscoverForm.base_url = 'https://api.sarvam.ai';
        } else if (autoDiscoverForm.provider === 'puter') {
          autoDiscoverForm.base_url = 'https://api.puter.com/puterai/openai/v1';
        } else {
          autoDiscoverForm.base_url = '';
        }
        autoDiscoverForm.label = '';
        autoDiscoverForm.account_id = '';
        autoDiscoverForm.api_token = '';
        autoDiscoverForm.api_key = '';
      }}>
        <option value="openrouter">OpenRouter (Free Models)</option>
        <option value="nvidia">NVIDIA NIM</option>
        <option value="ollama">Ollama Cloud</option>
        <option value="1minai">1min.ai (Multi-Modal)</option>
        <option value="cloudflare">Cloudflare Workers AI</option>
        <option value="sarvam">Sarvam AI</option>
        <option value="puter">Puter.com</option>
        <option value="custom">OpenAI-Compatible (Custom)</option>
      </Input>

      {#if autoDiscoverForm.provider === 'openrouter'}
        <div class="rounded-lg border border-indigo-500/20 bg-indigo-500/5 px-4 py-3 text-xs text-indigo-400 leading-relaxed">
          🆓 Only <strong>free-tier models</strong> will be registered (those with a <code>:free</code> identifier). No paid models will be added. Get your API key at <a href="https://openrouter.ai/settings/keys" target="_blank" rel="noopener noreferrer" class="underline">openrouter.ai/settings/keys</a>.
        </div>
      {/if}

      {#if autoDiscoverForm.provider === '1minai'}
        <div class="rounded-lg border border-emerald-500/20 bg-emerald-500/5 px-4 py-3 text-xs text-emerald-400 leading-relaxed">
          🤖 <strong>1min.ai</strong> supports all modalities: Writing, Image, Audio, Video, and Code. All models are auto-discovered from a static manifest — just enter your API key. Get your key at <a href="https://app.1min.ai" target="_blank" rel="noopener noreferrer" class="underline">app.1min.ai</a>.
        </div>
      {/if}

      {#if autoDiscoverForm.provider === 'cloudflare'}
        <div class="rounded-lg border border-orange-500/20 bg-orange-500/5 px-4 py-3 text-xs text-orange-400 leading-relaxed">
          ☁️ <strong>Cloudflare Workers AI</strong> requires your <strong>Account ID</strong> and an <strong>API Token</strong> with Workers AI permissions. All available models (Text, Image, Audio, Embeddings) are discovered automatically. Find your Account ID and create an API token at <a href="https://dash.cloudflare.com" target="_blank" rel="noopener noreferrer" class="underline">dash.cloudflare.com</a>.
        </div>
      {/if}

      {#if autoDiscoverForm.provider === 'sarvam'}
        <div class="rounded-lg border border-purple-500/20 bg-purple-500/5 px-4 py-3 text-xs text-purple-400 leading-relaxed">
          🇮🇳 <strong>Sarvam AI</strong> is a premium AI provider in India. It offers a static set of chat models (<code>sarvam-30b</code>, <code>sarvam-105b</code>) and supports reasoning. Discovery is instant and uses a hardcoded manifest. Get your API subscription key at <a href="https://dashboard.sarvam.ai" target="_blank" rel="noopener noreferrer" class="underline">dashboard.sarvam.ai</a>.
        </div>
      {/if}

      {#if autoDiscoverForm.provider === 'puter'}
        <div class="rounded-lg border border-blue-500/20 bg-blue-500/5 px-4 py-3 text-xs text-blue-400 leading-relaxed">
          🚀 <strong>Puter.com</strong> is a developer-friendly cloud with free AI access. Enter your API Token (Puter Auth Token) to auto-discover all models. Get your API Token at <a href="https://puter.com/dashboard" target="_blank" rel="noopener noreferrer" class="underline">puter.com/dashboard</a>.
        </div>
      {/if}

      {#if autoDiscoverForm.provider === 'custom'}
        <Input type="text" label="Label (optional)" placeholder="e.g. Together AI, DeepInfra" bind:value={autoDiscoverForm.label} />
      {/if}

      {#if autoDiscoverForm.provider === 'cloudflare'}
        <!-- Cloudflare needs Account ID + API Token instead of the generic api_key/base_url -->
        <Input
          type="text"
          label="Account ID"
          placeholder="a1b2c3d4e5f6..."
          bind:value={autoDiscoverForm.account_id}
        />
        <Input
          type="password"
          label="API Token"
          placeholder="Workers AI API Token..."
          bind:value={autoDiscoverForm.api_token}
        />
      {:else}
        <Input 
          type="password" 
          label="API Key" 
          placeholder={
            autoDiscoverForm.provider === 'nvidia' ? 'nvapi-...' :
            autoDiscoverForm.provider === 'ollama' ? 'Ollama Cloud API key...' :
            autoDiscoverForm.provider === 'openrouter' ? 'sk-or-v1-...' :
            autoDiscoverForm.provider === '1minai' ? '1min.ai API key...' :
            autoDiscoverForm.provider === 'sarvam' ? 'Sarvam API key (api-subscription-key)...' :
            autoDiscoverForm.provider === 'puter' ? 'Puter Auth Token...' :
            'Bearer API key...'
          } 
          bind:value={autoDiscoverForm.api_key} 
        />
        
        {#if autoDiscoverForm.provider !== 'openrouter' && autoDiscoverForm.provider !== '1minai' && autoDiscoverForm.provider !== 'sarvam' && autoDiscoverForm.provider !== 'puter'}
          <Input type="text" label="Base URL" placeholder={autoDiscoverForm.provider === 'custom' ? 'https://api.together.xyz/v1' : ''} bind:value={autoDiscoverForm.base_url} />
        {/if}
      {/if}
      
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
      <option value="openrouter">OpenRouter</option>
      <option value="1minai">1min.ai</option>
      <option value="cloudflare">Cloudflare Workers AI</option>
      <option value="sarvam">Sarvam AI</option>
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

<!-- ─── BULK DELETE CONFIRMATION DIALOG ─────────────────────────────────────────── -->
<Modal bind:show={showBulkDeleteConfirm} title="Delete Credentials?">
  <div class="flex flex-col items-center gap-4 text-center">
    <AlertTriangle size={48} class="text-red-500 mb-2" />
    <p class="text-sm text-secondary">
      Are you sure you want to permanently delete the {selectedIds.length} selected credentials?
      They will be removed from gateway routing immediately.
    </p>
    <p class="text-xs text-red-500 font-bold">This action is permanent and cannot be undone.</p>
  </div>

  {#snippet footer()}
    <div class="flex justify-center gap-3 w-full">
      <Button variant="outline" onclick={() => { showBulkDeleteConfirm = false; }}>Cancel</Button>
      <Button variant="danger" onclick={deleteCredentialsBulk} disabled={bulkDeleteLoading}>
        {#if bulkDeleteLoading}
          <span class="animate-spin">⟳</span>
        {:else}
          Delete ({selectedIds.length})
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

  /* OpenRouter brand badge — indigo tone matching openrouter.ai visual identity */
  :global(.badge-openrouter) {
    background: rgba(99, 102, 241, 0.12);
    color: #818cf8;
    border: 1px solid rgba(99, 102, 241, 0.25);
  }

  /* 1min.ai badge — emerald tone */
  :global(.badge-1minai) {
    background: rgba(16, 185, 129, 0.12);
    color: #34d399;
    border: 1px solid rgba(16, 185, 129, 0.25);
  }

  /* Cloudflare brand badge — orange/amber tone matching Cloudflare visual identity */
  :global(.badge-cloudflare) {
    background: rgba(249, 115, 22, 0.12);
    color: #fb923c;
    border: 1px solid rgba(249, 115, 22, 0.25);
  }

  /* Sarvam AI brand badge — purple/violet tone */
  :global(.badge-sarvam) {
    background: rgba(167, 139, 250, 0.12);
    color: #a78bfa;
    border: 1px solid rgba(167, 139, 250, 0.25);
  }

  /* Puter brand badge — blue/indigo tone */
  :global(.badge-puter) {
    background: rgba(59, 130, 246, 0.12);
    color: #60a5fa;
    border: 1px solid rgba(59, 130, 246, 0.25);
  }
</style>
