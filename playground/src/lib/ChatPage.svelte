<script>
  import { 
    ChevronDown, ExternalLink, Globe, Search, Cpu, Paperclip, Mic, 
    Send, FileText, RefreshCw, HelpCircle 
  } from '@lucide/svelte';

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
<header class="header flex items-center justify-between px-6 py-3 border-b shrink-0">
  <div class="model-picker-container relative">
    <button class="model-picker-btn flex items-center gap-2 font-semibold text-sm" onclick={handleModelPickerClick}>
      <span>{selectedModel || 'Configure Gateway'}</span>
      <ChevronDown size={14} />
    </button>
    
    {#if showModelDropdown && models.length > 0}
      <div class="model-dropdown absolute top-full left-0 mt-1 border rounded-lg shadow-xl z-20 w-56 bg-[var(--sidebar-bg)]">
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
<div class="telemetry-bar flex gap-6 px-6 py-2.5 border-b font-mono text-[10px] overflow-x-auto whitespace-nowrap shrink-0">
  <div>Status: <span class="hud-value">{statusHUD}</span></div>
  <div>Provider: <span class="hud-value text-[#f97316]">{providerHUD}</span></div>
  <div>Model: <span class="hud-value text-[#f97316]">{modelHUD}</span></div>
  <div>TTFT: <span class="hud-value">{ttftHUD}</span></div>
  <div>Latency: <span class="hud-value">{latencyHUD}</span></div>
  <div>Speed: <span class="hud-value">{speedHUD}</span></div>
</div>

<!-- Chat Scrollable Area -->
<div class="chat-scroll-area flex-grow overflow-y-auto" bind:this={chatScrollElement}>
  {#if messages.length === 0}
    <!-- Initial landing screen layout -->
    <div class="landing-container flex flex-col items-center justify-center text-center px-6">
      
      <h1 class="text-3xl font-extrabold tracking-tight mb-8">What's on your mind today?</h1>

      <!-- Prompt Card (Pill styled) -->
      <div class="prompt-pill-card mb-6">
        <textarea 
          class="prompt-textarea w-full text-sm outline-none resize-none" 
          placeholder="Ask me anything..." 
          rows="3"
          bind:value={inputText}
          onkeydown={(e) => { if(e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); submitPrompt(); } }}
        ></textarea>
        
        <div class="flex items-center justify-between pt-2 border-t">
          <div class="flex items-center gap-2">
            <button class="deeper-btn flex items-center gap-1 text-[10px] font-bold uppercase px-3 py-1.5 rounded-full border {isDeeperResearch ? 'active' : ''}" onclick={() => isDeeperResearch = !isDeeperResearch}>
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

      <!-- Bottom Presets Row -->
      <div class="presets-container flex gap-3 justify-center flex-wrap max-w-3xl">
        <button class="preset-pill flex items-center gap-2 px-4 py-2.5 rounded-full border text-xs font-medium" onclick={() => applyPreset("Summarize this article for me:")}>
          <FileText size={14} class="text-[#f97316]" />
          <span>Summarize Text</span>
        </button>
        
        <button class="preset-pill flex items-center gap-2 px-4 py-2.5 rounded-full border text-xs font-medium" onclick={() => applyPreset("Write a blog post outline on: ")}>
          <RefreshCw size={14} class="text-[#f97316]" />
          <span>Creative Writing</span>
        </button>
        
        <button class="preset-pill flex items-center gap-2 px-4 py-2.5 rounded-full border text-xs font-medium" onclick={() => applyPreset("Answer this complex question: ")}>
          <HelpCircle size={14} class="text-[#f97316]" />
          <span>Answer Questions</span>
        </button>
      </div>
    </div>
  {:else}
    <!-- Chat flow display -->
    <div class="chat-content-container">
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
  {/if}
</div>

<!-- Floating bottom input bar (Fixed overlay at bottom) -->
{#if messages.length > 0}
  <div class="bottom-input-container">
    <div class="prompt-pill-card">
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
        <button class="send-circle-btn flex items-center justify-center rounded-full w-8 h-8 text-white bg-[#f97316]" onclick={submitPrompt} disabled={!inputText.trim() || isSending}>
          <Send size={14} />
        </button>
      </div>
    </div>
    <div class="footer-disclaimer text-[10px] opacity-60 mt-2 text-center">
      Cognivo can make mistakes. Check important info.
    </div>
  </div>
{:else}
  <!-- Simple small footer when on landing screen -->
  <footer class="footer text-center py-3 text-[10px] border-t shrink-0">
    Cognivo can make mistakes. Check important info. See Cookie Preferences.
  </footer>
{/if}
