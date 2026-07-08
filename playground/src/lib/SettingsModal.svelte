<script>
  import Modal from './components/Modal.svelte';
  import Input from './components/Input.svelte';
  import Button from './components/Button.svelte';

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
  <Modal bind:show={showSettingsModal} maxWidth="md">
    {#snippet header()}
      <!-- Tabs Selector -->
      <div class="flex border-b text-xs w-full">
        <button 
          class="tab-btn px-6 py-3 flex-grow font-semibold text-center {activeSettingsTab === 'tenant' ? 'active' : ''}" 
          onclick={() => { activeSettingsTab = 'tenant'; }}
        >
          Tenant Key
        </button>
        <button 
          class="tab-btn px-6 py-3 flex-grow font-semibold text-center {activeSettingsTab === 'admin' ? 'active' : ''}" 
          onclick={() => { activeSettingsTab = 'admin'; }}
        >
          Admin: NVIDIA Key
        </button>
      </div>
    {/snippet}

    {#if activeSettingsTab === 'tenant'}
      <div class="flex flex-col gap-3">
        <h3 class="font-bold text-lg text-primary">Connect Gateway</h3>
        <p class="text-sm text-secondary leading-relaxed">
          Please input your Clever AI Gate Tenant API key (e.g. <code>cag_xxxx</code>) to load your chat sessions and start calling active routing models.
        </p>
        
        <Input 
          type={visibleApiKey ? 'text' : 'password'} 
          label="Tenant API Key"
          placeholder="cag_xxxx..." 
          bind:value={apiKey} 
          onkeydown={(e) => { if(e.key === 'Enter') { e.preventDefault(); handleSaveKey(); } }}
        >
          <button 
            type="button"
            class="absolute right-3 top-[10px] text-base p-1 hover:bg-black/5 dark:hover:bg-white/5 rounded-md" 
            onclick={() => visibleApiKey = !visibleApiKey}
          >
            {#if visibleApiKey}🔒{:else}👁️{/if}
          </button>
        </Input>

        {#if connectError}
          <div class="text-red-500 text-sm font-semibold mt-1">{connectError}</div>
        {/if}
      </div>
    {:else}
      <div class="flex flex-col gap-4">
        <h3 class="font-bold text-lg text-primary">Register NVIDIA NIM</h3>
        <p class="text-sm text-secondary leading-relaxed">
          Register your NVIDIA API key to auto-discover all active model configurations and synchronize them to Clever AI Gate.
        </p>
        
        <div class="flex flex-col gap-3">
          <Input 
            type="password" 
            label="Admin API Key" 
            placeholder="Enter Admin API Key..." 
            bind:value={adminApiKey} 
          />

          <Input 
            type="password" 
            label="NVIDIA API Key" 
            placeholder="nvapi-..." 
            bind:value={nvidiaApiKey} 
          />

          <Input 
            type="text" 
            label="Base URL" 
            placeholder="https://integrate.api.nvidia.com/v1" 
            bind:value={nvidiaBaseUrl} 
          />
        </div>

        {#if adminConnectError}
          <div class="text-red-500 text-sm font-semibold mt-1">{adminConnectError}</div>
        {/if}
        {#if adminConnectSuccess}
          <div class="text-green-500 text-sm font-semibold mt-1">{adminConnectSuccess}</div>
        {/if}
      </div>
    {/if}

    {#snippet footer()}
      <div class="flex justify-end gap-3 w-full">
        {#if activeSettingsTab === 'tenant'}
          {#if apiKey.trim() && localStorage.getItem('cag_playground_api_key') && !isConnecting}
            <Button variant="outline" onclick={() => { showSettingsModal = false; connectError = ''; }}>Cancel</Button>
          {/if}
          <Button 
            variant="primary" 
            onclick={handleSaveKey} 
            disabled={!apiKey.trim() || isConnecting}
          >
            {#if isConnecting}
              <span class="animate-spin">⟳</span> Connecting...
            {:else}
              Save & Connect
            {/if}
          </Button>
        {:else}
          {#if apiKey.trim() && localStorage.getItem('cag_playground_api_key')}
            <Button variant="outline" onclick={() => { showSettingsModal = false; adminConnectError = ''; adminConnectSuccess = ''; }}>Close</Button>
          {/if}
          <Button 
            variant="primary" 
            onclick={handleRegisterNvidia} 
            disabled={isAdminConnecting}
          >
            {#if isAdminConnecting}
              <span class="animate-spin">⟳</span> Registering...
            {:else}
              Register Provider
            {/if}
          </Button>
        {/if}
      </div>
    {/snippet}
  </Modal>
{/if}

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
</style>
