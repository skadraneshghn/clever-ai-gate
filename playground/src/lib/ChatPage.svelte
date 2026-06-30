<script>
  import { 
    ChevronDown, ExternalLink, Globe, Search, Cpu, Paperclip, Mic, 
    Send, FileText, RefreshCw, HelpCircle 
  } from '@lucide/svelte';
  import Button from './components/Button.svelte';
  import Card from './components/Card.svelte';
  import Input from './components/Input.svelte';

  let {
    apiKey,
    models = [],
    selectedModel = $bindable(''),
    showModelDropdown = $bindable(false),
    showCodePanel = $bindable(false),
    activeCodeTab = $bindable('curl'),
    messages = $bindable([]),
    inputText = $bindable(''),
    isDeeperResearch = $bindable(false),
    isSending = $bindable(false),
    handleModelPickerClick,
    submitPrompt,
    applyPreset,
    statusHUD,
    providerHUD,
    modelHUD,
    ttftHUD,
    latencyHUD,
    speedHUD
  } = $props();

  let chatScrollElement = $state(null);

  // Automatically scroll chat container to bottom when messages list updates
  $effect(() => {
    if (messages.length > 0 && chatScrollElement) {
      setTimeout(() => {
        if (chatScrollElement) {
          chatScrollElement.scrollTop = chatScrollElement.scrollHeight;
        }
      }, 16);
    }
  });
</script>

<!-- Top header bar -->
<header class="header flex items-center justify-between px-6 py-4 border-b shrink-0">
  <div class="model-picker-container relative">
    <Button variant="secondary" size="sm" onclick={handleModelPickerClick} class="font-bold flex items-center gap-2">
      <span>{selectedModel || 'Configure Gateway'}</span>
      <ChevronDown size={14} />
    </Button>
    
    {#if showModelDropdown && models.length > 0}
      <div class="model-dropdown absolute top-full left-0 mt-2 border rounded-xl shadow-2xl z-20 w-64 bg-[var(--card-bg)] border-[var(--border-color)] overflow-hidden">
        {#each models as model}
          <button 
            class="model-option flex items-center w-full px-4 py-3 text-left text-xs {selectedModel === model.id ? 'active' : ''}" 
            onclick={() => { selectedModel = model.id; showModelDropdown = false; }}
          >
            {model.id}
          </button>
        {/each}
      </div>
    {/if}
  </div>

  <div class="flex items-center gap-2">
    <Button variant="ghost" size="sm" onclick={() => showCodePanel = !showCodePanel} title="Toggle Integration Snippets">
      <ExternalLink size={16} />
    </Button>
    <Button variant="outline" size="sm" class="font-bold">Share</Button>
  </div>
</header>

<!-- Live telemetry HUD panel - Upgraded to a sleek flexbar with prominent badges -->
<div class="telemetry-bar flex items-center gap-6 px-6 py-3 border-b text-xs font-semibold overflow-x-auto whitespace-nowrap shrink-0 bg-orange-light/10">
  <div class="hud-item">Status: <span class="hud-badge text-primary">{statusHUD}</span></div>
  <div class="hud-item">Provider: <span class="hud-badge text-[#f97316] font-bold">{providerHUD}</span></div>
  <div class="hud-item">Model: <span class="hud-badge text-[#f97316] font-bold">{modelHUD}</span></div>
  <div class="hud-item">TTFT: <span class="hud-badge text-primary">{ttftHUD}</span></div>
  <div class="hud-item">Latency: <span class="hud-badge text-primary">{latencyHUD}</span></div>
  <div class="hud-item">Speed: <span class="hud-badge text-primary">{speedHUD}</span></div>
</div>

<!-- Chat Scrollable Area -->
<div class="chat-scroll-area flex-grow overflow-y-auto" bind:this={chatScrollElement}>
  {#if messages.length === 0}
    <!-- Initial landing screen layout -->
    <div class="landing-container flex flex-col items-center justify-center text-center px-6">
      
      <h1 class="text-3xl font-extrabold tracking-tight mb-8 text-primary">What's on your mind today?</h1>

      <!-- Prompt Card (Pill styled) -->
      <Card variant="filled" padding="md" class="prompt-pill-card mb-8">
        <textarea 
          class="prompt-textarea w-full text-base outline-none resize-none" 
          placeholder="Ask me anything..." 
          rows="3"
          bind:value={inputText}
          onkeydown={(e) => { if(e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); submitPrompt(); } }}
        ></textarea>
        
        <div class="flex items-center justify-between pt-3 border-t border-[var(--border-color)]">
          <div class="flex items-center gap-2">
            <button 
              class="deeper-btn flex items-center gap-1.5 text-xs font-bold uppercase px-3.5 py-1.5 rounded-full border {isDeeperResearch ? 'active' : ''}" 
              onclick={() => isDeeperResearch = !isDeeperResearch}
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
              onclick={submitPrompt} 
              disabled={!inputText.trim() || !apiKey}
            >
              <Send size={15} />
            </button>
          </div>
        </div>
      </Card>

      <!-- Bottom Presets Row -->
      <div class="presets-container flex gap-3 justify-center flex-wrap max-w-3xl">
        <Button variant="outline" size="md" class="preset-pill rounded-full" onclick={() => applyPreset("Summarize this article for me:")}>
          <FileText size={16} class="text-[#f97316]" />
          <span>Summarize Text</span>
        </Button>
        
        <Button variant="outline" size="md" class="preset-pill rounded-full" onclick={() => applyPreset("Write a blog post outline on: ")}>
          <RefreshCw size={16} class="text-[#f97316]" />
          <span>Creative Writing</span>
        </Button>
        
        <Button variant="outline" size="md" class="preset-pill rounded-full" onclick={() => applyPreset("Answer this complex question: ")}>
          <HelpCircle size={16} class="text-[#f97316]" />
          <span>Answer Questions</span>
        </Button>
      </div>
    </div>
  {:else}
    <!-- Chat flow display -->
    <div class="chat-content-container">
      {#each messages as msg}
        <div class="message-bubble flex flex-col gap-2 {msg.role === 'user' ? 'align-end' : ''}">
          <div class="text-xs font-bold uppercase tracking-wider text-secondary">{msg.role === 'user' ? 'You' : 'Assistant'}</div>
          
          <Card variant="filled" padding="md" class="bubble-content max-w-full">
            {#if msg.reasoning_content}
              <div class="reasoning-container p-4 rounded-xl border-l-2 mb-4">
                <div class="text-xs font-bold text-orange-500 uppercase tracking-wider mb-2">🧠 Thinking Process</div>
                <div class="text-sm italic leading-relaxed whitespace-pre-wrap">{msg.reasoning_content}</div>
              </div>
            {/if}
            <div class="leading-relaxed whitespace-pre-wrap text-base">{msg.content || (isSending && !msg.reasoning_content ? 'Connecting...' : '')}</div>
          </Card>
        </div>
      {/each}
    </div>
  {/if}
</div>

<!-- Floating bottom input bar (Fixed overlay at bottom) -->
{#if messages.length > 0}
  <div class="bottom-input-container">
    <Card variant="filled" padding="md" class="prompt-pill-card">
      <textarea 
        class="prompt-textarea w-full text-base outline-none resize-none" 
        placeholder="Ask me anything..." 
        rows="1"
        bind:value={inputText}
        onkeydown={(e) => { if(e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); submitPrompt(); } }}
      ></textarea>
      <div class="flex items-center justify-between pt-3 border-t border-[var(--border-color)]">
        <div class="flex items-center gap-2">
          <Button variant="ghost" size="sm" class="action-icon-btn"><Paperclip size={16} /></Button>
          <Button variant="ghost" size="sm" class="action-icon-btn"><Mic size={16} /></Button>
        </div>
        <button 
          class="send-circle-btn flex items-center justify-center rounded-full w-9 h-9 text-white bg-[#f97316]" 
          onclick={submitPrompt} 
          disabled={!inputText.trim() || isSending}
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
  /* HUD value tags styling */
  .hud-badge {
    background-color: var(--frame-bg);
    border: 1px solid var(--border-color);
    padding: 2px 8px;
    border-radius: 6px;
    margin-left: 4px;
    display: inline-block;
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

  :global(.preset-pill) {
    transition: all 0.25s ease;
  }
  :global(.preset-pill:hover) {
    border-color: #f97316;
    background-color: rgba(249, 115, 22, 0.05);
  }

  :global(.prompt-pill-card) {
    border-radius: 24px !important;
    box-shadow: 0 10px 30px var(--shadow-color) !important;
  }

  .deeper-btn {
    color: var(--text-secondary);
    border: 1.5px solid var(--border-color);
    transition: all 0.2s;
    background: transparent;
    cursor: pointer;
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

  :global(.bubble-content) {
    max-width: 768px !important;
    border-radius: 18px !important;
    box-shadow: 0 4px 16px var(--shadow-color) !important;
  }

  .reasoning-container {
    background-color: rgba(249, 115, 22, 0.04);
    border-left: 3px solid rgba(249, 115, 22, 0.5);
  }
</style>
