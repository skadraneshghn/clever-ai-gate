<script>
  import { onMount, tick } from 'svelte';
  import { 
    Search, X, RefreshCw, Check, Cpu, Sparkles, Eye, 
    ChevronDown, Layers, Terminal
  } from '@lucide/svelte';
  import { appState } from '$lib/state.svelte.js';

  let { 
    variant = 'header', // 'header' | 'badge'
    class: customClass = '' 
  } = $props();

  let isOpen = $state(false);
  let searchQuery = $state('');
  let selectedProvider = $state('all');
  let displayLimit = $state(50);
  let selectedIndex = $state(0);
  let searchInputRef = $state(null);
  let listContainerRef = $state(null);
  let pickerContainerRef = $state(null);
  let isRefreshing = $state(false);

  // Extract known provider from model ID
  function detectProvider(id) {
    if (!id) return 'other';
    const lower = id.toLowerCase();
    if (lower.startsWith('nvidia/')) return 'nvidia';
    if (lower.startsWith('openrouter/')) return 'openrouter';
    if (lower.startsWith('ollama/')) return 'ollama';
    if (lower.startsWith('gemini/') || lower.includes('gemini-')) return 'gemini';
    if (lower.startsWith('1min/')) return '1min';
    if (lower.startsWith('cloudflare/')) return 'cloudflare';
    if (lower.startsWith('sarvam/')) return 'sarvam';
    if (lower.startsWith('puter/')) return 'puter';
    if (lower.startsWith('agentrouter/')) return 'agentrouter';
    if (lower.startsWith('zenmux/')) return 'zenmux';
    if (lower.includes('gpt-') || lower.includes('o1-') || lower.includes('o3-')) return 'openai';
    if (lower.includes('claude-')) return 'anthropic';
    if (lower.includes('deepseek')) return 'deepseek';
    if (lower.includes('meta-llama') || lower.includes('llama-')) return 'meta';
    if (lower.includes('mistral') || lower.includes('mixtral')) return 'mistral';
    if (lower.includes('qwen')) return 'qwen';
    return 'custom';
  }

  // Get distinct provider tags present in the loaded models
  let availableProviders = $derived.by(() => {
    const counts = { all: appState.models.length };
    for (const m of appState.models) {
      const p = detectProvider(m.id);
      counts[p] = (counts[p] || 0) + 1;
    }
    // Return ordered list with prominent providers first if present
    const order = ['all', 'nvidia', 'openai', 'anthropic', 'gemini', 'openrouter', 'ollama', 'deepseek', 'meta', '1min', 'cloudflare', 'puter', 'custom'];
    return order.filter(p => counts[p] > 0).map(p => ({
      id: p,
      label: p === 'all' ? 'All' : p.toUpperCase(),
      count: counts[p] || 0
    }));
  });

  // Filtered models with instant search
  let filteredModels = $derived.by(() => {
    const q = searchQuery.trim().toLowerCase();
    const prov = selectedProvider;

    return appState.models.filter(m => {
      // Provider filter
      if (prov !== 'all') {
        const p = detectProvider(m.id);
        if (p !== prov) return false;
      }
      // Search query filter
      if (!q) return true;
      if (m.id.toLowerCase().includes(q)) return true;
      const p = detectProvider(m.id);
      if (p.includes(q)) return true;
      // Capabilities filter
      if (m.capabilities) {
        for (const [cap, enabled] of Object.entries(m.capabilities)) {
          if (enabled && cap.toLowerCase().includes(q)) return true;
        }
      }
      return false;
    });
  });

  // Windowed visible slice (only renders top displayLimit items)
  let visibleModels = $derived(filteredModels.slice(0, displayLimit));

  // Reset window and selection when query or provider changes
  $effect(() => {
    // track dependencies
    void searchQuery;
    void selectedProvider;
    displayLimit = 50;
    selectedIndex = 0;
    if (listContainerRef) {
      listContainerRef.scrollTop = 0;
    }
  });

  // Handle infinite scroll inside the windowed list
  function handleScroll(e) {
    const el = e.currentTarget;
    if (el.scrollHeight - el.scrollTop - el.clientHeight < 160) {
      if (displayLimit < filteredModels.length) {
        displayLimit = Math.min(displayLimit + 50, filteredModels.length);
      }
    }
  }

  // Toggle dropdown and auto-focus search
  async function toggleDropdown(e) {
    if (e) e.stopPropagation();
    if (appState.models.length === 0) {
      // If no models, trigger load first
      await appState.loadModels();
      if (appState.models.length === 0) {
        appState.activeSettingsTab = 'admin';
        appState.showSettingsModal = true;
        return;
      }
    }
    isOpen = !isOpen;
    if (isOpen) {
      displayLimit = 50;
      await tick();
      if (searchInputRef) {
        searchInputRef.focus();
      }
    }
  }

  function selectModel(modelId) {
    appState.selectedModel = modelId;
    isOpen = false;
    searchQuery = '';
  }

  async function handleRefresh(e) {
    if (e) e.stopPropagation();
    isRefreshing = true;
    try {
      const ok = await appState.loadModels(true);
      if (ok) {
        appState.addToast('success', `Refreshed ${appState.models.length} active models`);
      } else {
        appState.addToast('error', 'Failed to refresh models');
      }
    } finally {
      isRefreshing = false;
    }
  }

  // Keyboard navigation
  function handleKeyDown(e) {
    if (!isOpen) return;

    if (e.key === 'Escape') {
      e.preventDefault();
      isOpen = false;
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      if (filteredModels.length > 0) {
        selectedIndex = (selectedIndex + 1) % Math.min(filteredModels.length, displayLimit);
        scrollSelectedIndexIntoView();
      }
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      if (filteredModels.length > 0) {
        selectedIndex = (selectedIndex - 1 + Math.min(filteredModels.length, displayLimit)) % Math.min(filteredModels.length, displayLimit);
        scrollSelectedIndexIntoView();
      }
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (filteredModels[selectedIndex]) {
        selectModel(filteredModels[selectedIndex].id);
      }
    }
  }

  function scrollSelectedIndexIntoView() {
    if (!listContainerRef) return;
    const items = listContainerRef.querySelectorAll('.model-item');
    if (items[selectedIndex]) {
      items[selectedIndex].scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    }
  }

  // Outside click listener
  onMount(() => {
    function handleClickOutside(event) {
      if (isOpen && pickerContainerRef && !pickerContainerRef.contains(event.target)) {
        isOpen = false;
      }
    }
    document.addEventListener('click', handleClickOutside);
    return () => {
      document.removeEventListener('click', handleClickOutside);
    };
  });
</script>

<div class="model-picker-wrapper relative inline-block {customClass}" bind:this={pickerContainerRef} onkeydown={handleKeyDown}>
  <!-- Trigger Button -->
  {#if variant === 'header'}
    <button 
      type="button"
      class="header-trigger flex items-center gap-2 px-3 py-1.5 rounded-lg border font-semibold text-xs transition-all cursor-pointer shadow-sm hover:border-[#f97316]/50"
      onclick={toggleDropdown}
      title="Select AI Model"
    >
      <Cpu size={14} class="text-[#f97316] shrink-0" />
      <span class="truncate max-w-[200px] text-primary">{appState.selectedModel || 'Select Model'}</span>
      <ChevronDown size={13} class="opacity-60 transition-transform duration-200 {isOpen ? 'rotate-180' : ''}" />
    </button>
  {:else}
    <button 
      type="button"
      class="model-badge-btn flex items-center gap-1.5 px-3 py-1.5 rounded-xl border text-xs font-semibold transition-all cursor-pointer hover:border-[#f97316]/50" 
      onclick={toggleDropdown}
      title="Change Selected Model"
    >
      <Cpu size={14} class="text-[#f97316]" />
      <span class="truncate max-w-[180px] text-primary font-medium">{appState.selectedModel || 'Select Model'}</span>
      <ChevronDown size={13} class="opacity-60 transition-transform duration-200 {isOpen ? 'rotate-180' : ''}" />
    </button>
  {/if}

  <!-- Dropdown Popover (Windowed, Ultra-Lightweight) -->
  {#if isOpen}
    <div class="model-popover animate-fade-in">
      <!-- Search and Refresh Header -->
      <div class="popover-search-bar">
        <div class="search-input-box flex-grow flex items-center gap-2 px-3 py-2 rounded-lg border bg-black/5 dark:bg-white/5">
          <Search size={14} class="text-secondary shrink-0" />
          <input 
            type="text"
            placeholder="Search {appState.models.length} models (e.g. gpt-4, claude, llama, vision)..."
            bind:this={searchInputRef}
            bind:value={searchQuery}
            class="search-input w-full bg-transparent text-xs text-primary outline-none"
            onclick={(e) => e.stopPropagation()}
          />
          {#if searchQuery}
            <button 
              type="button" 
              class="clear-btn text-secondary hover:text-primary cursor-pointer"
              onclick={(e) => { e.stopPropagation(); searchQuery = ''; searchInputRef?.focus(); }}
              title="Clear search"
            >
              <X size={13} />
            </button>
          {/if}
        </div>

        <button 
          type="button" 
          class="refresh-icon-btn p-2 rounded-lg border text-secondary hover:text-[#f97316] hover:border-[#f97316]/40 transition-colors cursor-pointer shrink-0"
          onclick={handleRefresh}
          disabled={isRefreshing}
          title="Refresh models from gateway"
        >
          <RefreshCw size={14} class={isRefreshing ? 'animate-spin text-[#f97316]' : ''} />
        </button>
      </div>

      <!-- Provider Filter Pills -->
      {#if availableProviders.length > 2}
        <div class="provider-pills-row flex items-center gap-1.5 px-3 py-2 border-b overflow-x-auto no-scrollbar">
          {#each availableProviders as prov}
            <button 
              type="button"
              class="provider-pill text-[11px] font-medium px-2.5 py-1 rounded-full border transition-all cursor-pointer whitespace-nowrap {selectedProvider === prov.id ? 'active' : ''}"
              onclick={(e) => { e.stopPropagation(); selectedProvider = prov.id; }}
            >
              {prov.label} <span class="opacity-60 text-[10px]">({prov.count})</span>
            </button>
          {/each}
        </div>
      {/if}

      <!-- Model List Container (Windowed Virtualization) -->
      <div 
        class="model-list-container" 
        bind:this={listContainerRef}
        onscroll={handleScroll}
      >
        {#if visibleModels.length > 0}
          {#each visibleModels as model, idx (model.id)}
            <button 
              type="button"
              class="model-item flex items-center justify-between w-full px-3 py-2.5 text-left border-b border-[var(--border-color)]/50 transition-colors cursor-pointer {idx === selectedIndex ? 'highlighted' : ''} {appState.selectedModel === model.id ? 'selected' : ''}"
              onclick={() => selectModel(model.id)}
              onmouseenter={() => selectedIndex = idx}
            >
              <div class="model-info flex flex-col gap-0.5 min-w-0 pr-3">
                <div class="flex items-center gap-2">
                  <span class="model-id font-mono text-xs font-semibold truncate text-primary">
                    {model.id}
                  </span>
                  {#if appState.selectedModel === model.id}
                    <span class="active-tag text-[10px] font-bold text-[#f97316] shrink-0 flex items-center gap-0.5">
                      <Check size={11} /> Active
                    </span>
                  {/if}
                </div>
                
                <!-- Metadata: Provider and Capability Badges -->
                <div class="model-badges flex items-center gap-1.5 flex-wrap mt-0.5">
                  <span class="provider-tag text-[10px] px-1.5 py-0.2 rounded font-mono uppercase bg-black/5 dark:bg-white/5 text-secondary">
                    {detectProvider(model.id)}
                  </span>
                  {#if model.capabilities?.vision}
                    <span class="cap-tag vision flex items-center gap-0.5 text-[10px] px-1.5 py-0.2 rounded font-medium bg-blue-500/10 text-blue-500">
                      <Eye size={10} /> Vision
                    </span>
                  {/if}
                  {#if model.capabilities?.reasoning}
                    <span class="cap-tag reasoning flex items-center gap-0.5 text-[10px] px-1.5 py-0.2 rounded font-medium bg-purple-500/10 text-purple-500">
                      <Sparkles size={10} /> Reasoning
                    </span>
                  {/if}
                  {#if model.capabilities?.coding}
                    <span class="cap-tag code flex items-center gap-0.5 text-[10px] px-1.5 py-0.2 rounded font-medium bg-emerald-500/10 text-emerald-500">
                      <Terminal size={10} /> Code
                    </span>
                  {/if}
                </div>
              </div>

              <div class="select-indicator shrink-0">
                {#if appState.selectedModel === model.id}
                  <div class="check-circle w-5 h-5 rounded-full bg-[#f97316] text-white flex items-center justify-center">
                    <Check size={12} strokeWidth={3} />
                  </div>
                {:else}
                  <div class="select-dot w-2 h-2 rounded-full bg-secondary/30"></div>
                {/if}
              </div>
            </button>
          {/each}

          {#if visibleModels.length < filteredModels.length}
            <div class="load-more-indicator py-2 text-center text-[11px] text-secondary">
              Scroll down to load more ({visibleModels.length} of {filteredModels.length} displayed)
            </div>
          {/if}
        {:else}
          <div class="empty-state py-8 text-center text-xs text-secondary">
            <Cpu size={24} class="mx-auto mb-2 opacity-30" />
            <p>No models match "{searchQuery}"</p>
            {#if selectedProvider !== 'all'}
              <button 
                type="button" 
                class="mt-2 text-[#f97316] hover:underline cursor-pointer"
                onclick={() => selectedProvider = 'all'}
              >
                Clear provider filter
              </button>
            {/if}
          </div>
        {/if}
      </div>

      <!-- Popover Footer HUD -->
      <div class="popover-footer flex items-center justify-between px-3 py-2 border-t text-[11px] text-secondary bg-black/5 dark:bg-white/5">
        <span>
          Showing <strong>{visibleModels.length}</strong> of <strong>{filteredModels.length}</strong> models
          {#if filteredModels.length < appState.models.length}
            (total: {appState.models.length})
          {/if}
        </span>
        <span class="text-[10px] opacity-70">↑↓ to navigate · Enter to select</span>
      </div>
    </div>
  {/if}
</div>

<style>
  .header-trigger {
    background-color: var(--card-bg);
    border-color: var(--border-color);
  }
  .header-trigger:hover {
    background-color: var(--item-hover);
  }

  .model-badge-btn {
    background-color: var(--card-bg);
    border-color: var(--border-color);
  }
  .model-badge-btn:hover {
    background-color: var(--item-hover);
  }

  .model-popover {
    position: absolute;
    top: calc(100% + 6px);
    left: 0;
    width: 480px;
    max-width: 90vw;
    background-color: var(--card-bg);
    border: 1px solid var(--border-color);
    border-radius: 14px;
    box-shadow: 0 16px 40px rgba(0, 0, 0, 0.25);
    z-index: 100;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .popover-search-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 12px;
    border-bottom: 1px solid var(--border-color);
  }

  .provider-pill {
    background-color: transparent;
    border-color: var(--border-color);
    color: var(--text-secondary);
  }
  .provider-pill:hover {
    color: var(--text-primary);
    border-color: var(--text-secondary);
  }
  .provider-pill.active {
    background-color: rgba(249, 115, 22, 0.1);
    color: #f97316;
    border-color: #f97316;
    font-weight: 700;
  }

  .model-list-container {
    max-height: 320px;
    overflow-y: auto;
    overscroll-behavior: contain;
  }

  .model-item {
    background-color: transparent;
  }
  .model-item:hover, .model-item.highlighted {
    background-color: var(--item-hover);
  }
  .model-item.selected {
    background-color: rgba(249, 115, 22, 0.06);
  }

  .no-scrollbar::-webkit-scrollbar {
    display: none;
  }
  .no-scrollbar {
    -ms-overflow-style: none;
    scrollbar-width: none;
  }
</style>
