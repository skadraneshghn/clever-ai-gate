<script>
  import { onMount } from 'svelte';
  import { 
    ChevronDown, ExternalLink, Globe, Search, Cpu, Paperclip, Mic, 
    Send, FileText, RefreshCw, HelpCircle, CheckCircle, XCircle,
    ChevronUp
  } from '@lucide/svelte';
  import { appState } from '$lib/state.svelte.js';
  import Button from '$lib/components/Button.svelte';
  import Card from '$lib/components/Card.svelte';
  import Input from '$lib/components/Input.svelte';

  let chatScrollElement = $state(null);
  let modelSearchQuery = $state('');

  // Derived filtered models list
  let filteredModels = $derived(
    appState.models.filter(m => m.id.toLowerCase().includes(modelSearchQuery.toLowerCase()))
  );

  // Automatically scroll chat container to bottom when messages list updates
  $effect(() => {
    if (appState.messages.length > 0 && chatScrollElement) {
      setTimeout(() => {
        if (chatScrollElement) {
          chatScrollElement.scrollTop = chatScrollElement.scrollHeight;
        }
      }, 16);
    }
  });

  onMount(() => {
    // If the models are not loaded yet and apiKey exists, load them
    if (appState.apiKey && appState.models.length === 0) {
      appState.loadModels();
    }
  });
</script>

<!-- Top header bar -->
<header class="header flex items-center justify-between px-6 py-4 border-b shrink-0">
  <div class="model-picker-container relative">
    <Button variant="secondary" size="sm" onclick={() => appState.handleModelPickerClick()} class="font-bold flex items-center gap-2">
      <span>{appState.selectedModel || 'Configure Gateway'}</span>
      <ChevronDown size={14} />
    </Button>
    
    {#if appState.showModelDropdown && appState.models.length > 0}
      <div class="model-dropdown animate-fade-in">
        <div class="model-dropdown-search">
          <Search size={14} class="opacity-60 text-secondary" />
          <input
            type="text"
            placeholder="Search models..."
            class="model-search-input"
            bind:value={modelSearchQuery}
            onclick={(e) => e.stopPropagation()}
            onkeydown={(e) => e.stopPropagation()}
          />
        </div>
        <div class="model-dropdown-list">
          {#each filteredModels as model}
            <button 
              class="model-option flex items-center w-full px-4 py-3 text-left text-xs {appState.selectedModel === model.id ? 'active' : ''}" 
              onclick={() => { appState.selectedModel = model.id; appState.showModelDropdown = false; modelSearchQuery = ''; }}
            >
              {model.id}
            </button>
          {:else}
            <div class="p-4 text-center text-xs opacity-60 text-secondary">No models found</div>
          {/each}
        </div>
      </div>
    {/if}
  </div>

  <div class="flex items-center gap-2">
    <Button variant="ghost" size="sm" onclick={() => appState.showCodePanel = !appState.showCodePanel} title="Toggle Integration Snippets">
      <ExternalLink size={16} />
    </Button>
    <Button variant="outline" size="sm" class="font-bold">Share</Button>
  </div>
</header>

<!-- Live telemetry HUD panel -->
<div class="telemetry-bar flex items-center gap-6 px-6 py-3 border-b text-xs font-semibold overflow-x-auto whitespace-nowrap shrink-0 bg-orange-light/10">
  <div class="hud-item">Status: <span class="hud-badge text-primary">{appState.statusHUD}</span></div>
  <div class="hud-item">Provider: <span class="hud-badge text-[#f97316] font-bold">{appState.providerHUD}</span></div>
  <div class="hud-item">Model: <span class="hud-badge text-[#f97316] font-bold">{appState.modelHUD}</span></div>
  <div class="hud-item">TTFT: <span class="hud-badge text-primary">{appState.ttftHUD}</span></div>
  <div class="hud-item">Latency: <span class="hud-badge text-primary">{appState.latencyHUD}</span></div>
  <div class="hud-item">Speed: <span class="hud-badge text-primary">{appState.speedHUD}</span></div>
</div>

<!-- Chat Scrollable Area -->
<div class="chat-scroll-area flex-grow overflow-y-auto" bind:this={chatScrollElement}>
  {#if appState.messages.length === 0}
    <!-- Initial landing screen layout -->
    <div class="landing-container flex flex-col items-center justify-center text-center px-6">
      
      <h1 class="text-3xl font-extrabold tracking-tight mb-8 text-primary">What's on your mind today?</h1>

      <!-- Prompt Card (Pill styled) -->
      <Card variant="filled" padding="md" class="prompt-pill-card mb-8">
        <textarea 
          class="prompt-textarea w-full text-base outline-none resize-none" 
          placeholder="Ask me anything..." 
          rows="3"
          bind:value={appState.inputText}
          onkeydown={(e) => { if(e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); appState.submitPrompt(); } }}
        ></textarea>
        
        <div class="flex items-center justify-between pt-3 border-t border-[var(--border-color)]">
          <div class="flex items-center gap-2">
            <button 
              class="deeper-btn flex items-center gap-1.5 text-xs font-bold uppercase px-3.5 py-1.5 rounded-full border {appState.isDeeperResearch ? 'active' : ''}" 
              onclick={() => appState.isDeeperResearch = !appState.isDeeperResearch}
            >
              <Globe size={13} />
              Deeper Research
            </button>
            <Button variant="ghost" size="sm" class="action-icon-btn"><Search size={16} /></Button>
            <Button variant="ghost" size="sm" class="action-icon-btn"><Cpu size={16} /></Button>
          </div>
          
          <div class="flex items-center gap-2">
            <Button variant="ghost" size="sm" class="action-icon-btn"><Paperclip size={16} /></Button>
            <Button variant="ghost" size="sm" class="action-icon-btn"><Mic size={16} /></Button>
            <button 
              class="send-circle-btn flex items-center justify-center rounded-full w-9 h-9 text-white bg-[#f97316]" 
              onclick={() => appState.submitPrompt()} 
              disabled={!appState.inputText.trim() || !appState.apiKey}
            >
              <Send size={15} />
            </button>
          </div>
        </div>
      </Card>

      <!-- Bottom Presets Row -->
      <div class="presets-container flex gap-3 justify-center flex-wrap max-w-3xl">
        <Button variant="outline" size="md" class="preset-pill rounded-full" onclick={() => appState.applyPreset("Summarize this article for me:")}>
          <FileText size={16} class="text-[#f97316]" />
          <span>Summarize Text</span>
        </Button>
        
        <Button variant="outline" size="md" class="preset-pill rounded-full" onclick={() => appState.applyPreset("Write a blog post outline on: ")}>
          <RefreshCw size={16} class="text-[#f97316]" />
          <span>Creative Writing</span>
        </Button>
        
        <Button variant="outline" size="md" class="preset-pill rounded-full" onclick={() => appState.applyPreset("Answer this complex question: ")}>
          <HelpCircle size={16} class="text-[#f97316]" />
          <span>Answer Questions</span>
        </Button>
      </div>
    </div>
  {:else}
    <!-- Chat flow display -->
    <div class="chat-content-container">
      {#each appState.messages as msg}
        <div class="message-bubble {msg.role === 'user' ? 'user' : 'assistant'} flex flex-col gap-2">
          <div class="text-xs font-bold uppercase tracking-wider text-secondary">{msg.role === 'user' ? 'You' : 'Assistant'}</div>
          
          <Card variant="filled" padding="md" class="bubble-content max-w-full">
            {#if msg.reasoning_content}
              <div class="reasoning-container p-4 rounded-xl border-l-2 mb-4">
                <div class="text-xs font-bold text-orange-500 uppercase tracking-wider mb-2">🧠 Thinking Process</div>
                <div class="text-sm italic leading-relaxed whitespace-pre-wrap">{msg.reasoning_content}</div>
              </div>
            {/if}
            <div class="leading-relaxed whitespace-pre-wrap text-base">{msg.content || (appState.isSending && !msg.reasoning_content ? 'Connecting...' : '')}</div>
          </Card>
        </div>
      {/each}
    </div>
  {/if}
</div>

<!-- Floating bottom input bar -->
{#if appState.messages.length > 0}
  <div class="bottom-input-container">
    <Card variant="filled" padding="md" class="prompt-pill-card">
      <textarea 
        class="prompt-textarea w-full text-base outline-none resize-none" 
        placeholder="Ask me anything..." 
        rows="1"
        bind:value={appState.inputText}
        onkeydown={(e) => { if(e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); appState.submitPrompt(); } }}
      ></textarea>
      <div class="flex items-center justify-between pt-3 border-t border-[var(--border-color)]">
        <div class="flex items-center gap-2">
          <Button variant="ghost" size="sm" class="action-icon-btn"><Paperclip size={16} /></Button>
          <Button variant="ghost" size="sm" class="action-icon-btn"><Mic size={16} /></Button>
        </div>
        <button 
          class="send-circle-btn flex items-center justify-center rounded-full w-9 h-9 text-white bg-[#f97316]" 
          onclick={() => appState.submitPrompt()} 
          disabled={!appState.inputText.trim() || appState.isSending}
        >
          <Send size={15} />
        </button>
      </div>
    </Card>
    <div class="footer-disclaimer text-xs opacity-60 mt-3 text-center text-secondary">
      Cognivo can make mistakes. Check important info.
    </div>
  </div>
{:else}
  <!-- Simple small footer when on landing screen -->
  <footer class="footer text-center py-4 text-xs border-t shrink-0 text-secondary">
    Cognivo can make mistakes. Check important info. See Cookie Preferences.
  </footer>
{/if}

<style>
  /* Core Chat Layouts */
  .landing-container {
    max-width: 800px;
    width: 100%;
    margin: 0 auto;
    padding: 80px 20px 40px 20px;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    min-height: calc(100vh - 160px);
    box-sizing: border-box;
  }

  :global(.prompt-pill-card) {
    width: 100%;
    max-width: 800px;
    border-radius: 24px !important;
    background-color: var(--card-bg) !important;
    border: 1px solid var(--border-color) !important;
    box-shadow: 0 10px 40px var(--shadow-color) !important;
    transition: border-color 0.25s ease, box-shadow 0.25s ease;
    padding: 18px 24px !important;
    box-sizing: border-box;
  }

  :global(.prompt-pill-card:focus-within) {
    border-color: #f97316 !important;
    box-shadow: 0 10px 40px rgba(249, 115, 22, 0.08), 0 0 0 3px rgba(249, 115, 22, 0.12) !important;
  }

  .prompt-textarea {
    width: 100%;
    border: none;
    background: transparent;
    resize: none;
    font-family: inherit;
    font-size: 15px;
    color: var(--text-primary);
    line-height: 1.6;
    outline: none;
    padding: 0;
    margin: 0;
  }
  .prompt-textarea::placeholder {
    color: var(--text-secondary);
    opacity: 0.5;
  }

  .presets-container {
    width: 100%;
    max-width: 800px;
    display: flex;
    gap: 12px;
    justify-content: center;
    flex-wrap: wrap;
    margin-top: 24px;
  }

  :global(.preset-pill) {
    border-radius: 100px !important;
    font-size: 13px !important;
    font-weight: 500 !important;
    background-color: var(--card-bg) !important;
    border: 1px solid var(--border-color) !important;
    color: var(--text-secondary) !important;
    transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1) !important;
  }

  :global(.preset-pill:hover) {
    border-color: #f97316 !important;
    color: #f97316 !important;
    background-color: rgba(249, 115, 22, 0.04) !important;
    transform: translateY(-1px);
  }

  /* Chat history feed wrapper */
  .chat-content-container {
    max-width: 800px;
    width: 100%;
    margin: 0 auto;
    padding: 32px 20px 160px 20px; /* Space for bottom floating input */
    display: flex;
    flex-direction: column;
    gap: 32px;
    box-sizing: border-box;
  }

  .message-bubble {
    width: 100%;
    display: flex;
    flex-direction: column;
  }

  .message-bubble.user {
    align-items: flex-end;
  }

  .message-bubble.assistant {
    align-items: flex-start;
  }

  :global(.bubble-content) {
    max-width: 85% !important;
    border-radius: 20px !important;
    line-height: 1.6;
    font-size: 15px;
    box-shadow: 0 4px 20px var(--shadow-color) !important;
    border: 1px solid var(--border-color) !important;
  }

  .message-bubble.user :global(.bubble-content) {
    background-color: var(--frame-bg) !important;
    border-bottom-right-radius: 4px !important;
  }

  .message-bubble.assistant :global(.bubble-content) {
    background-color: var(--card-bg) !important;
    border-bottom-left-radius: 4px !important;
  }

  /* Floating input bar at bottom */
  .bottom-input-container {
    position: absolute;
    bottom: 0;
    left: 0;
    right: 0;
    padding: 24px 20px;
    background: linear-gradient(180deg, rgba(255, 255, 255, 0) 0%, var(--main-bg) 60%);
    display: flex;
    flex-direction: column;
    align-items: center;
    z-index: 10;
    box-sizing: border-box;
  }

  :global(.dark) .bottom-input-container {
    background: linear-gradient(180deg, rgba(15, 15, 18, 0) 0%, var(--main-bg) 60%);
  }

  /* HUD value tags styling */
  .hud-badge {
    background-color: var(--frame-bg);
    border: 1px solid var(--border-color);
    padding: 2px 8px;
    border-radius: 6px;
    margin-left: 4px;
    display: inline-block;
  }

  .model-dropdown {
    position: absolute;
    top: 100%;
    left: 0;
    margin-top: 8px;
    border: 1px solid var(--border-color);
    border-radius: 12px;
    box-shadow: 0 10px 30px var(--shadow-color);
    z-index: 50;
    width: 320px;
    background-color: var(--card-bg);
    overflow: hidden;
    display: flex;
    flex-direction: column;
    max-height: 320px;
  }

  .model-dropdown-search {
    padding: 10px;
    border-bottom: 1px solid var(--border-color);
    display: flex;
    align-items: center;
    gap: 8px;
    background-color: rgba(107, 114, 128, 0.05);
    flex-shrink: 0;
  }

  .model-search-input {
    width: 100%;
    font-size: 14px;
    background: transparent;
    border: none;
    outline: none;
    color: var(--text-primary);
  }

  .model-dropdown-list {
    overflow-y: auto;
    flex-grow: 1;
    max-height: 240px;
  }

  /* Model picker items overrides */
  .model-option {
    color: var(--text-primary);
    background: transparent;
    border: none;
    border-bottom: 1px solid var(--border-color);
    transition: background-color 0.15s;
    cursor: pointer;
  }
  .model-option:last-child {
    border-bottom: none;
  }
  .model-option:hover {
    background-color: var(--item-hover);
  }
  .model-option.active {
    color: #f97316;
    background-color: rgba(249, 115, 22, 0.08);
    font-weight: 700;
  }

  .deeper-btn {
    color: var(--text-secondary);
    border: 1.5px solid var(--border-color);
    transition: all 0.2s;
    background: transparent;
    cursor: pointer;
    border-radius: 100px;
  }
  .deeper-btn:hover {
    color: var(--text-primary);
    border-color: var(--text-secondary);
  }
  .deeper-btn.active {
    color: #f97316;
    border-color: #f97316;
    background: rgba(249, 115, 22, 0.06);
  }

  .send-circle-btn {
    border: none;
    cursor: pointer;
    transition: all 0.2s;
  }
  .send-circle-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .send-circle-btn:hover:not(:disabled) {
    background-color: #ea580c;
    transform: scale(1.05);
  }

  .reasoning-container {
    background-color: rgba(249, 115, 22, 0.04);
    border-left: 3px solid rgba(249, 115, 22, 0.5);
  }
</style>
