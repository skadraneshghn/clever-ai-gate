<script>
  import { page } from '$app/stores';
  import { 
    Settings, Plus, Trash2, Sparkles, Sun, Moon, KeyRound, Terminal, Users, Cpu
  } from '@lucide/svelte';
  import { appState } from '$lib/state.svelte.js';
  import Button from '$lib/components/Button.svelte';
  import Card from '$lib/components/Card.svelte';
  import LoadingBar from '$lib/components/LoadingBar.svelte';
  import SettingsModal from '$lib/SettingsModal.svelte';
  import ToastsComponent from '$lib/Toasts.svelte';
  import '../app.css';

  let { children } = $props();

  // Route-based active states
  let isChat = $derived($page.url.pathname === '/playground' || $page.url.pathname === '/playground/');
  let isProviders = $derived($page.url.pathname.includes('/providers'));
  let isTenants = $derived($page.url.pathname.includes('/tenants'));
  let isPools = $derived($page.url.pathname.includes('/pools'));
  let isLogs = $derived($page.url.pathname.includes('/logs'));
</script>

<LoadingBar />

<div class="app-wrapper flex w-screen h-screen overflow-hidden">
  <!-- Desktop Frame Layout (Cognivo UI) -->
  <div class="cognivo-frame flex w-full h-full overflow-hidden">
    
    <!-- Left Sidebar -->
    <aside class="sidebar flex flex-col w-sidebar shrink-0 border-r p-4 justify-between">
      <div class="flex flex-col gap-6 overflow-hidden">
        <!-- Logo -->
        <div class="flex items-center justify-between">
          <div class="logo flex items-center gap-2">
            <svg class="w-8 h-8 text-[#f97316]" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3">
              <path stroke-linecap="round" stroke-linejoin="round" d="M4 6h16M4 12h16m-7 6h7" />
            </svg>
            <span class="font-bold text-lg tracking-tight select-none">Cognivo</span>
          </div>
          <Button variant="ghost" size="sm" class="p-2" onclick={() => appState.applyTheme(appState.theme === 'dark' ? 'light' : 'dark')}>
            {#if appState.theme === 'dark'}
              <Sun size={16} />
            {:else}
              <Moon size={16} />
            {/if}
          </Button>
        </div>

        <!-- New Chat Button -->
        <Button href="/playground" variant="primary" align="between" class="new-chat-btn w-full" onclick={() => appState.startNewChat()}>
          <span class="flex items-center gap-2">
            <Plus size={20} />
            New Chat
          </span>
          <span class="btn-shortcut">⌘ N</span>
        </Button>

        <!-- Grouped Scrollable History -->
        <div class="history-section flex flex-col gap-4 overflow-y-auto pr-1">
          {#if appState.sidebarChats.today.length > 0}
            <div>
              <div class="history-label text-xs uppercase font-bold tracking-wider mb-2">Today</div>
              <div class="flex flex-col gap-1">
                {#each appState.sidebarChats.today as chat}
                  <a href="/playground" class="history-item flex items-center justify-between p-2.5 rounded-lg w-full text-left {appState.currentChatId === chat.id ? 'active' : ''}" onclick={() => appState.selectChat(chat.id)}>
                    <span class="truncate pr-2">{chat.title}</span>
                    <Trash2 size={14} class="trash-icon hover:text-red-500" onclick={(e) => appState.deleteChat(chat.id, e)} />
                  </a>
                {/each}
              </div>
            </div>
          {/if}

          {#if appState.sidebarChats.yesterday.length > 0}
            <div>
              <div class="history-label text-xs uppercase font-bold tracking-wider mb-2">Yesterday</div>
              <div class="flex flex-col gap-1">
                {#each appState.sidebarChats.yesterday as chat}
                  <a href="/playground" class="history-item flex items-center justify-between p-2.5 rounded-lg w-full text-left {appState.currentChatId === chat.id ? 'active' : ''}" onclick={() => appState.selectChat(chat.id)}>
                    <span class="truncate pr-2">{chat.title}</span>
                    <Trash2 size={14} class="trash-icon hover:text-red-500" onclick={(e) => appState.deleteChat(chat.id, e)} />
                  </a>
                {/each}
              </div>
            </div>
          {/if}

          {#if appState.sidebarChats.older.length > 0}
            <div>
              <div class="history-label text-xs uppercase font-bold tracking-wider mb-2">Older</div>
              <div class="flex flex-col gap-1">
                {#each appState.sidebarChats.older as chat}
                  <a href="/playground" class="history-item flex items-center justify-between p-2.5 rounded-lg w-full text-left {appState.currentChatId === chat.id ? 'active' : ''}" onclick={() => appState.selectChat(chat.id)}>
                    <span class="truncate pr-2">{chat.title}</span>
                    <Trash2 size={14} class="trash-icon hover:text-red-500" onclick={(e) => appState.deleteChat(chat.id, e)} />
                  </a>
                {/each}
              </div>
            </div>
          {/if}
        </div>
      </div>

      <!-- Bottom sidebar info -->
      <div class="flex flex-col gap-2 border-t pt-4 animate-fade-in">
        <Button
          href="/playground"
          variant={isChat ? 'secondary' : 'ghost'}
          align="left"
          class="nav-link w-full {isChat ? 'nav-link-active' : ''}"
        >
          <Sparkles size={18} />
          <span>Chat</span>
        </Button>
        <Button
          href="/playground/providers"
          variant={isProviders ? 'secondary' : 'ghost'}
          align="left"
          class="nav-link w-full {isProviders ? 'nav-link-active' : ''}"
        >
          <KeyRound size={18} />
          <span>Providers</span>
        </Button>
        <Button
          href="/playground/tenants"
          variant={isTenants ? 'secondary' : 'ghost'}
          align="left"
          class="nav-link w-full {isTenants ? 'nav-link-active' : ''}"
        >
          <Users size={18} />
          <span>Tenants</span>
        </Button>
        <Button
          href="/playground/pools"
          variant={isPools ? 'secondary' : 'ghost'}
          align="left"
          class="nav-link w-full {isPools ? 'nav-link-active' : ''}"
        >
          <Cpu size={18} />
          <span>Model Pools</span>
        </Button>
        <Button
          href="/playground/logs"
          variant={isLogs ? 'secondary' : 'ghost'}
          align="left"
          class="nav-link w-full {isLogs ? 'nav-link-active' : ''}"
        >
          <Terminal size={18} />
          <span>Gateway Logs</span>
          {#if appState.logsStreaming}
            <span class="logs-live-badge">LIVE</span>
          {/if}
        </Button>
        <Button
          variant="ghost"
          align="left"
          class="nav-link w-full"
          onclick={() => appState.showSettingsModal = true}
        >
          <Settings size={18} />
          <span>Settings</span>
        </Button>

        <!-- User profile panel -->
        <Card variant="filled" padding="sm" class="profile-card flex-row items-center justify-between">
          <div class="flex items-center gap-3 overflow-hidden">
            <div class="avatar flex items-center justify-center w-9 h-9 rounded-full text-white bg-gradient-to-tr from-orange to-pink font-bold text-xs shrink-0">
              {appState.tenantInitials}
            </div>
            <div class="flex flex-col overflow-hidden text-left">
              <span class="font-bold text-xs truncate">{appState.tenantName || 'Not Connected'}</span>
              <span class="text-[10px] text-[#f97316] font-bold uppercase tracking-wider mt-0.5">
                {#if appState.tenantRateLimit}
                  Limit: {appState.tenantRateLimit} RPM
                {:else}
                  No Limit
                {/if}
              </span>
              {#if appState.tenantBalance}
                <span class="text-[10px] opacity-75 mt-0.5">Bal: {appState.formatBalance(appState.tenantBalance)} tokens</span>
              {/if}
            </div>
          </div>
          <Button variant="ghost" size="sm" class="p-1 shrink-0" onclick={() => appState.showSettingsModal = true} title="Configure Key">
            <Settings size={14} />
          </Button>
        </Card>
      </div>
    </aside>

    <!-- Main Workspace -->
    <main class="main-panel flex flex-col flex-grow overflow-hidden relative">
      {@render children()}
    </main>

    <!-- Side Code Panel (collapsible, Chat page specific logic) -->
    {#if appState.showCodePanel && isChat}
      <aside class="code-panel flex flex-col w-code border-l">
        <header class="flex items-center justify-between px-4 py-3 border-b bg-gray-light">
          <span class="text-xs font-bold uppercase tracking-wider">Integrations</span>
          <Button variant="ghost" size="sm" class="text-orange-500 font-bold" onclick={() => appState.showCodePanel = false}>Close</Button>
        </header>
        
        <div class="flex border-b text-xs">
          <Button variant={appState.activeCodeTab === 'curl' ? 'secondary' : 'ghost'} class="flex-grow rounded-none border-b-2 {appState.activeCodeTab === 'curl' ? 'active' : ''}" style="height:36px; border-radius:0;" onclick={() => appState.activeCodeTab = 'curl'}>cURL</Button>
          <Button variant={appState.activeCodeTab === 'js' ? 'secondary' : 'ghost'} class="flex-grow rounded-none border-b-2 {appState.activeCodeTab === 'js' ? 'active' : ''}" style="height:36px; border-radius:0;" onclick={() => appState.activeCodeTab = 'js'}>JS</Button>
          <Button variant={appState.activeCodeTab === 'python' ? 'secondary' : 'ghost'} class="flex-grow rounded-none border-b-2 {appState.activeCodeTab === 'python' ? 'active' : ''}" style="height:36px; border-radius:0;" onclick={() => appState.activeCodeTab = 'python'}>Python</Button>
        </div>

        <div class="p-4 flex-grow overflow-auto font-mono text-xs bg-black-light leading-relaxed whitespace-pre-wrap select-text">
          {#if appState.activeCodeTab === 'curl'}
            curl {window.location.origin}/v1/chat/completions \
              -H "Authorization: Bearer {appState.apiKey || 'YOUR_KEY'}" \
              -H "Content-Type: application/json" \
              -d '{JSON.stringify({ model: appState.selectedModel, messages: [{role: 'user', content: 'hello'}] }, null, 2)}'
          {:else if appState.activeCodeTab === 'js'}
            const res = await fetch("{window.location.origin}/v1/chat/completions", &#123;
              method: "POST",
              headers: &#123;
                "Authorization": "Bearer {appState.apiKey || 'YOUR_KEY'}",
                "Content-Type": "application/json"
              &#125;,
              body: JSON.stringify(&#123;
                model: "{appState.selectedModel}",
                messages: [&#123;role: "user", content: "hello"&#125;]
              &#125;)
            &#125;);
          {:else}
            import openai
            client = openai.OpenAI(
              api_key="{appState.apiKey || 'YOUR_KEY'}",
              base_url="{window.location.origin}/v1"
            )
            res = client.chat.completions.create(
              model="{appState.selectedModel}",
              messages=[&#123;"role": "user", "content": "hello"&#125;]
            )
          {/if}
        </div>
      </aside>
    {/if}

  </div>
</div>

<!-- Settings Key Config Modal -->
{#if !appState.isInitializing && (appState.showSettingsModal || !appState.apiKey)}
  <SettingsModal
    bind:showSettingsModal={appState.showSettingsModal}
    bind:apiKey={appState.apiKey}
    bind:activeSettingsTab={appState.activeSettingsTab}
    bind:visibleApiKey={appState.visibleApiKey}
    bind:connectError={appState.connectError}
    bind:isConnecting={appState.isConnecting}
    handleSaveKey={() => appState.handleSaveKey()}
    bind:adminApiKey={appState.adminApiKey}
    bind:nvidiaApiKey={appState.nvidiaApiKey}
    bind:nvidiaBaseUrl={appState.nvidiaBaseUrl}
    bind:isAdminConnecting={appState.isAdminConnecting}
    bind:adminConnectSuccess={appState.adminConnectSuccess}
    bind:adminConnectError={appState.adminConnectError}
    handleRegisterNvidia={() => appState.handleRegisterNvidia()}
    isInitializing={appState.isInitializing}
  />
{/if}

<!-- Toast Notifications overlay -->
<ToastsComponent toasts={appState.toasts} removeToast={(id) => appState.removeToast(id)} />

<style>
  :global(body) {
    background-color: var(--bg-color);
    color: var(--text-primary);
    transition: background-color 0.2s, color 0.2s;
  }

  /* Themes support variables */
  :global(:root) {
    --bg-color: #f3f4f6;
    --frame-bg: #ffffff;
    --sidebar-bg: #f9fafb;
    --main-bg: #ffffff;
    --border-color: rgba(0, 0, 0, 0.08);
    --text-primary: #1f2937;
    --text-secondary: #4b5563;
    --item-hover: rgba(0, 0, 0, 0.03);
    --card-bg: #ffffff;
    --shadow-color: rgba(0, 0, 0, 0.04);
  }

  :global(.dark) {
    --bg-color: #0d0d12;
    --frame-bg: #13131a;
    --sidebar-bg: #0c0c0f;
    --main-bg: #13131a;
    --border-color: rgba(255, 255, 255, 0.07);
    --text-primary: #f3f4f6;
    --text-secondary: #9ca3af;
    --item-hover: rgba(255, 255, 255, 0.03);
    --card-bg: #1c1c24;
    --shadow-color: rgba(0, 0, 0, 0.4);
  }

  :global(.app-wrapper) {
    background-color: var(--bg-color);
  }

  :global(.cognivo-frame) {
    background-color: var(--frame-bg);
    border-color: var(--border-color);
    box-shadow: none;
    height: 100%;
    width: 100%;
  }

  /* Sidebar styling */
  :global(.sidebar) {
    background-color: var(--sidebar-bg);
    border-color: var(--border-color);
  }

  :global(.new-chat-btn) {
    background-color: #f97316;
    box-shadow: 0 4px 12px rgba(249, 115, 22, 0.25);
    transition: transform 0.1s, opacity 0.2s;
  }
  :global(.new-chat-btn:hover) {
    opacity: 0.95;
    transform: translateY(-0.5px);
  }
  :global(.btn-shortcut) {
    font-size: 8px;
    background-color: rgba(255, 255, 255, 0.2);
    padding: 2px 4px;
    border-radius: 4px;
    font-weight: bold;
  }

  :global(.nav-link) {
    color: var(--text-secondary);
    font-size: 12px;
    font-weight: 500;
    transition: background-color 0.15s, color 0.15s;
  }
  :global(.nav-link:hover) {
    background-color: var(--item-hover);
    color: var(--text-primary);
  }

  :global(.history-label) {
    color: var(--text-secondary);
    opacity: 0.6;
  }

  :global(.history-item) {
    color: var(--text-secondary);
    font-size: 11px;
    transition: all 0.15s;
    display: flex;
    text-decoration: none;
  }
  :global(.history-item:hover) {
    background-color: var(--item-hover);
    color: var(--text-primary);
  }
  :global(.history-item.active) {
    background-color: rgba(249, 115, 22, 0.08);
    color: #f97316;
    font-weight: 600;
  }
  :global(.trash-icon) {
    opacity: 0;
    transition: opacity 0.15s;
  }
  :global(.history-item:hover .trash-icon) {
    opacity: 0.7;
  }

  :global(.profile-card) {
    background-color: var(--frame-bg);
    border-color: var(--border-color);
  }

  /* Main Workspace styling */
  :global(.main-panel) {
    background-color: var(--main-bg);
    height: 100%;
    position: relative;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
</style>
