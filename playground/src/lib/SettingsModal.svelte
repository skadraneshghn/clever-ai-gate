<script>
  let {
    showSettingsModal = $bindable(false),
    apiKey = $bindable(''),
    activeSettingsTab = $bindable('tenant'),
    visibleApiKey = $bindable(false),
    connectError = $bindable(''),
    isConnecting = $bindable(false),
    handleSaveKey,
    adminApiKey = $bindable(''),
    nvidiaApiKey = $bindable(''),
    nvidiaBaseUrl = $bindable(''),
    isAdminConnecting = $bindable(false),
    adminConnectSuccess = $bindable(''),
    adminConnectError = $bindable(''),
    handleRegisterNvidia,
    isInitializing
  } = $props();
</script>

{#if !isInitializing && (showSettingsModal || !apiKey)}
  <div class="modal-backdrop fixed inset-0 flex items-center justify-center p-4 z-50 bg-black-trans backdrop-blur-sm">
    <div class="modal-content w-full max-w-sm rounded-xl border p-6 shadow-2xl relative">
      <!-- Tabs Selector -->
      <div class="flex border-b text-[10px] mb-4">
        <button 
          class="tab-btn px-4 py-2 flex-grow font-semibold text-center {activeSettingsTab === 'tenant' ? 'active' : ''}" 
          onclick={() => { activeSettingsTab = 'tenant'; }}
        >
          Tenant Key
        </button>
        <button 
          class="tab-btn px-4 py-2 flex-grow font-semibold text-center {activeSettingsTab === 'admin' ? 'active' : ''}" 
          onclick={() => { activeSettingsTab = 'admin'; }}
        >
          Admin: NVIDIA Key
        </button>
      </div>

      {#if activeSettingsTab === 'tenant'}
        <h3 class="font-bold text-lg mb-2">Connect Gateway</h3>
        <p class="text-xs mb-4">Please input your Clever AI Gate Tenant API key (e.g. <code>cag_xxxx</code>) to load your chat sessions and start calling active routing models.</p>
        
        <div class="form-group flex flex-col gap-2 mb-5">
          <label class="text-xs font-bold uppercase tracking-wider" for="gw-api-key">Tenant API Key</label>
          <div class="relative flex items-center">
            <input 
              type={visibleApiKey ? 'text' : 'password'} 
              id="gw-api-key" 
              class="input-box w-full p-2.5 rounded-lg border text-sm" 
              placeholder="cag_xxxx..." 
              bind:value={apiKey} 
              onkeydown={(e) => { if(e.key === 'Enter') { e.preventDefault(); handleSaveKey(); } }}
            />
            <button class="absolute right-3" onclick={() => visibleApiKey = !visibleApiKey}>
              {#if visibleApiKey}🔒{:else}👁️{/if}
            </button>
          </div>
        </div>

        {#if connectError}
          <div class="text-red-500 text-xs mb-4 font-medium">{connectError}</div>
        {/if}

        <div class="flex justify-end gap-2 text-xs">
          {#if apiKey.trim() && localStorage.getItem('cag_playground_api_key') && !isConnecting}
            <button class="px-4 py-2 rounded-lg border" onclick={() => { showSettingsModal = false; connectError = ''; }}>Cancel</button>
          {/if}
          <button 
            class="px-4 py-2 rounded-lg text-white bg-[#f97316] font-semibold flex items-center justify-center gap-1.5 min-w-[120px]" 
            onclick={handleSaveKey} 
            disabled={!apiKey.trim() || isConnecting}
          >
            {#if isConnecting}
              <span class="animate-spin">🔄</span> Connecting...
            {:else}
              Save & Connect
            {/if}
          </button>
        </div>
      {:else}
        <h3 class="font-bold text-lg mb-2">Register NVIDIA NIM</h3>
        <p class="text-xs mb-4">Register NVIDIA key. This auto-discovers all active model configurations and synchronizes them to our AI Gateway.</p>
        
        <div class="flex flex-col gap-3 mb-5">
          <div class="form-group flex flex-col gap-1">
            <label class="text-[10px] font-bold uppercase tracking-wider" for="admin-key">Admin API Key</label>
            <input 
              type="password" 
              id="admin-key" 
              class="input-box w-full p-2.5 rounded-lg border text-sm" 
              placeholder="Enter Admin API Key..." 
              bind:value={adminApiKey} 
            />
          </div>

          <div class="form-group flex flex-col gap-1">
            <label class="text-[10px] font-bold uppercase tracking-wider" for="nv-key">NVIDIA API Key</label>
            <input 
              type="password" 
              id="nv-key" 
              class="input-box w-full p-2.5 rounded-lg border text-sm" 
              placeholder="nvapi-..." 
              bind:value={nvidiaApiKey} 
            />
          </div>

          <div class="form-group flex flex-col gap-1">
            <label class="text-[10px] font-bold uppercase tracking-wider" for="nv-url">Base URL</label>
            <input 
              type="text" 
              id="nv-url" 
              class="input-box w-full p-2.5 rounded-lg border text-sm" 
              placeholder="https://integrate.api.nvidia.com/v1" 
              bind:value={nvidiaBaseUrl} 
            />
          </div>
        </div>

        {#if adminConnectError}
          <div class="text-red-500 text-xs mb-4 font-medium">{adminConnectError}</div>
        {/if}
        {#if adminConnectSuccess}
          <div class="text-green-500 text-xs mb-4 font-medium">{adminConnectSuccess}</div>
        {/if}

        <div class="flex justify-end gap-2 text-xs">
          {#if apiKey.trim() && localStorage.getItem('cag_playground_api_key')}
            <button class="px-4 py-2 rounded-lg border" onclick={() => { showSettingsModal = false; adminConnectError = ''; adminConnectSuccess = ''; }}>Close</button>
          {/if}
          <button 
            class="px-4 py-2 rounded-lg text-white bg-[#f97316] font-semibold flex items-center justify-center gap-1.5 min-w-[120px]" 
            onclick={handleRegisterNvidia} 
            disabled={isAdminConnecting}
          >
            {#if isAdminConnecting}
              <span class="animate-spin">🔄</span> Registering...
            {:else}
              Register Provider
            {/if}
          </button>
        </div>
      {/if}
    </div>
  </div>
{/if}
