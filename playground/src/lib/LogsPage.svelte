<script>
  import { Terminal, Square, Play, Eraser, Download } from '@lucide/svelte';

  let {
    adminKey = $bindable(''),
    logLines = $bindable([]),
    logsStreaming = $bindable(false),
    logsAutoScroll = $bindable(true),
    logsError = $bindable(''),
    startLogsStream,
    stopLogsStream,
    clearLogs,
    downloadTodayLog,
    formatLogTime,
    logLevelClass
  } = $props();

  let logsTerminalEl = $state(null);

  // Automatically scroll to bottom when new logs arrive
  $effect(() => {
    if (logLines && logsAutoScroll && logsTerminalEl) {
      // Small timeout to ensure DOM has rendered new elements
      setTimeout(() => {
        if (logsTerminalEl) {
          logsTerminalEl.scrollTop = logsTerminalEl.scrollHeight;
        }
      }, 16);
    }
  });
</script>

<header class="header flex items-center justify-between px-6 py-3 border-b shrink-0">
  <div class="flex items-center gap-3">
    <Terminal size={18} class="text-[#f97316]" />
    <span class="font-bold text-sm">Gateway Core Logs</span>
    {#if logsStreaming}
      <span class="flex items-center gap-1.5">
        <span class="log-pulse-dot"></span>
        <span class="text-[10px] font-bold text-green-500 uppercase">Live</span>
      </span>
    {:else}
      <span class="text-[10px] font-bold text-secondary uppercase">Offline</span>
    {/if}
  </div>
  <div class="flex items-center gap-2">
    {#if logsStreaming}
      <button class="log-action-btn log-btn-stop" onclick={stopLogsStream} title="Pause stream">
        <Square size={12} />
        Pause
      </button>
    {:else}
      <button class="log-action-btn log-btn-start" onclick={startLogsStream} title="Start stream">
        <Play size={12} />
        Connect
      </button>
    {/if}
    <button class="log-action-btn log-btn-clear" onclick={clearLogs} title="Clear buffer">
      <Eraser size={12} />
      Clear
    </button>
    <button class="log-action-btn log-btn-download" onclick={downloadTodayLog} title="Download today's log file">
      <Download size={12} />
      Download
    </button>
    <label class="flex items-center gap-1.5 text-[10px] font-medium cursor-pointer select-none">
      <input type="checkbox" bind:checked={logsAutoScroll} class="log-checkbox" />
      Auto-scroll
    </label>
  </div>
</header>

{#if !adminKey.trim()}
  <!-- Admin key prompt if not set -->
  <div class="logs-key-prompt">
    <div class="logs-key-card">
      <Terminal size={32} class="text-[#f97316] mb-3" />
      <h2 class="font-bold text-base mb-1">Admin Key Required</h2>
      <p class="text-xs mb-4">The log stream is protected by your Admin API Key.</p>
      <div class="flex gap-2 w-full max-w-sm">
        <input
          type="password"
          class="input-box flex-grow p-2.5 rounded-lg border text-sm"
          placeholder="Enter Admin API Key..."
          bind:value={adminKey}
          onkeydown={(e) => { if (e.key === 'Enter') startLogsStream(); }}
        />
        <button class="px-4 py-2 rounded-lg text-white bg-[#f97316] font-semibold text-xs" onclick={startLogsStream}>
          Connect
        </button>
      </div>
      {#if logsError}
        <p class="text-red-500 text-xs mt-3">{logsError}</p>
      {/if}
    </div>
  </div>
{:else}
  <!-- Log terminal -->
  <div class="log-terminal-wrap">
    <!-- Stats bar -->
    <div class="log-stats-bar">
      <span>Entries: <strong>{logLines.length}</strong></span>
      <span>Buffer: <strong>{Math.min(logLines.length, 500)}/500</strong></span>
      {#if logsError}
        <span class="text-red-500 font-medium">{logsError}</span>
      {/if}
    </div>

    <!-- Terminal body -->
    <div
      class="log-terminal"
      bind:this={logsTerminalEl}
      onscroll={() => {
        if (!logsTerminalEl) return;
        const atBottom = logsTerminalEl.scrollHeight - logsTerminalEl.scrollTop - logsTerminalEl.clientHeight < 40;
        logsAutoScroll = atBottom;
      }}
    >
      {#if logLines.length === 0}
        <div class="log-empty">
          <Terminal size={40} class="opacity-20 mb-3" />
          <p class="opacity-40 text-xs">{logsStreaming ? 'Waiting for log entries…' : 'Click Connect to start streaming logs.'}</p>
        </div>
      {:else}
        {#each logLines as log, i (i)}
          <div class="log-row {logLevelClass(log.level)}">
            <span class="log-time">{formatLogTime(log.timestamp)}</span>
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
