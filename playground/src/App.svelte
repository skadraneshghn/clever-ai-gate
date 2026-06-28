<script>
  import { onMount } from 'svelte';
  import { 
    Globe, BookOpen, FileText, Settings, Compass, Search, HelpCircle, 
    Send, Plus, Trash2, Sparkles, User, Sun, Moon, Cpu, Paperclip, Mic, 
    ExternalLink, Check, Copy, ChevronDown, RefreshCw, LogIn
  } from '@lucide/svelte';

  // State (using Svelte 5 Runes)
  let theme = $state('light'); // Default to light as in the first screenshot
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

  // Dynamic templates values
  let showCodePanel = $state(false);
  let activeCodeTab = $state('curl');

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

  // Load configuration from localStorage on mount
  onMount(async () => {
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

  // Load tenant details from GET /api/v1/playground/tenant. Returns true if key is valid.
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

  // Format large numbers for token display (e.g. 1000000000 -> 1.0B)
  function formatBalance(num) {
    if (num >= 1e9) {
      return (num / 1e9).toFixed(1) + 'B';
    }
    if (num >= 1e6) {
      return (num / 1e6).toFixed(1) + 'M';
    }
    if (num >= 1e3) {
      return (num / 1e3).toFixed(1) + 'K';
    }
    return num.toString();
  }

  // Load active models from the Go backend GET /v1/models route
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

  // Load chat history from the Go backend GET /api/v1/playground/chats route
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

  // Set active conversation session
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

  // Create a new empty conversation session in database
  async function startNewChat() {
    currentChatId = null;
    messages = [];
    inputText = '';
  }

  // Delete chat session
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

  // Trigger prompt presets
  function applyPreset(text) {
    inputText = text;
  }

  // Main Submit Chat request (Handles streaming & parsing)
  async function submitPrompt() {
    if (!inputText.trim() || isSending) return;
    
    const userMsg = { role: 'user', content: inputText };
    messages = [...messages, userMsg];
    const originalInput = inputText;
    inputText = '';
    isSending = true;

    // HUD reset
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
    
    // Create new assistant placeholder
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

      // Read response headers for metadata HUD
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
        buffer = lines.pop(); // Keep partial line in buffer

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
              // Parse error wrapper
            }
          }
        }

        // Live calculation speed
        const elapsed = (performance.now() - startTime) / 1000;
        if (elapsed > 0) {
          speedHUD = `${Math.round(tokenCount / elapsed)} tok/s`;
        }
      }

      const totalElapsed = performance.now() - startTime;
      latencyHUD = `${Math.round(totalElapsed)}ms`;
      statusHUD = 'Done';

      // Save/Persist conversation in database
      await saveConversation(originalInput);

    } catch (err) {
      statusHUD = 'Connection Failed';
      messages[assistantIndex].content = `Connection failed: ${err.message}`;
    } finally {
      isSending = false;
    }
  }

  // Database synchronisation helper (saves the conversation log)
  async function saveConversation(firstPrompt) {
    const title = messages[0].content.substring(0, 35) + (messages[0].content.length > 35 ? '...' : '');
    
    try {
      if (currentChatId) {
        // Update existing chat
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
        // Create new chat
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
            <svg class="w-7 h-7 text-[#f97316]" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3">
              <path stroke-linecap="round" stroke-linejoin="round" d="M4 6h16M4 12h16m-7 6h7" />
            </svg>
            <span class="font-bold text-lg tracking-tight select-none">Cognivo</span>
          </div>
          <button class="icon-button" onclick={() => applyTheme(theme === 'dark' ? 'light' : 'dark')}>
            {#if theme === 'dark'}
              <Sun size={16} />
            {:else}
              <Moon size={16} />
            {/if}
          </button>
        </div>

        <!-- New Chat Button -->
        <button class="new-chat-btn flex items-center justify-between w-full p-3 rounded-lg text-white font-medium" onclick={startNewChat}>
          <span class="flex items-center gap-2">
            <Plus size={18} />
            New Chat
          </span>
          <span class="btn-shortcut">⌘ N</span>
        </button>

        <!-- Grouped Scrollable History -->
        <div class="history-section flex flex-col gap-4 overflow-y-auto pr-1">
          {#if sidebarChats.today.length > 0}
            <div>
              <div class="history-label text-[10px] uppercase font-bold tracking-wider mb-2">Today</div>
              <div class="flex flex-col gap-1">
                {#each sidebarChats.today as chat}
                  <button class="history-item flex items-center justify-between p-2 rounded-md w-full text-left {currentChatId === chat.id ? 'active' : ''}" onclick={() => selectChat(chat.id)}>
                    <span class="truncate pr-2">{chat.title}</span>
                    <Trash2 size={13} class="trash-icon hover:text-red-500" onclick={(e) => deleteChat(chat.id, e)} />
                  </button>
                {/each}
              </div>
            </div>
          {/if}

          {#if sidebarChats.yesterday.length > 0}
            <div>
              <div class="history-label text-[10px] uppercase font-bold tracking-wider mb-2">Yesterday</div>
              <div class="flex flex-col gap-1">
                {#each sidebarChats.yesterday as chat}
                  <button class="history-item flex items-center justify-between p-2 rounded-md w-full text-left {currentChatId === chat.id ? 'active' : ''}" onclick={() => selectChat(chat.id)}>
                    <span class="truncate pr-2">{chat.title}</span>
                    <Trash2 size={13} class="trash-icon hover:text-red-500" onclick={(e) => deleteChat(chat.id, e)} />
                  </button>
                {/each}
              </div>
            </div>
          {/if}

          {#if sidebarChats.older.length > 0}
            <div>
              <div class="history-label text-[10px] uppercase font-bold tracking-wider mb-2">Older</div>
              <div class="flex flex-col gap-1">
                {#each sidebarChats.older as chat}
                  <button class="history-item flex items-center justify-between p-2 rounded-md w-full text-left {currentChatId === chat.id ? 'active' : ''}" onclick={() => selectChat(chat.id)}>
                    <span class="truncate pr-2">{chat.title}</span>
                    <Trash2 size={13} class="trash-icon hover:text-red-500" onclick={(e) => deleteChat(chat.id, e)} />
                  </button>
                {/each}
              </div>
            </div>
          {/if}
        </div>
      </div>

      <!-- Bottom sidebar info -->
      <div class="flex flex-col gap-2 border-t pt-4">
        <button class="nav-link flex items-center gap-3 w-full p-2-5 rounded-lg text-left" onclick={() => showSettingsModal = true}>
          <Settings size={18} />
          <span>Settings</span>
        </button>

        <!-- User profile panel -->
        <div class="profile-card flex items-center justify-between p-2-5 rounded-lg border">
          <div class="flex items-center gap-2 overflow-hidden">
            <div class="avatar flex items-center justify-center w-8 h-8 rounded-full text-white bg-gradient-to-tr from-orange to-pink font-bold text-xs shrink-0">
              {tenantInitials}
            </div>
            <div class="flex flex-col overflow-hidden">
              <span class="font-bold text-xs truncate">{tenantName || 'Not Connected'}</span>
              <span class="text-[9px] text-[#f97316] font-semibold uppercase">
                {#if tenantRateLimit}
                  Limit: {tenantRateLimit} RPM
                {:else}
                  No Limit
                {/if}
              </span>
              {#if tenantBalance}
                <span class="text-[8px] opacity-75">Bal: {formatBalance(tenantBalance)} tokens</span>
              {/if}
            </div>
          </div>
          <button class="icon-button" onclick={() => showSettingsModal = true} title="Configure Key">
            <Settings size={14} />
          </button>
        </div>
      </div>
    </aside>

    <!-- Main Workspace -->
    <main class="main-panel flex flex-col flex-grow overflow-hidden">
      <!-- Top header bar -->
      <header class="header flex items-center justify-between px-6 py-3 border-b">
        <div class="model-picker-container relative">
          <button class="model-picker-btn flex items-center gap-2 font-semibold text-sm" onclick={() => showModelDropdown = !showModelDropdown}>
            <span>{selectedModel || 'Configure Gateway'}</span>
            <ChevronDown size={14} />
          </button>
          
          {#if showModelDropdown && models.length > 0}
            <div class="model-dropdown absolute top-full left-0 mt-1 border rounded-lg shadow-xl z-20 w-56">
              {#each models as model}
                <button class="model-option flex items-center w-full px-4 py-2-5 text-left text-xs {selectedModel === model.id ? 'active' : ''}" onclick={() => { selectedModel = model.id; showModelDropdown = false; }}>
                  {model.id}
                </button>
              {/each}
            </div>
          {/if}
        </div>

        <div class="flex items-center gap-2">
          <button class="icon-button" onclick={() => showCodePanel = !showCodePanel} title="Toggle Integration Snippets">
            <ExternalLink size={16} />
          </button>
          <button class="share-btn text-xs font-semibold px-3 py-1-5 rounded-lg border">Share</button>
        </div>
      </header>

      <!-- Live telemetry HUD panel -->
      <div class="telemetry-bar flex gap-6 px-6 py-2.5 border-b font-mono text-[10px] overflow-x-auto whitespace-nowrap">
        <div>Status: <span class="hud-value">{statusHUD}</span></div>
        <div>Provider: <span class="hud-value text-[#f97316]">{providerHUD}</span></div>
        <div>Model: <span class="hud-value text-[#f97316]">{modelHUD}</span></div>
        <div>TTFT: <span class="hud-value">{ttftHUD}</span></div>
        <div>Latency: <span class="hud-value">{latencyHUD}</span></div>
        <div>Speed: <span class="hud-value">{speedHUD}</span></div>
      </div>
      <!-- Chat Workspace Area -->
      <div class="chat-container flex-grow overflow-y-auto p-6 flex flex-col">
        {#if messages.length === 0}
          <!-- Initial landing screen layout -->
          <div class="flex-grow flex flex-col items-center justify-center gap-8 max-w-3xl mx-auto w-full text-center">
            
            <!-- Large central Cognivo orange SVG logo -->
            <div class="center-logo flex items-center justify-center p-5 rounded-2xl bg-orange-light">
              <svg class="w-16 h-16 text-[#f97316]" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                <path stroke-linecap="round" stroke-linejoin="round" d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z" />
              </svg>
            </div>

            <h1 class="text-3xl font-extrabold tracking-tight">Let's start a smart conversation</h1>

            <!-- Bounded input form box -->
            <div class="input-card flex flex-col w-full border rounded-xl p-3 shadow-md gap-3">
              <textarea 
                class="prompt-textarea w-full text-sm outline-none resize-none" 
                placeholder="Ask me anything..." 
                rows="2"
                bind:value={inputText}
                onkeydown={(e) => { if(e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); submitPrompt(); } }}
              ></textarea>
              
              <div class="flex items-center justify-between pt-2 border-t">
                <div class="flex items-center gap-2">
                  <button class="deeper-btn flex items-center gap-1 text-[10px] font-bold uppercase px-2-5 py-1-5 rounded-lg border {isDeeperResearch ? 'active' : ''}" onclick={() => isDeeperResearch = !isDeeperResearch}>
                    <Globe size={11} />
                    Deeper Research
                  </button>
                  <button class="action-icon-btn"><Search size={14} /></button>
                  <button class="action-icon-btn"><Cpu size={14} /></button>
                </div>
                
                <div class="flex items-center gap-2">
                  <button class="action-icon-btn"><Paperclip size={14} /></button>
                  <button class="action-icon-btn"><Mic size={14} /></button>
                  <button class="send-circle-btn flex items-center justify-center rounded-full w-8 h-8 text-white bg-[#f97316]" onclick={submitPrompt} disabled={!inputText.trim() || !apiKey}>
                    <Send size={14} />
                  </button>
                </div>
              </div>
            </div>

            <!-- Bottom Cards Row -->
            <div class="grid grid-cols-3 gap-4 w-full">
              <button class="preset-card flex flex-col text-left p-4 rounded-xl border gap-2" onclick={() => applyPreset("Summarize this article for me:")}>
                <FileText size={16} class="text-[#f97316]" />
                <div class="font-bold text-xs">Summarize Text</div>
                <div class="text-[10px] leading-relaxed">Turn long articles into easy summaries.</div>
              </button>
              
              <button class="preset-card flex flex-col text-left p-4 rounded-xl border gap-2" onclick={() => applyPreset("Write a blog post outline on: ")}>
                <RefreshCw size={16} class="text-[#f97316]" />
                <div class="font-bold text-xs">Creative Writing</div>
                <div class="text-[10px] leading-relaxed">Generate stories, blog posts, or ideas in seconds.</div>
              </button>
              
              <button class="preset-card flex flex-col text-left p-4 rounded-xl border gap-2" onclick={() => applyPreset("Answer this complex question: ")}>
                <HelpCircle size={16} class="text-[#f97316]" />
                <div class="font-bold text-xs">Answer Questions</div>
                <div class="text-[10px] leading-relaxed">Ask anything—from facts to advice—and get answers.</div>
              </button>
            </div>
          </div>
        {:else}
          <!-- Chat flow display -->
          <div class="flex flex-col gap-6 max-w-4xl mx-auto w-full flex-grow">
            {#each messages as msg}
              <div class="message-bubble flex flex-col gap-2 {msg.role === 'user' ? 'align-end' : ''}">
                <div class="text-[10px] font-bold uppercase tracking-wider text-secondary">{msg.role === 'user' ? 'You' : 'Assistant'}</div>
                
                <div class="bubble-content p-4 rounded-xl border text-sm max-w-full">
                  {#if msg.reasoning_content}
                    <div class="reasoning-container p-3 rounded-lg border-l-2 mb-3">
                      <div class="text-[10px] font-bold text-orange-500 uppercase tracking-wider mb-2">🧠 Thinking Process</div>
                      <div class="text-xs italic leading-relaxed whitespace-pre-wrap">{msg.reasoning_content}</div>
                    </div>
                  {/if}
                  <div class="leading-relaxed whitespace-pre-wrap">{msg.content || (isSending && !msg.reasoning_content ? 'Connecting...' : '')}</div>
                </div>
              </div>
            {/each}
          </div>
          
          <!-- Bottom input bar for follow-ups -->
          <div class="bottom-input-bar max-w-4xl mx-auto w-full pt-4 mt-auto">
            <div class="input-card flex flex-col w-full border rounded-xl p-3 shadow-md gap-3">
              <textarea 
                class="prompt-textarea w-full text-sm outline-none resize-none" 
                placeholder="Ask me anything..." 
                rows="1"
                bind:value={inputText}
                onkeydown={(e) => { if(e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); submitPrompt(); } }}
              ></textarea>
              <div class="flex items-center justify-between pt-2 border-t">
                <div class="flex items-center gap-2">
                  <button class="action-icon-btn"><Paperclip size={14} /></button>
                  <button class="action-icon-btn"><Mic size={14} /></button>
                </div>
                <button class="send-circle-btn flex items-center justify-center rounded-full w-8 h-8 text-white bg-[#f97316]" onclick={submitPrompt} disabled={!inputText.trim()}>
                  <Send size={14} />
                </button>
              </div>
            </div>
          </div>
        {/if}
      </div>

      <!-- Footer disclaimer -->
      <footer class="footer text-center py-3 text-[10px] border-t">
        Cognivo can make mistakes. Check important info. See Cookie Preferences.
      </footer>
    </main>

    <!-- Side Code Panel (collapsible) -->
    {#if showCodePanel}
      <aside class="code-panel flex flex-col w-code border-l">
        <header class="flex items-center justify-between px-4 py-3 border-b bg-gray-light">
          <span class="text-xs font-bold uppercase tracking-wider">Integrations</span>
          <button class="text-xs text-orange-500 font-semibold" onclick={() => showCodePanel = false}>Close</button>
        </header>
        
        <div class="flex border-b text-[10px]">
          <button class="tab-btn px-3 py-2 flex-grow {activeCodeTab === 'curl' ? 'active' : ''}" onclick={() => activeCodeTab = 'curl'}>cURL</button>
          <button class="tab-btn px-3 py-2 flex-grow {activeCodeTab === 'js' ? 'active' : ''}" onclick={() => activeCodeTab = 'js'}>JS</button>
          <button class="tab-btn px-3 py-2 flex-grow {activeCodeTab === 'python' ? 'active' : ''}" onclick={() => activeCodeTab = 'python'}>Python</button>
        </div>

        <div class="p-4 flex-grow overflow-auto font-mono text-[10px] bg-black-light leading-relaxed whitespace-pre-wrap select-text">
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

<!-- Settings Key Config Modal -->
{#if showSettingsModal || !apiKey}
  <div class="modal-backdrop fixed inset-0 flex items-center justify-center p-4 z-50 bg-black-trans backdrop-blur-sm">
    <div class="modal-content w-full max-w-sm rounded-xl border p-6 shadow-2xl relative">
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
    </div>
  </div>
{/if}

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

  .app-wrapper {
    background-color: var(--bg-color);
  }

  .cognivo-frame {
    background-color: var(--frame-bg);
    border-color: var(--border-color);
    box-shadow: none;
    height: 100%;
    width: 100%;
  }

  /* Sidebar styling */
  .sidebar {
    background-color: var(--sidebar-bg);
    border-color: var(--border-color);
  }

  .new-chat-btn {
    background-color: #f97316;
    box-shadow: 0 4px 12px rgba(249, 115, 22, 0.25);
    transition: transform 0.1s, opacity 0.2s;
  }
  .new-chat-btn:hover {
    opacity: 0.95;
    transform: translateY(-0.5px);
  }
  .btn-shortcut {
    font-size: 8px;
    background-color: rgba(255, 255, 255, 0.2);
    padding: 2px 4px;
    border-radius: 4px;
    font-weight: bold;
  }

  .nav-link {
    color: var(--text-secondary);
    font-size: 12px;
    font-weight: 500;
    transition: background-color 0.15s, color 0.15s;
  }
  .nav-link:hover {
    background-color: var(--item-hover);
    color: var(--text-primary);
  }

  .history-label {
    color: var(--text-secondary);
    opacity: 0.6;
  }

  .history-item {
    color: var(--text-secondary);
    font-size: 11px;
    transition: all 0.15s;
  }
  .history-item:hover {
    background-color: var(--item-hover);
    color: var(--text-primary);
  }
  .history-item.active {
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

  .profile-card {
    background-color: var(--frame-bg);
    border-color: var(--border-color);
  }

  /* Main Workspace styling */
  .main-panel {
    background-color: var(--main-bg);
  }
  .header, .telemetry-bar, .footer {
    border-color: var(--border-color);
  }

  .model-picker-btn {
    color: var(--text-primary);
    background: var(--item-hover);
    padding: 6px 12px;
    border-radius: 20px;
    transition: opacity 0.15s;
  }
  .model-picker-btn:hover {
    opacity: 0.9;
  }

  .model-dropdown {
    background-color: var(--card-bg);
    border-color: var(--border-color);
  }
  .model-option {
    color: var(--text-primary);
    transition: background-color 0.15s;
  }
  .model-option:hover {
    background-color: var(--item-hover);
  }
  .model-option.active {
    color: #f97316;
    background-color: rgba(249, 115, 22, 0.06);
    font-weight: bold;
  }

  .hud-value {
    color: var(--text-primary);
    font-weight: 600;
  }

  /* Center UI layout elements */

  .input-card {
    background-color: var(--card-bg);
    border-color: var(--border-color);
  }
  .prompt-textarea {
    background: transparent;
    color: var(--text-primary);
  }

  .action-icon-btn {
    color: var(--text-secondary);
    padding: 6px;
    border-radius: 6px;
    transition: all 0.15s;
  }
  .action-icon-btn:hover {
    color: var(--text-primary);
    background: var(--item-hover);
  }

  .deeper-btn {
    color: var(--text-secondary);
    border-color: var(--border-color);
    transition: all 0.2s;
  }
  .deeper-btn:hover {
    color: var(--text-primary);
  }
  .deeper-btn.active {
    color: #f97316;
    border-color: #f97316;
    background: rgba(249, 115, 22, 0.05);
  }


  .preset-card {
    background-color: var(--card-bg);
    border-color: var(--border-color);
    transition: all 0.2s;
  }
  .preset-card:hover {
    border-color: #f97316;
    transform: translateY(-0.5px);
  }

  /* Chat Bubble flow elements */
  .bubble-content {
    background-color: var(--card-bg);
    border-color: var(--border-color);
    color: var(--text-primary);
  }
  .reasoning-container {
    background-color: rgba(249, 115, 22, 0.03);
    border-left-color: rgba(249, 115, 22, 0.3);
  }

  /* Sidebar Code Panel */
  .code-panel {
    background-color: var(--sidebar-bg);
    border-color: var(--border-color);
  }
  .tab-btn {
    border: none;
    color: var(--text-secondary);
    background: transparent;
    transition: all 0.2s;
    font-weight: 500;
  }
  .tab-btn.active {
    color: #f97316;
    border-bottom: 2px solid #f97316;
  }

  /* Modal Settings */
  .modal-content {
    background-color: var(--card-bg);
    border-color: var(--border-color);
    color: var(--text-primary);
  }
  .input-box {
    background-color: var(--frame-bg);
    border-color: var(--border-color);
    color: var(--text-primary);
  }

  /* Shared Utility icons button style */
  .icon-button {
    color: var(--text-secondary);
    padding: 6px;
    border-radius: 6px;
    transition: all 0.15s;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .icon-button:hover {
    color: var(--text-primary);
    background: var(--item-hover);
  }

  @keyframes spin {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }
  .animate-spin {
    animation: spin 1s linear infinite;
    display: inline-block;
  }
</style>
