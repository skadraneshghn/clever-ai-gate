<script>
  import { onMount, onDestroy } from 'svelte';
  import { 
    Settings, Plus, Trash2, Sparkles, Sun, Moon, KeyRound, Terminal, Users, Cpu
  } from '@lucide/svelte';
  import Button from './lib/components/Button.svelte';
  import Card from './lib/components/Card.svelte';

  // State (using Svelte 5 Runes)
  let theme = $state('light');
  let apiKey = $state('');
  let models = $state([]);
  let selectedModel = $state('');
  let chats = $state([]);
  let currentChatId = $state(null);
  let messages = $state([]);
  
  // Real Tenant info loaded from API
  let tenantName = $state('');
  let tenantBalance = $state(0);
  let tenantRateLimit = $state(0);

  // Compute initials from tenantName reactively
  let tenantInitials = $derived.by(() => {
    if (!tenantName) return 'T';
    const cleanName = tenantName.replace(/[^a-zA-Z0-9\s-_]/g, '');
    const parts = cleanName.split(/[\s\-_]+/);
    const initials = parts.map(p => p[0]).filter(Boolean).join('');
    return initials ? initials.substring(0, 2).toUpperCase() : 'T';
  });
  
  // HUD Telemetry stats
  let statusHUD = $state('Enter API Key');
  let providerHUD = $state('—');
  let modelHUD = $state('—');
  let ttftHUD = $state('—');
  let latencyHUD = $state('—');
  let speedHUD = $state('—');

  // Input states
  let inputText = $state('');
  let isDeeperResearch = $state(false);
  let isSending = $state(false);
  let showSettingsModal = $state(false);
  let showModelDropdown = $state(false);
  let visibleApiKey = $state(false);
  let connectError = $state('');
  let isConnecting = $state(false);
  let isInitializing = $state(true);

  // Settings modal tabs and admin registration state
  let activeSettingsTab = $state('tenant');
  let adminApiKey = $state('');
  let nvidiaApiKey = $state('');
  let nvidiaBaseUrl = $state('https://integrate.api.nvidia.com/v1');
  let isAdminConnecting = $state(false);
  let adminConnectSuccess = $state('');
  let adminConnectError = $state('');

  // Dynamic templates values
  let showCodePanel = $state(false);
  let activeCodeTab = $state('curl');

  // Page routing: 'chat' | 'logs' | 'providers'
  let activePage = $state('chat');

  // Shared Admin Key
  let adminKey = $state('');

  // Logs Console State
  let logLines = $state([]);
  let logsStreaming = $state(false);
  let logsAutoScroll = $state(true);
  let logsError = $state('');
  let logsAbortController = null;

  // Toast Notification System
  let toasts = $state([]);
  let toastCounter = 0;

  function addToast(type, message, timeout = 4000) {
    const id = ++toastCounter;
    toasts = [...toasts, { id, type, message }];
    setTimeout(() => {
      toasts = toasts.filter(t => t.id !== id);
    }, timeout);
  }

  function removeToast(id) {
    toasts = toasts.filter(t => t.id !== id);
  }

  // Sidebar grouping computed state
  let sidebarChats = $derived.by(() => {
    const today = [];
    const yesterday = [];
    const older = [];
    
    chats.forEach(c => {
      const date = new Date(c.updated_at || c.created_at);
      const diffTime = Math.abs(new Date() - date);
      const diffDays = Math.ceil(diffTime / (1000 * 60 * 60 * 24));
      
      if (diffDays <= 1) {
        today.push(c);
      } else if (diffDays <= 2) {
        yesterday.push(c);
      } else {
        older.push(c);
      }
    });

    return { today, yesterday, older };
  });

  // Load configuration from backend/localStorage on mount
  onMount(async () => {
    // Attempt to load keys from default config route (accessible only with Basic Auth credentials)
    try {
      const res = await fetch('/api/v1/playground/config');
      if (res.ok) {
        const data = await res.json();
        if (data.tenant_key) {
          localStorage.setItem('cag_playground_api_key', data.tenant_key);
        }
        if (data.admin_key) {
          localStorage.setItem('cag_admin_key', data.admin_key);
        }
      }
    } catch (e) {
      console.warn('Failed to load default config from backend:', e);
    }

    const savedKey = localStorage.getItem('cag_playground_api_key');
    if (savedKey) {
      statusHUD = 'Verifying...';
      const isValid = await loadTenantInfo(savedKey);
      if (isValid) {
        apiKey = savedKey;
        statusHUD = 'Ready';
        loadModels();
        loadChats();
      } else {
        localStorage.removeItem('cag_playground_api_key');
        apiKey = '';
        statusHUD = 'Enter API Key';
        showSettingsModal = true;
      }
    } else {
      showSettingsModal = true;
    }

    const savedTheme = localStorage.getItem('cag_playground_theme');
    if (savedTheme) {
      theme = savedTheme;
      applyTheme(theme);
    }

    const savedAdminKey = localStorage.getItem('cag_admin_key');
    if (savedAdminKey) {
      adminKey = savedAdminKey;
    }

    isInitializing = false;
  });

  onDestroy(() => {
    stopLogsStream();
  });

  // Apply theme helper
  function applyTheme(newTheme) {
    theme = newTheme;
    localStorage.setItem('cag_playground_theme', newTheme);
    if (newTheme === 'dark') {
      document.documentElement.classList.add('dark');
    } else {
      document.documentElement.classList.remove('dark');
    }
  }

  async function handleSaveKey() {
    const keyToSave = apiKey.trim();
    if (!keyToSave) {
      connectError = 'API key cannot be empty.';
      return;
    }

    connectError = '';
    isConnecting = true;
    statusHUD = 'Connecting...';

    const isValid = await loadTenantInfo(keyToSave);
    if (isValid) {
      apiKey = keyToSave;
      localStorage.setItem('cag_playground_api_key', keyToSave);
      showSettingsModal = false;
      statusHUD = 'Ready';
      loadModels();
      loadChats();
    } else {
      connectError = 'Invalid API key. Please check your credentials and try again.';
      statusHUD = 'Error';
    }
    isConnecting = false;
  }

  async function handleRegisterNvidia() {
    const adminKeyToUse = adminApiKey.trim();
    const nvKeyToUse = nvidiaApiKey.trim();
    const baseUrlToUse = nvidiaBaseUrl.trim();

    if (!adminKeyToUse || !nvKeyToUse || !baseUrlToUse) {
      adminConnectError = 'All fields are required.';
      return;
    }

    adminConnectError = '';
    adminConnectSuccess = '';
    isAdminConnecting = true;

    try {
      const res = await fetch('/api/v1/admin/providers/nvidia', {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${adminKeyToUse}`,
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({
          provider: 'nvidia',
          api_key: nvKeyToUse,
          base_url: baseUrlToUse,
          weight: 1
        })
      });

      if (res.status === 200) {
        const data = await res.json();
        adminConnectSuccess = `Successfully registered ${data.models_count || 0} models!`;
        if (apiKey) {
          loadModels();
        }
      } else {
        const errData = await res.json();
        adminConnectError = errData.details || errData.error || 'Failed to register provider.';
      }
    } catch (e) {
      adminConnectError = `Network error: ${e.message}`;
    } finally {
      isAdminConnecting = false;
    }
  }

  function handleModelPickerClick() {
    if (models.length === 0) {
      activeSettingsTab = 'admin';
      showSettingsModal = true;
    } else {
      showModelDropdown = !showModelDropdown;
    }
  }

  async function loadTenantInfo(keyToUse = apiKey) {
    if (!keyToUse.trim()) return false;
    try {
      const res = await fetch('/api/v1/playground/tenant', {
        headers: {
          'Authorization': `Bearer ${keyToUse}`
        }
      });
      if (res.status === 200) {
        const data = await res.json();
        tenantName = data.name || 'Tenant';
        tenantBalance = data.token_balance || 0;
        tenantRateLimit = data.rate_limit_rpm || 0;
        return true;
      }
      return false;
    } catch (e) {
      console.error('Failed to load tenant info', e);
      return false;
    }
  }

  function formatBalance(num) {
    if (num >= 1e9) return (num / 1e9).toFixed(1) + 'B';
    if (num >= 1e6) return (num / 1e6).toFixed(1) + 'M';
    if (num >= 1e3) return (num / 1e3).toFixed(1) + 'K';
    return num.toString();
  }

  async function loadModels() {
    try {
      const res = await fetch('/v1/models', {
        headers: {
          'Authorization': `Bearer ${apiKey}`
        }
      });
      if (res.status === 200) {
        const data = await res.json();
        models = data.data || [];
        if (models.length > 0) {
          selectedModel = models[0].id;
        }
        statusHUD = 'Ready';
      } else {
        statusHUD = `Error: ${res.statusText}`;
      }
    } catch (e) {
      statusHUD = 'Failed to fetch models';
    }
  }

  async function loadChats() {
    try {
      const res = await fetch('/api/v1/playground/chats', {
        headers: {
          'Authorization': `Bearer ${apiKey}`
        }
      });
      if (res.status === 200) {
        chats = await res.json();
      }
    } catch (e) {
      console.error('Failed to load chat sessions', e);
    }
  }

  async function selectChat(id) {
    currentChatId = id;
    try {
      const res = await fetch(`/api/v1/playground/chats/${id}`, {
        headers: {
          'Authorization': `Bearer ${apiKey}`
        }
      });
      if (res.status === 200) {
        const data = await res.json();
        messages = data.messages || [];
      }
    } catch (e) {
      console.error('Failed to fetch conversation details', e);
    }
  }

  async function startNewChat() {
    currentChatId = null;
    messages = [];
    inputText = '';
  }

  async function deleteChat(id, e) {
    e.stopPropagation();
    try {
      const res = await fetch(`/api/v1/playground/chats/${id}`, {
        method: 'DELETE',
        headers: {
          'Authorization': `Bearer ${apiKey}`
        }
      });
      if (res.status === 200) {
        loadChats();
        if (currentChatId === id) {
          startNewChat();
        }
      }
    } catch (e) {
      console.error('Failed to delete chat', e);
    }
  }

  function applyPreset(text) {
    inputText = text;
  }

  async function submitPrompt() {
    if (!inputText.trim() || isSending) return;
    
    const userMsg = { role: 'user', content: inputText };
    messages = [...messages, userMsg];
    const originalInput = inputText;
    inputText = '';
    isSending = true;

    statusHUD = 'Streaming...';
    providerHUD = '—';
    modelHUD = selectedModel;
    ttftHUD = 'Calculating...';
    latencyHUD = 'Calculating...';
    speedHUD = 'Calculating...';

    const startTime = performance.now();
    let ttftTime = 0;
    let firstTokenReceived = false;
    let tokenCount = 0;
    
    const assistantPlaceholder = { role: 'assistant', content: '', reasoning_content: '' };
    messages = [...messages, assistantPlaceholder];
    const assistantIndex = messages.length - 1;

    try {
      const response = await fetch('/v1/chat/completions', {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${apiKey}`,
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({
          model: selectedModel,
          messages: messages.slice(0, -1).map(m => ({ role: m.role, content: m.content })),
          stream: true,
          temperature: 0.7
        })
      });

      if (response.status !== 200) {
        const errorText = await response.text();
        messages[assistantIndex].content = `Error: ${errorText}`;
        statusHUD = `Error [${response.status}]`;
        isSending = false;
        return;
      }

      const gwProvider = response.headers.get('X-Gateway-Provider');
      const gwModel = response.headers.get('X-Gateway-Model-Pattern');
      if (gwProvider) providerHUD = gwProvider.toUpperCase();
      if (gwModel) modelHUD = gwModel;

      const reader = response.body.getReader();
      const decoder = new TextDecoder('utf-8');
      let buffer = '';

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        if (!firstTokenReceived) {
          firstTokenReceived = true;
          ttftTime = performance.now() - startTime;
          ttftHUD = `${Math.round(ttftTime)}ms`;
        }

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop();

        for (const line of lines) {
          const cleanedLine = line.trim();
          if (!cleanedLine) continue;

          if (cleanedLine.startsWith('data: ')) {
            const dataStr = cleanedLine.slice(6);
            if (dataStr === '[DONE]') continue;

            try {
              const parsed = JSON.parse(dataStr);
              const delta = parsed.choices[0].delta;
              
              if (delta.reasoning_content) {
                messages[assistantIndex].reasoning_content += delta.reasoning_content;
                tokenCount++;
              } else if (delta.content) {
                messages[assistantIndex].content += delta.content;
                tokenCount++;
              }
            } catch (e) {
              // Parse error
            }
          }
        }

        const elapsed = (performance.now() - startTime) / 1000;
        if (elapsed > 0) {
          speedHUD = `${Math.round(tokenCount / elapsed)} tok/s`;
        }
      }

      const totalElapsed = performance.now() - startTime;
      latencyHUD = `${Math.round(totalElapsed)}ms`;
      statusHUD = 'Done';

      await saveConversation(originalInput);

    } catch (err) {
      statusHUD = 'Connection Failed';
      messages[assistantIndex].content = `Connection failed: ${err.message}`;
    } finally {
      isSending = false;
    }
  }

  async function saveConversation(firstPrompt) {
    const title = messages[0].content.substring(0, 35) + (messages[0].content.length > 35 ? '...' : '');
    
    try {
      if (currentChatId) {
        await fetch(`/api/v1/playground/chats/${currentChatId}`, {
          method: 'PUT',
          headers: {
            'Authorization': `Bearer ${apiKey}`,
            'Content-Type': 'application/json'
          },
          body: JSON.stringify({
            title: title,
            messages: messages
          })
        });
      } else {
        const res = await fetch('/api/v1/playground/chats', {
          method: 'POST',
          headers: {
            'Authorization': `Bearer ${apiKey}`,
            'Content-Type': 'application/json'
          },
          body: JSON.stringify({
            title: title,
            messages: messages
          })
        });
        if (res.status === 201) {
          const data = await res.json();
          currentChatId = data.id;
        }
      }
      loadChats();
    } catch (e) {
      console.error('Failed to auto-save conversation', e);
    }
  }

  async function startLogsStream() {
    if (logsStreaming) return;
    const key = adminKey.trim();
    if (!key) {
      logsError = 'Admin API key is required to stream logs.';
      return;
    }
    localStorage.setItem('cag_admin_key', key);
    logsError = '';
    logsStreaming = true;
    logsAbortController = new AbortController();

    try {
      const resp = await fetch('/api/v1/admin/logs/stream', {
        headers: { 'Authorization': `Bearer ${key}` },
        signal: logsAbortController.signal,
      });

      if (!resp.ok) {
        const errText = await resp.text();
        logsError = `Server error ${resp.status}: ${errText}`;
        logsStreaming = false;
        return;
      }

      const reader = resp.body.getReader();
      const decoder = new TextDecoder();
      let buf = '';

      while (logsStreaming) {
        const { value, done } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });
        const frames = buf.split('\n\n');
        buf = frames.pop();

        for (const frame of frames) {
          const trimmed = frame.trim();
          if (!trimmed || trimmed.startsWith(': ')) continue;
          if (trimmed.startsWith('data: ')) {
            const raw = trimmed.slice(6).trim();
            try {
              const parsed = JSON.parse(raw);
              logLines = [...logLines.slice(-499), parsed];
            } catch {
              // Ignore non-JSON
            }
          }
        }
      }
    } catch (err) {
      if (err.name !== 'AbortError') {
        logsError = `Stream error: ${err.message}`;
        setTimeout(() => {
          if (!logsStreaming) startLogsStream();
        }, 3000);
      }
    } finally {
      logsStreaming = false;
    }
  }

  function stopLogsStream() {
    logsStreaming = false;
    logsAbortController?.abort();
    logsAbortController = null;
  }

  function clearLogs() {
    logLines = [];
  }

  async function downloadTodayLog() {
    const key = adminKey.trim();
    if (!key) { logsError = 'Admin API key required.'; return; }
    try {
      const resp = await fetch('/api/v1/admin/logs/download', {
        headers: { 'Authorization': `Bearer ${key}` },
      });
      if (!resp.ok) {
        logsError = `Download failed: ${resp.status}`;
        return;
      }
      const blob = await resp.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      const today = new Date().toISOString().slice(0, 10);
      a.href = url;
      a.download = `gateway-${today}.log`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    } catch (err) {
      logsError = `Download error: ${err.message}`;
    }
  }

  function logLevelClass(level) {
    switch ((level || '').toLowerCase()) {
      case 'debug': return 'lvl-debug';
      case 'warn':  return 'lvl-warn';
      case 'error': return 'lvl-error';
      case 'fatal': return 'lvl-fatal';
      default:      return 'lvl-info';
    }
  }

  function formatLogTime(ts) {
    if (!ts) return '';
    try {
      const d = new Date(ts);
      return d.toTimeString().slice(0, 8) + '.' + String(d.getMilliseconds()).padStart(3, '0');
    } catch { return ts; }
  }
</script>

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
          <Button variant="ghost" size="sm" class="p-2" onclick={() => applyTheme(theme === 'dark' ? 'light' : 'dark')}>
            {#if theme === 'dark'}
              <Sun size={16} />
            {:else}
              <Moon size={16} />
            {/if}
          </Button>
        </div>

        <!-- New Chat Button -->
        <Button variant="primary" class="new-chat-btn w-full justify-between" onclick={startNewChat}>
          <span class="flex items-center gap-2">
            <Plus size={20} />
            New Chat
          </span>
          <span class="btn-shortcut">⌘ N</span>
        </Button>

        <!-- Grouped Scrollable History -->
        <div class="history-section flex flex-col gap-4 overflow-y-auto pr-1">
          {#if sidebarChats.today.length > 0}
            <div>
              <div class="history-label text-xs uppercase font-bold tracking-wider mb-2">Today</div>
              <div class="flex flex-col gap-1">
                {#each sidebarChats.today as chat}
                  <button class="history-item flex items-center justify-between p-2.5 rounded-lg w-full text-left {currentChatId === chat.id ? 'active' : ''}" onclick={() => selectChat(chat.id)}>
                    <span class="truncate pr-2">{chat.title}</span>
                    <Trash2 size={14} class="trash-icon hover:text-red-500" onclick={(e) => deleteChat(chat.id, e)} />
                  </button>
                {/each}
              </div>
            </div>
          {/if}

          {#if sidebarChats.yesterday.length > 0}
            <div>
              <div class="history-label text-xs uppercase font-bold tracking-wider mb-2">Yesterday</div>
              <div class="flex flex-col gap-1">
                {#each sidebarChats.yesterday as chat}
                  <button class="history-item flex items-center justify-between p-2.5 rounded-lg w-full text-left {currentChatId === chat.id ? 'active' : ''}" onclick={() => selectChat(chat.id)}>
                    <span class="truncate pr-2">{chat.title}</span>
                    <Trash2 size={14} class="trash-icon hover:text-red-500" onclick={(e) => deleteChat(chat.id, e)} />
                  </button>
                {/each}
              </div>
            </div>
          {/if}

          {#if sidebarChats.older.length > 0}
            <div>
              <div class="history-label text-xs uppercase font-bold tracking-wider mb-2">Older</div>
              <div class="flex flex-col gap-1">
                {#each sidebarChats.older as chat}
                  <button class="history-item flex items-center justify-between p-2.5 rounded-lg w-full text-left {currentChatId === chat.id ? 'active' : ''}" onclick={() => selectChat(chat.id)}>
                    <span class="truncate pr-2">{chat.title}</span>
                    <Trash2 size={14} class="trash-icon hover:text-red-500" onclick={(e) => deleteChat(chat.id, e)} />
                  </button>
                {/each}
              </div>
            </div>
          {/if}
        </div>
      </div>

      <!-- Bottom sidebar info -->
      <div class="flex flex-col gap-2 border-t pt-4">
        <Button
          variant={activePage === 'chat' ? 'secondary' : 'ghost'}
          class="nav-link w-full justify-start {activePage === 'chat' ? 'nav-link-active' : ''}"
          onclick={() => { activePage = 'chat'; }}
        >
          <Sparkles size={18} />
          <span>Chat</span>
        </Button>
        <Button
          variant={activePage === 'providers' ? 'secondary' : 'ghost'}
          class="nav-link w-full justify-start {activePage === 'providers' ? 'nav-link-active' : ''}"
          onclick={() => { activePage = 'providers'; }}
        >
          <KeyRound size={18} />
          <span>Providers</span>
        </Button>
        <Button
          variant={activePage === 'tenants' ? 'secondary' : 'ghost'}
          class="nav-link w-full justify-start {activePage === 'tenants' ? 'nav-link-active' : ''}"
          onclick={() => { activePage = 'tenants'; }}
        >
          <Users size={18} />
          <span>Tenants</span>
        </Button>
        <Button
          variant={activePage === 'pools' ? 'secondary' : 'ghost'}
          class="nav-link w-full justify-start {activePage === 'pools' ? 'nav-link-active' : ''}"
          onclick={() => { activePage = 'pools'; }}
        >
          <Cpu size={18} />
          <span>Model Pools</span>
        </Button>
        <Button
          variant={activePage === 'logs' ? 'secondary' : 'ghost'}
          class="nav-link w-full justify-start {activePage === 'logs' ? 'nav-link-active' : ''}"
          onclick={() => { activePage = 'logs'; if (!logsStreaming && adminKey) startLogsStream(); }}
        >
          <Terminal size={18} />
          <span>Gateway Logs</span>
          {#if logsStreaming}
            <span class="logs-live-badge">LIVE</span>
          {/if}
        </Button>
        <Button
          variant="ghost"
          class="nav-link w-full justify-start"
          onclick={() => showSettingsModal = true}
        >
          <Settings size={18} />
          <span>Settings</span>
        </Button>

        <!-- User profile panel -->
        <Card variant="filled" padding="sm" class="profile-card flex-row items-center justify-between">
          <div class="flex items-center gap-3 overflow-hidden">
            <div class="avatar flex items-center justify-center w-9 h-9 rounded-full text-white bg-gradient-to-tr from-orange to-pink font-bold text-xs shrink-0">
              {tenantInitials}
            </div>
            <div class="flex flex-col overflow-hidden text-left">
              <span class="font-bold text-xs truncate">{tenantName || 'Not Connected'}</span>
              <span class="text-[10px] text-[#f97316] font-bold uppercase tracking-wider mt-0.5">
                {#if tenantRateLimit}
                  Limit: {tenantRateLimit} RPM
                {:else}
                  No Limit
                {/if}
              </span>
              {#if tenantBalance}
                <span class="text-[10px] opacity-75 mt-0.5">Bal: {formatBalance(tenantBalance)} tokens</span>
              {/if}
            </div>
          </div>
          <Button variant="ghost" size="sm" class="p-1 shrink-0" onclick={() => showSettingsModal = true} title="Configure Key">
            <Settings size={14} />
          </Button>
        </Card>
      </div>
    </aside>

    <!-- Main Workspace -->
    <main class="main-panel flex flex-col flex-grow overflow-hidden relative">

      {#if activePage === 'logs'}
        <!-- ═══════════════════════════════════════════════════════════════ -->
        <!-- LOGS PAGE (lazy loaded)                                         -->
        <!-- ═══════════════════════════════════════════════════════════════ -->
        {#await import('./lib/LogsPage.svelte') then { default: LogsPage }}
          <LogsPage
            bind:adminKey
            bind:logLines
            bind:logsStreaming
            bind:logsAutoScroll
            bind:logsError
            {startLogsStream}
            {stopLogsStream}
            {clearLogs}
            {downloadTodayLog}
            {formatLogTime}
            {logLevelClass}
          />
        {/await}

      {:else if activePage === 'providers'}
        <!-- ═══════════════════════════════════════════════════════════════ -->
        <!-- PROVIDERS PAGE (lazy loaded)                                    -->
        <!-- ═══════════════════════════════════════════════════════════════ -->
        {#await import('./lib/ProvidersPage.svelte') then { default: ProvidersPage }}
          <ProvidersPage
            bind:adminKey
            {apiKey}
            {loadModels}
            {addToast}
          />
        {/await}

      {:else if activePage === 'tenants'}
        <!-- ═══════════════════════════════════════════════════════════════ -->
        <!-- TENANTS PAGE (lazy loaded)                                      -->
        <!-- ═══════════════════════════════════════════════════════════════ -->
        {#await import('./lib/TenantsPage.svelte') then { default: TenantsPage }}
          <TenantsPage
            bind:adminKey
            {addToast}
          />
        {/await}

      {:else if activePage === 'pools'}
        <!-- ═══════════════════════════════════════════════════════════════ -->
        <!-- MODEL POOLS PAGE (lazy loaded)                                  -->
        <!-- ═══════════════════════════════════════════════════════════════ -->
        {#await import('./lib/PoolsPage.svelte') then { default: PoolsPage }}
          <PoolsPage
            bind:adminKey
            {addToast}
          />
        {/await}

      {:else}
        <!-- ═══════════════════════════════════════════════════════════════ -->
        <!-- CHAT PAGE (lazy loaded)                                         -->
        <!-- ═══════════════════════════════════════════════════════════════ -->
        {#await import('./lib/ChatPage.svelte') then { default: ChatPage }}
          <ChatPage
            {apiKey}
            {models}
            bind:selectedModel
            bind:showModelDropdown
            bind:showCodePanel
            bind:activeCodeTab
            bind:messages
            bind:inputText
            bind:isDeeperResearch
            bind:isSending
            {handleModelPickerClick}
            {submitPrompt}
            {applyPreset}
            {statusHUD}
            {providerHUD}
            {modelHUD}
            {ttftHUD}
            {latencyHUD}
            {speedHUD}
          />
        {/await}
      {/if}
    </main>

    <!-- Side Code Panel (collapsible) -->
    {#if showCodePanel && activePage === 'chat'}
      <aside class="code-panel flex flex-col w-code border-l">
        <header class="flex items-center justify-between px-4 py-3 border-b bg-gray-light">
          <span class="text-xs font-bold uppercase tracking-wider">Integrations</span>
          <Button variant="ghost" size="sm" class="text-orange-500 font-bold" onclick={() => showCodePanel = false}>Close</Button>
        </header>
        
        <div class="flex border-b text-xs">
          <Button variant={activeCodeTab === 'curl' ? 'secondary' : 'ghost'} class="flex-grow rounded-none border-b-2 {activeCodeTab === 'curl' ? 'active' : ''}" style="height:36px; border-radius:0;" onclick={() => activeCodeTab = 'curl'}>cURL</Button>
          <Button variant={activeCodeTab === 'js' ? 'secondary' : 'ghost'} class="flex-grow rounded-none border-b-2 {activeCodeTab === 'js' ? 'active' : ''}" style="height:36px; border-radius:0;" onclick={() => activeCodeTab = 'js'}>JS</Button>
          <Button variant={activeCodeTab === 'python' ? 'secondary' : 'ghost'} class="flex-grow rounded-none border-b-2 {activeCodeTab === 'python' ? 'active' : ''}" style="height:36px; border-radius:0;" onclick={() => activeCodeTab = 'python'}>Python</Button>
        </div>

        <div class="p-4 flex-grow overflow-auto font-mono text-xs bg-black-light leading-relaxed whitespace-pre-wrap select-text">
          {#if activeCodeTab === 'curl'}
            curl {window.location.origin}/v1/chat/completions \
              -H "Authorization: Bearer {apiKey || 'YOUR_KEY'}" \
              -H "Content-Type: application/json" \
              -d '{JSON.stringify({ model: selectedModel, messages: [{role: 'user', content: 'hello'}] }, null, 2)}'
          {:else if activeCodeTab === 'js'}
            const res = await fetch("{window.location.origin}/v1/chat/completions", &#123;
              method: "POST",
              headers: &#123;
                "Authorization": "Bearer {apiKey || 'YOUR_KEY'}",
                "Content-Type": "application/json"
              &#125;,
              body: JSON.stringify(&#123;
                model: "{selectedModel}",
                messages: [&#123;role: "user", content: "hello"&#125;]
              &#125;)
            &#125;);
          {:else}
            import openai
            client = openai.OpenAI(
              api_key="{apiKey || 'YOUR_KEY'}",
              base_url="{window.location.origin}/v1"
            )
            res = client.chat.completions.create(
              model="{selectedModel}",
              messages=[&#123;"role": "user", "content": "hello"&#125;]
            )
          {/if}
        </div>
      </aside>
    {/if}

  </div>
</div>

<!-- Settings Key Config Modal (lazy loaded) -->
{#if !isInitializing && (showSettingsModal || !apiKey)}
  {#await import('./lib/SettingsModal.svelte') then { default: SettingsModal }}
    <SettingsModal
      bind:showSettingsModal
      bind:apiKey
      bind:activeSettingsTab
      bind:visibleApiKey
      bind:connectError
      bind:isConnecting
      {handleSaveKey}
      bind:adminApiKey
      bind:nvidiaApiKey
      bind:nvidiaBaseUrl
      bind:isAdminConnecting
      bind:adminConnectSuccess
      bind:adminConnectError
      {handleRegisterNvidia}
      {isInitializing}
    />
  {/await}
{/if}

<!-- Toast Notifications overlay (lazy loaded) -->
{#await import('./lib/Toasts.svelte') then { default: ToastsComponent }}
  <ToastsComponent {toasts} {removeToast} />
{/await}

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
  :global(.header), :global(.telemetry-bar), :global(.footer) {
    border-color: var(--border-color);
  }

  :global(.model-picker-btn) {
    color: var(--text-primary);
    background: var(--item-hover);
    padding: 6px 12px;
    border-radius: 20px;
    transition: opacity 0.15s;
  }
  :global(.model-picker-btn:hover) {
    opacity: 0.9;
  }

  :global(.model-dropdown) {
    background-color: var(--card-bg);
    border-color: var(--border-color);
  }
  :global(.model-option) {
    color: var(--text-primary);
    transition: background-color 0.15s;
  }
  :global(.model-option:hover) {
    background-color: var(--item-hover);
  }
  :global(.model-option.active) {
    color: #f97316;
    background-color: rgba(249, 115, 22, 0.06);
    font-weight: bold;
  }

  :global(.hud-value) {
    color: var(--text-primary);
    font-weight: 600;
  }

  /* Chat Scrollable Area & Layout Centering */
  :global(.chat-scroll-area) {
    flex-grow: 1;
    overflow-y: auto;
    width: 100%;
    position: relative;
  }

  :global(.landing-container) {
    width: 100%;
    max-width: 768px;
    margin: 0 auto;
    padding: 6rem 1.5rem 2rem 1.5rem;
    box-sizing: border-box;
  }

  :global(.chat-content-container) {
    width: 100%;
    max-width: 768px;
    margin: 0 auto;
    display: flex;
    flex-direction: column;
    gap: 1.5rem;
    padding: 2rem 1.5rem 150px 1.5rem;
    box-sizing: border-box;
  }

  /* Prompt Pill Card (ChatGPT input styling) */
  :global(.prompt-pill-card) {
    background-color: var(--card-bg);
    border: 1px solid var(--border-color);
    border-radius: 26px;
    padding: 12px 18px;
    box-shadow: 0 4px 24px var(--shadow-color);
    width: 100%;
    max-width: 768px;
    margin: 0 auto;
    display: flex;
    flex-direction: column;
    gap: 8px;
    box-sizing: border-box;
  }

  :global(.prompt-textarea) {
    background: transparent;
    color: var(--text-primary);
    border: none;
    outline: none;
    font-family: inherit;
    resize: none;
    font-size: 14px;
    line-height: 1.5;
  }

  :global(.action-icon-btn) {
    color: var(--text-secondary);
    padding: 6px;
    border-radius: 50%;
    transition: all 0.15s;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  :global(.action-icon-btn:hover) {
    color: var(--text-primary);
    background: var(--item-hover);
  }

  :global(.deeper-btn) {
    color: var(--text-secondary);
    border-color: var(--border-color);
    transition: all 0.2s;
    background: transparent;
    cursor: pointer;
    font-size: 10px;
    font-weight: 700;
  }
  :global(.deeper-btn:hover) {
    color: var(--text-primary);
    border-color: var(--text-secondary);
  }
  :global(.deeper-btn.active) {
    color: #f97316;
    border-color: #f97316;
    background: rgba(249, 115, 22, 0.05);
  }

  /* Presets horizontal layout styling */
  :global(.presets-container) {
    display: flex;
    gap: 10px;
    justify-content: center;
    flex-wrap: wrap;
    width: 100%;
  }

  :global(.preset-pill) {
    background-color: var(--card-bg);
    border: 1px solid var(--border-color);
    color: var(--text-secondary);
    transition: all 0.2s;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
  }
  :global(.preset-pill:hover) {
    border-color: #f97316;
    color: var(--text-primary);
    background-color: var(--item-hover);
    transform: translateY(-0.5px);
  }

  /* Floating Bottom Input Bar Container */
  :global(.bottom-input-container) {
    position: absolute;
    bottom: 0;
    left: 0;
    right: 0;
    padding: 1.5rem 1.5rem 1.5rem 1.5rem;
    background: linear-gradient(to top, var(--main-bg) 60%, transparent 100%);
    display: flex;
    flex-direction: column;
    align-items: center;
    z-index: 10;
    box-sizing: border-box;
  }

  /* Chat Bubble flow elements */
  :global(.bubble-content) {
    background-color: var(--card-bg);
    border-color: var(--border-color);
    color: var(--text-primary);
    box-shadow: 0 2px 8px var(--shadow-color);
  }
  :global(.reasoning-container) {
    background-color: rgba(249, 115, 22, 0.03);
    border-left-color: rgba(249, 115, 22, 0.3);
  }

  /* Sidebar Code Panel */
  :global(.code-panel) {
    background-color: var(--sidebar-bg);
    border-color: var(--border-color);
  }
  :global(.tab-btn) {
    border: none;
    color: var(--text-secondary);
    background: transparent;
    transition: all 0.2s;
    font-weight: 500;
  }
  :global(.tab-btn.active) {
    color: #f97316;
    border-bottom: 2px solid #f97316;
  }

  /* Modal Settings */
  :global(.modal-content) {
    background-color: var(--card-bg);
    border-color: var(--border-color);
    color: var(--text-primary);
  }
  :global(.input-box) {
    background-color: var(--frame-bg);
    border-color: var(--border-color);
    color: var(--text-primary);
  }

  /* Shared Utility icons button style */
  :global(.icon-button) {
    color: var(--text-secondary);
    padding: 6px;
    border-radius: 6px;
    transition: all 0.15s;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  :global(.icon-button:hover) {
    color: var(--text-primary);
    background: var(--item-hover);
  }

  @keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }
  :global(.animate-spin) {
    animation: spin 1s linear infinite;
    display: inline-block;
  }
  /* ── Logs Page ──────────────────────────────────────────────────────────── */

  :global(.nav-link-active) {
    color: #f97316 !important;
    background-color: rgba(249, 115, 22, 0.08);
    font-weight: 600;
  }

  :global(.logs-live-badge) {
    font-size: 8px;
    font-weight: 800;
    letter-spacing: 0.08em;
    color: #04d361;
    background: rgba(4, 211, 97, 0.12);
    border: 1px solid rgba(4, 211, 97, 0.3);
    border-radius: 4px;
    padding: 1px 5px;
    margin-left: auto;
    text-transform: uppercase;
  }

  /* Pulsing live indicator dot */
  :global(.log-pulse-dot) {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: #04d361;
    box-shadow: 0 0 0 0 rgba(4, 211, 97, 0.6);
    animation: log-pulse 1.8s ease-in-out infinite;
    display: inline-block;
    flex-shrink: 0;
  }
  @keyframes log-pulse {
    0%   { box-shadow: 0 0 0 0 rgba(4, 211, 97, 0.6); }
    60%  { box-shadow: 0 0 0 7px rgba(4, 211, 97, 0); }
    100% { box-shadow: 0 0 0 0 rgba(4, 211, 97, 0); }
  }

  /* Action buttons in log header */
  :global(.log-action-btn) {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 4px 8px;
    border-radius: 6px;
    border: 1px solid var(--border-color);
    font-size: 10px;
    font-weight: 600;
    background: var(--item-hover);
    color: var(--text-secondary);
    transition: all 0.15s;
    cursor: pointer;
  }
  :global(.log-action-btn:hover) {
    color: var(--text-primary);
    background: var(--border-color);
  }
  :global(.log-btn-start) {
    border-color: rgba(4, 211, 97, 0.4);
    color: #04d361;
    background: rgba(4, 211, 97, 0.06);
  }
  :global(.log-btn-start:hover) {
    background: rgba(4, 211, 97, 0.12);
  }
  :global(.log-btn-stop) {
    border-color: rgba(247, 64, 64, 0.4);
    color: #f74040;
    background: rgba(247, 64, 64, 0.06);
  }
  :global(.log-btn-stop:hover) {
    background: rgba(247, 64, 64, 0.12);
  }

  :global(.log-checkbox) {
    accent-color: #f97316;
  }

  /* Log Terminal layout */
  :global(.log-terminal-wrap) {
    flex: 1;
    display: flex;
    flex-direction: column;
    background: #09090b;
    border-top: 1px solid var(--border-color);
    overflow: hidden;
  }
  :global(.log-stats-bar) {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 6px 16px;
    background: #18181b;
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
    font-family: monospace;
    font-size: 9px;
    color: #a1a1aa;
  }
  :global(.log-terminal) {
    flex: 1;
    padding: 16px;
    overflow-y: auto;
    font-family: 'Fira Code', 'Courier New', Courier, monospace;
    font-size: 11px;
    line-height: 1.6;
    color: #e4e4e7;
    background: #09090b;
  }

  :global(.log-empty) {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: #71717a;
    text-align: center;
  }

  /* Log rows by severity level */
  :global(.log-row) {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    padding: 2px 0;
    border-bottom: 1px solid rgba(255, 255, 255, 0.02);
  }
  :global(.log-time) {
    color: #71717a;
    font-size: 10px;
    width: 95px;
    flex-shrink: 0;
  }
  :global(.log-lvl) {
    font-weight: 700;
    font-size: 9px;
    padding: 1px 4px;
    border-radius: 4px;
    width: 40px;
    text-align: center;
    flex-shrink: 0;
  }
  :global(.lvl-debug .log-lvl) { background: rgba(59, 130, 246, 0.15); color: #60a5fa; }
  :global(.lvl-info .log-lvl)  { background: rgba(255, 255, 255, 0.08); color: #e4e4e7; }
  :global(.lvl-warn .log-lvl)  { background: rgba(234, 179, 8, 0.15); color: #facc15; }
  :global(.lvl-error .log-lvl) { background: rgba(239, 68, 68, 0.15); color: #f87171; }
  :global(.lvl-fatal .log-lvl) { background: rgba(236, 72, 153, 0.2); color: #f472b6; border: 1px solid rgba(236, 72, 153, 0.4); }

  :global(.log-msg) {
    color: #e4e4e7;
    word-break: break-all;
    flex: 1;
    min-width: 200px;
  }
  :global(.log-caller) {
    color: #52525b;
    font-size: 9px;
    margin-left: auto;
    font-style: italic;
  }

  /* Meta fields (model, provider) */
  :global(.log-meta) {
    color: #404050;
    font-size: 10px;
  }

  /* Error detail */
  :global(.log-err-detail) {
    color: #f74040;
    font-size: 11px;
    width: 100%;
    padding-left: calc(95px + 40px + 16px);
    margin-top: 1px;
  }

  /* ── Providers Management Page ─────────────────────────────────────────── */

  :global(.providers-grid-wrap) {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    padding: 0;
  }

  :global(.providers-loading) {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 4rem 2rem;
    color: var(--text-secondary);
  }

  :global(.providers-table-container) {
    flex: 1;
    overflow: auto;
    padding: 0;
  }

  :global(.providers-table) {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
  }

  :global(.providers-table thead) {
    position: sticky;
    top: 0;
    z-index: 5;
    background-color: var(--sidebar-bg);
  }

  :global(.providers-table th) {
    padding: 10px 14px;
    text-align: left;
    font-size: 10px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-secondary);
    border-bottom: 1px solid var(--border-color);
    white-space: nowrap;
  }

  :global(.providers-table td) {
    padding: 10px 14px;
    border-bottom: 1px solid var(--border-color);
    color: var(--text-primary);
    vertical-align: middle;
  }

  :global(.provider-row) {
    transition: background-color 0.15s;
  }
  :global(.provider-row:hover) {
    background-color: var(--item-hover);
  }

  /* Provider Badges */
  :global(.provider-badge) {
    display: inline-block;
    padding: 2px 8px;
    border-radius: 4px;
    font-size: 10px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }

  :global(.badge-openai) {
    background: rgba(16, 163, 127, 0.1);
    color: #10a37f;
    border: 1px solid rgba(16, 163, 127, 0.25);
  }
  :global(.badge-nvidia) {
    background: rgba(118, 185, 0, 0.1);
    color: #76b900;
    border: 1px solid rgba(118, 185, 0, 0.25);
  }
  :global(.badge-ollama) {
    background: rgba(255, 255, 255, 0.08);
    color: var(--text-primary);
    border: 1px solid var(--border-color);
  }
  :global(.badge-anthropic) {
    background: rgba(204, 143, 82, 0.1);
    color: #cc8f52;
    border: 1px solid rgba(204, 143, 82, 0.25);
  }
  :global(.badge-default) {
    background: rgba(107, 114, 128, 0.1);
    color: var(--text-secondary);
    border: 1px solid var(--border-color);
  }
  :global(.badge-custom) {
    background: rgba(99, 102, 241, 0.1);
    color: #818cf8;
    border: 1px solid rgba(99, 102, 241, 0.25);
  }

  /* Health Indicator Dot */
  :global(.health-dot) {
    display: inline-block;
    width: 10px;
    height: 10px;
    border-radius: 50%;
    cursor: help;
  }
  :global(.health-dot.healthy) {
    background: #04d361;
    box-shadow: 0 0 6px rgba(4, 211, 97, 0.4);
  }
  :global(.health-dot.unhealthy) {
    background: #f74040;
    box-shadow: 0 0 6px rgba(247, 64, 64, 0.4);
  }

  /* Toggle Switch */
  :global(.toggle-switch) {
    position: relative;
    display: inline-block;
    width: 36px;
    height: 20px;
  }
  :global(.toggle-switch input) {
    opacity: 0;
    width: 0;
    height: 0;
  }
  :global(.toggle-slider) {
    position: absolute;
    cursor: pointer;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background-color: rgba(107, 114, 128, 0.3);
    transition: 0.2s;
    border-radius: 20px;
  }
  :global(.toggle-slider:before) {
    position: absolute;
    content: "";
    height: 14px;
    width: 14px;
    left: 3px;
    bottom: 3px;
    background-color: white;
    transition: 0.2s;
    border-radius: 50%;
  }
  :global(.toggle-switch input:checked + .toggle-slider) {
    background-color: #04d361;
  }
  :global(.toggle-switch input:checked + .toggle-slider:before) {
    transform: translateX(16px);
  }

  /* Select styling */
  :global(select.input-box) {
    appearance: none;
    background-image: url("data:image/svg+xml,%3csvg xmlns='http://www.w3.org/2000/svg' fill='none' viewBox='0 0 20 20'%3e%3cpath stroke='%236b7280' stroke-linecap='round' stroke-linejoin='round' stroke-width='1.5' d='M6 8l4 4 4-4'/%3e%3c/svg%3e");
    background-position: right 8px center;
    background-repeat: no-repeat;
    background-size: 16px 16px;
    padding-right: 28px;
  }

  /* ── Toast Notifications ───────────────────────────────────────────────── */

  :global(.toast-container) {
    position: fixed;
    bottom: 24px;
    right: 24px;
    z-index: 100;
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-width: 380px;
  }

  :global(.toast) {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 10px 14px;
    border-radius: 10px;
    border: 1px solid transparent;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.2);
    animation: toast-slide-in 0.3s ease-out;
    backdrop-filter: blur(12px);
  }

  :global(.toast-success) {
    background: rgba(4, 211, 97, 0.12);
    border-color: rgba(4, 211, 97, 0.3);
    color: #04d361;
  }
  :global(.toast-error) {
    background: rgba(247, 64, 64, 0.12);
    border-color: rgba(247, 64, 64, 0.3);
    color: #f74040;
  }
  :global(.toast-info) {
    background: rgba(75, 163, 255, 0.12);
    border-color: rgba(75, 163, 255, 0.3);
    color: #4ba3ff;
  }

  :global(.toast-close) {
    opacity: 0.6;
    padding: 2px;
    border-radius: 4px;
    transition: opacity 0.15s;
    cursor: pointer;
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    border: none;
    color: inherit;
  }
  :global(.toast-close:hover) {
    opacity: 1;
  }

  @keyframes toast-slide-in {
    from {
      opacity: 0;
      transform: translateX(40px) scale(0.95);
    }
    to {
      opacity: 1;
      transform: translateX(0) scale(1);
    }
  }
</style>
