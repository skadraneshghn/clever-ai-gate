<script>
  import { onMount, onDestroy } from 'svelte';
  import { Terminal, Square, Play, Eraser, Download, Shield } from '@lucide/svelte';
  import { appState } from '$lib/state.svelte.js';
  import Button from '$lib/components/Button.svelte';
  import Input from '$lib/components/Input.svelte';
  import Card from '$lib/components/Card.svelte';

  let logsTerminalEl = $state(null);

  // Automatically scroll to bottom when new logs arrive
  $effect(() => {
    if (appState.logLines && appState.logsAutoScroll && logsTerminalEl) {
      setTimeout(() => {
        if (logsTerminalEl) {
          logsTerminalEl.scrollTop = logsTerminalEl.scrollHeight;
        }
      }, 16);
    }
  });

  function connectAdminKey() {
    const key = appState.adminKey.trim();
    if (!key) return;
    localStorage.setItem('cag_admin_key', key);
    appState.startLogsStream();
  }

  onMount(() => {
    if (appState.adminKey.trim() && !appState.logsStreaming) {
      appState.startLogsStream();
    }
  });

  onDestroy(() => {
    appState.stopLogsStream();
  });
</script>

<header class="header flex items-center justify-between px-6 py-4 border-b shrink-0">
  <div class="flex items-center gap-3">
    <Terminal size={20} class="text-[#f97316]" />
    <span class="font-bold text-base">Gateway Core Logs</span>
    {#if appState.logsStreaming}
      <span class="flex items-center gap-1.5 bg-green-500/10 border border-green-500/30 px-2.5 py-0.5 rounded-full animate-fade-in">
        <span class="log-pulse-dot"></span>
        <span class="text-xs font-bold text-green-500 uppercase">Live</span>
      </span>
    {:else}
      <span class="text-xs font-bold text-secondary bg-gray-500/10 border border-gray-500/30 px-2.5 py-0.5 rounded-full uppercase">Offline</span>
    {/if}
  </div>
  
  {#if appState.adminKey.trim()}
    <div class="flex items-center gap-2 animate-fade-in">
      {#if appState.logsStreaming}
        <Button variant="danger" size="sm" onclick={() => appState.stopLogsStream()} title="Pause stream">
          <Square size={14} />
          Pause
        </Button>
      {:else}
        <Button variant="success" size="sm" onclick={() => appState.startLogsStream()} title="Start stream">
          <Play size={14} />
          Connect
        </Button>
      {/if}
      <Button variant="secondary" size="sm" onclick={() => appState.clearLogs()} title="Clear buffer">
        <Eraser size={14} />
        Clear
      </Button>
      <Button variant="secondary" size="sm" onclick={() => appState.downloadTodayLog()} title="Download today's log file">
        <Download size={14} />
        Download
      </Button>
      <label class="flex items-center gap-2 text-xs font-semibold cursor-pointer select-none ml-2 text-secondary hover:text-primary">
        <input type="checkbox" bind:checked={appState.logsAutoScroll} class="log-checkbox w-4 h-4 rounded border-gray-300 accent-orange-500" />
        Auto-scroll
      </label>
    </div>
  {/if}
</header>

{#if !appState.adminKey.trim()}
  <!-- Admin key prompt if not set -->
  <div class="logs-key-prompt flex flex-col justify-center items-center flex-grow p-6">
    <Card variant="filled" padding="lg" class="logs-key-card flex flex-col items-center text-center">
      <Shield size={40} class="text-[#f97316] mb-4 animate-pulse" />
      <h2 class="font-bold text-lg mb-2 text-primary">Admin Key Required</h2>
      <p class="text-sm mb-6 text-secondary max-w-sm">The core gateway log stream is protected. Enter your Admin API Key to establish a secure connection.</p>
      
      <div class="flex flex-col gap-3 w-full max-w-sm">
        <Input
          type="password"
          placeholder="Enter Admin API Key..."
          bind:value={appState.adminKey}
          onkeydown={(e) => { if (e.key === 'Enter') connectAdminKey(); }}
        />
        <Button variant="primary" size="md" onclick={connectAdminKey}>
          Connect Stream
        </Button>
      </div>
      
      {#if appState.logsError}
        <p class="text-red-500 text-sm font-semibold mt-4">{appState.logsError}</p>
      {/if}
    </Card>
  </div>
{:else}
  <!-- Log terminal -->
  <div class="log-terminal-wrap flex-grow flex flex-col overflow-hidden">
    <!-- Stats bar -->
    <div class="log-stats-bar">
      <span>Entries: <strong>{appState.logLines.length}</strong></span>
      <span>Buffer: <strong>{Math.min(appState.logLines.length, 500)}/500</strong></span>
      {#if appState.logsError}
        <span class="text-red-500 font-bold ml-auto">{appState.logsError}</span>
      {/if}
    </div>

    <!-- Terminal body -->
    <div
      class="log-terminal flex-grow"
      bind:this={logsTerminalEl}
      onscroll={() => {
        if (!logsTerminalEl) return;
        const atBottom = logsTerminalEl.scrollHeight - logsTerminalEl.scrollTop - logsTerminalEl.clientHeight < 40;
        appState.logsAutoScroll = atBottom;
      }}
    >
      {#if appState.logLines.length === 0}
        <div class="log-empty">
          <Terminal size={48} class="opacity-20 mb-4" />
          <p class="opacity-50 text-sm">{appState.logsStreaming ? 'Waiting for log entries…' : 'Click Connect to start streaming logs.'}</p>
        </div>
      {:else}
        {#each appState.logLines as log, i (i)}
          <div class="log-row {appState.logLevelClass(log.level)}">
            <span class="log-time">{appState.formatLogTime(log.timestamp)}</span>
            <span class="log-lvl">{(log.level || 'info').toUpperCase()}</span>
            <span class="log-msg">{log.msg || ''}</span>
            {#if log.caller}
              <span class="log-caller">{log.caller}</span>
            {/if}
            {#if log.model || log.provider}
              <span class="log-meta">
                {#if log.model}model={log.model}{/if}
                {#if log.model && log.provider} · {/if}
                {#if log.provider}provider={log.provider}{/if}
              </span>
            {/if}
            {#if log.error}
              <span class="log-err-detail">{log.error}</span>
            {/if}
          </div>
        {/each}
      {/if}
    </div>
  </div>
{/if}

<style>
  .log-terminal-wrap {
    flex: 1;
    display: flex;
    flex-direction: column;
    background: #09090b;
    border-top: 1px solid var(--border-color);
    overflow: hidden;
  }
  .log-stats-bar {
    display: flex;
    align-items: center;
    gap: 20px;
    padding: 10px 20px;
    background: #18181b;
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
    font-family: monospace;
    font-size: 11px;
    color: #a1a1aa;
  }
  .log-terminal {
    flex: 1;
    padding: 20px;
    overflow-y: auto;
    font-family: 'Fira Code', 'Courier New', Courier, monospace;
    font-size: 13px;
    line-height: 1.7;
    color: #e4e4e7;
    background: #09090b;
  }

  .log-empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: #71717a;
    text-align: center;
  }

  .log-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px;
    padding: 4px 0;
    border-bottom: 1px solid rgba(255, 255, 255, 0.02);
  }
  .log-time {
    color: #71717a;
    font-size: 11px;
    width: 105px;
    flex-shrink: 0;
  }
  .log-lvl {
    font-weight: 700;
    font-size: 10px;
    padding: 2px 6px;
    border-radius: 4px;
    width: 50px;
    text-align: center;
    flex-shrink: 0;
  }
  :global(.lvl-debug .log-lvl) { background: rgba(59, 130, 246, 0.15); color: #60a5fa; }
  :global(.lvl-info .log-lvl)  { background: rgba(255, 255, 255, 0.08); color: #e4e4e7; }
  :global(.lvl-warn .log-lvl)  { background: rgba(234, 179, 8, 0.15); color: #facc15; }
  :global(.lvl-error .log-lvl) { background: rgba(239, 68, 68, 0.15); color: #f87171; }
  :global(.lvl-fatal .log-lvl) { background: rgba(236, 72, 153, 0.2); color: #f472b6; border: 1px solid rgba(236, 72, 153, 0.4); }

  .log-msg {
    color: #e4e4e7;
    word-break: break-all;
    flex: 1;
    min-width: 240px;
  }
  .log-caller {
    color: #52525b;
    font-size: 10px;
    margin-left: auto;
    font-style: italic;
  }

  .log-meta {
    color: #52526b;
    font-size: 11px;
  }

  .log-err-detail {
    color: #f87171;
    font-size: 12px;
    width: 100%;
    padding-left: calc(105px + 50px + 20px);
    margin-top: 2px;
  }
</style>
