<script>
  import { onMount, onDestroy } from 'svelte';
  import {
    CalendarClock, Plus, RefreshCw, Play, Pause, Trash2, Pencil, X,
    Settings2, Activity, Clock, CheckCircle, XCircle, AlertCircle,
    RotateCcw, ChevronLeft, ChevronRight, Search, ToggleLeft, ToggleRight,
    ListChecks, Layers, Zap, Timer
  } from '@lucide/svelte';
  import { appState } from '$lib/state.svelte.js';
  import Button from '$lib/components/Button.svelte';
  import Card from '$lib/components/Card.svelte';
  import Modal from '$lib/components/Modal.svelte';
  import Input from '$lib/components/Input.svelte';

  // ─── API helper ────────────────────────────────────────────────────────────
  function adminKey() {
    return localStorage.getItem('cag_admin_key') || appState.adminKey || '';
  }

  async function api(method, path, body) {
    const res = await fetch('/api/v1/admin' + path, {
      method,
      headers: {
        'Content-Type': 'application/json',
        'Authorization': 'Bearer ' + adminKey(),
      },
      body: body ? JSON.stringify(body) : undefined,
    });
    if (res.status === 204) return null;
    const data = await res.json();
    if (!res.ok) throw new Error(data.error || data.message || res.statusText);
    return data;
  }

  // ─── Active tab ─────────────────────────────────────────────────────────────
  let activeTab = $state('jobs'); // 'jobs' | 'runs' | 'settings'

  // ─── Stats ─────────────────────────────────────────────────────────────────
  let stats = $state(null);

  async function loadStats() {
    try { stats = await api('GET', '/scheduler/stats'); } catch {}
  }

  // ─── Job types ─────────────────────────────────────────────────────────────
  let jobTypes = $state([]);
  async function loadTypes() {
    try { jobTypes = await api('GET', '/scheduler/types') || []; } catch {}
  }

  // ─── Jobs list ─────────────────────────────────────────────────────────────
  let jobs = $state([]);
  let jobsTotal = $state(0);
  let jobsLimit = 20;
  let jobsOffset = $state(0);
  let jobsLoading = $state(false);
  let jobsError = $state('');
  let jobSearch = $state('');
  let jobFilterStatus = $state('');
  let jobFilterType = $state('');
  let searchTimer = null;

  async function loadJobs() {
    jobsLoading = true;
    jobsError = '';
    try {
      const params = new URLSearchParams({ limit: jobsLimit, offset: jobsOffset });
      if (jobSearch) params.set('search', jobSearch);
      if (jobFilterStatus) params.set('enabled', jobFilterStatus === 'enabled' ? 'true' : 'false');
      if (jobFilterType) params.set('job_type', jobFilterType);
      const data = await api('GET', '/jobs?' + params);
      jobs = data.data || [];
      jobsTotal = data.total || 0;
    } catch(e) {
      jobsError = e.message;
    } finally {
      jobsLoading = false;
    }
  }

  function onSearchInput() {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => { jobsOffset = 0; loadJobs(); }, 300);
  }

  function prevJobsPage() { if (jobsOffset > 0) { jobsOffset = Math.max(0, jobsOffset - jobsLimit); loadJobs(); } }
  function nextJobsPage() { if (jobsOffset + jobsLimit < jobsTotal) { jobsOffset += jobsLimit; loadJobs(); } }

  // ─── Run History ────────────────────────────────────────────────────────────
  let runs = $state([]);
  let runsTotal = $state(0);
  let runsLimit = 50;
  let runsOffset = $state(0);
  let runsLoading = $state(false);
  let runsFilterStatus = $state('');

  async function loadRuns() {
    runsLoading = true;
    try {
      const params = new URLSearchParams({ limit: runsLimit, offset: runsOffset });
      if (runsFilterStatus) params.set('status', runsFilterStatus);
      const data = await api('GET', '/jobs/runs?' + params);
      runs = data.data || [];
      runsTotal = data.total || 0;
    } catch(e) {} finally { runsLoading = false; }
  }

  function prevRunsPage() { if (runsOffset > 0) { runsOffset = Math.max(0, runsOffset - runsLimit); loadRuns(); } }
  function nextRunsPage() { if (runsOffset + runsLimit < runsTotal) { runsOffset += runsLimit; loadRuns(); } }

  async function deleteRun(id) {
    try { await api('DELETE', '/jobs/runs/' + id); loadRuns(); toast('Run deleted', 'success'); }
    catch(e) { toast(e.message, 'error'); }
  }

  // ─── Settings ───────────────────────────────────────────────────────────────
  let settingsRows = $state([]);
  let settingsLoading = $state(false);
  let settingsSaving = $state(false);

  // Editable settings object
  let cfg = $state({
    max_concurrent_jobs: 10,
    job_timeout: 300,
    timezone: 'UTC',
    singleton_mode: true,
    paused: false,
    max_retries: 3,
    retry_backoff: 'exponential',
    retry_delay: 30,
    worker_pool_size: 5,
    queue_key: 'cag:jobs:queue',
    dlq_enabled: true,
    dlq_key: 'cag:jobs:dlq',
    dlq_ttl: 604800,
    log_retention_days: 30,
    heartbeat_interval: 30,
  });

  async function loadSettings() {
    settingsLoading = true;
    try {
      const rows = await api('GET', '/scheduler/settings') || [];
      settingsRows = rows;
      const kv = {};
      rows.forEach(r => kv[r.key] = r.value);
      const g = (k, d) => kv[k] !== undefined ? kv[k] : String(d);
      cfg = {
        max_concurrent_jobs: parseInt(g('max_concurrent_jobs', 10)),
        job_timeout: parseInt(g('job_timeout', 300)),
        timezone: g('timezone', 'UTC'),
        singleton_mode: g('singleton_mode', 'true') === 'true',
        paused: g('paused', 'false') === 'true',
        max_retries: parseInt(g('max_retries', 3)),
        retry_backoff: g('retry_backoff', 'exponential'),
        retry_delay: parseInt(g('retry_delay', 30)),
        worker_pool_size: parseInt(g('worker_pool_size', 5)),
        queue_key: g('queue_key', 'cag:jobs:queue'),
        dlq_enabled: g('dlq_enabled', 'true') === 'true',
        dlq_key: g('dlq_key', 'cag:jobs:dlq'),
        dlq_ttl: parseInt(g('dlq_ttl', 604800)),
        log_retention_days: parseInt(g('log_retention_days', 30)),
        heartbeat_interval: parseInt(g('heartbeat_interval', 30)),
      };
    } catch(e) { toast(e.message, 'error'); }
    finally { settingsLoading = false; }
  }

  async function saveSettings() {
    settingsSaving = true;
    try {
      await api('PUT', '/scheduler/settings', { settings: cfg });
      toast('Settings saved and live-reloaded ✓', 'success');
      loadStats();
    } catch(e) { toast(e.message, 'error'); }
    finally { settingsSaving = false; }
  }

  async function restartScheduler() {
    try { await api('POST', '/scheduler/restart'); toast('Scheduler config reloaded', 'success'); }
    catch(e) { toast(e.message, 'error'); }
  }

  // ─── Job Actions ────────────────────────────────────────────────────────────
  async function triggerJob(id, name) {
    try {
      const r = await api('POST', '/jobs/' + id + '/trigger');
      toast(`▶ "${name}" triggered (Run: ${r.run_id.substring(0,8)})`, 'success');
      setTimeout(loadJobs, 1200);
    } catch(e) { toast(e.message, 'error'); }
  }

  async function pauseJob(id) {
    try { await api('POST', '/jobs/' + id + '/pause'); toast('Job paused', 'info'); loadJobs(); }
    catch(e) { toast(e.message, 'error'); }
  }

  async function resumeJob(id) {
    try { await api('POST', '/jobs/' + id + '/resume'); toast('Job resumed ✓', 'success'); loadJobs(); }
    catch(e) { toast(e.message, 'error'); }
  }

  async function deleteJob(id, name) {
    if (!confirm(`Delete job "${name}" and all its run history?`)) return;
    try {
      await api('DELETE', '/jobs/' + id);
      toast(`"${name}" deleted`, 'success');
      loadJobs(); loadStats();
    } catch(e) { toast(e.message, 'error'); }
  }

  // ─── Create / Edit Modal ────────────────────────────────────────────────────
  let showCreateModal = $state(false);
  let editJobId = $state(null);
  let formSaving = $state(false);

  let form = $state({
    name: '', description: '', job_type: '', schedule_type: 'cron',
    cron_expression: '', interval_seconds: 0, run_at: '',
    payload: '{}', max_retries: 3, timeout_seconds: 300,
    timezone: 'UTC', tags: '', is_enabled: true, is_singleton: true,
  });

  function openCreate() {
    editJobId = null;
    form = { name:'', description:'', job_type:'', schedule_type:'cron',
      cron_expression:'', interval_seconds:0, run_at:'',
      payload:'{}', max_retries:3, timeout_seconds:300,
      timezone:'UTC', tags:'', is_enabled:true, is_singleton:true };
    showCreateModal = true;
  }

  async function openEdit(id) {
    editJobId = id;
    try {
      const j = await api('GET', '/jobs/' + id);
      form = {
        name: j.name, description: j.description || '',
        job_type: j.job_type, schedule_type: j.schedule_type,
        cron_expression: j.cron_expression || '',
        interval_seconds: j.interval_seconds || 0,
        run_at: '', payload: JSON.stringify(j.payload || {}, null, 2),
        max_retries: j.max_retries, timeout_seconds: j.timeout_seconds,
        timezone: j.timezone || 'UTC',
        tags: (j.tags || []).join(', '),
        is_enabled: j.is_enabled, is_singleton: j.is_singleton,
      };
      showCreateModal = true;
    } catch(e) { toast(e.message, 'error'); }
  }

  async function submitForm() {
    formSaving = true;
    try {
      let payload = {};
      try { payload = JSON.parse(form.payload || '{}'); }
      catch { toast('Invalid JSON in Payload field', 'error'); formSaving = false; return; }

      const body = {
        name: form.name,
        description: form.description,
        job_type: form.job_type,
        schedule_type: form.schedule_type,
        cron_expression: form.cron_expression,
        interval_seconds: parseInt(form.interval_seconds) || 0,
        run_at: form.run_at || undefined,
        payload,
        max_retries: parseInt(form.max_retries),
        timeout_seconds: parseInt(form.timeout_seconds),
        timezone: form.timezone,
        tags: form.tags.split(',').map(t => t.trim()).filter(Boolean),
        is_enabled: form.is_enabled,
        is_singleton: form.is_singleton,
      };

      if (editJobId) {
        await api('PUT', '/jobs/' + editJobId, body);
        toast('Job updated ✓', 'success');
      } else {
        await api('POST', '/jobs', body);
        toast('Job created ✓', 'success');
      }

      showCreateModal = false;
      loadJobs(); loadStats();
    } catch(e) { toast(e.message, 'error'); }
    finally { formSaving = false; }
  }

  // ─── Detail Modal ───────────────────────────────────────────────────────────
  let showDetailModal = $state(false);
  let detailJob = $state(null);
  let detailRuns = $state([]);
  let detailLoading = $state(false);

  async function openDetail(id) {
    showDetailModal = true;
    detailLoading = true;
    detailJob = null; detailRuns = [];
    try {
      const [j, r] = await Promise.all([
        api('GET', '/jobs/' + id),
        api('GET', `/jobs/${id}/runs?limit=10`)
      ]);
      detailJob = j;
      detailRuns = r.data || [];
    } catch(e) { toast(e.message, 'error'); }
    finally { detailLoading = false; }
  }

  // ─── Cron preview ───────────────────────────────────────────────────────────
  const cronDescriptions = {
    '*/1 * * * *': 'every minute', '*/5 * * * *': 'every 5 minutes',
    '*/15 * * * *': 'every 15 minutes', '*/30 * * * *': 'every 30 minutes',
    '0 * * * *': 'every hour', '0 */2 * * *': 'every 2 hours',
    '0 */6 * * *': 'every 6 hours', '0 0 * * *': 'every day at midnight',
    '0 0 * * 0': 'every Sunday', '0 0 1 * *': 'first of month',
    '@daily': 'daily', '@hourly': 'hourly', '@weekly': 'weekly',
  };

  let cronPreview = $derived(
    form.schedule_type === 'cron' && form.cron_expression
      ? cronDescriptions[form.cron_expression] || ''
      : ''
  );

  // ─── Toast ──────────────────────────────────────────────────────────────────
  let toasts = $state([]);
  let toastId = 0;
  function toast(msg, type = 'info') {
    const id = ++toastId;
    toasts = [...toasts, { id, msg, type }];
    setTimeout(() => { toasts = toasts.filter(t => t.id !== id); }, 4000);
  }

  // ─── Auto refresh ───────────────────────────────────────────────────────────
  let refreshInterval;

  onMount(() => {
    loadStats(); loadJobs(); loadTypes();
    refreshInterval = setInterval(() => {
      loadStats();
      if (activeTab === 'jobs') loadJobs();
      if (activeTab === 'runs') loadRuns();
    }, 10000);
  });

  onDestroy(() => { clearInterval(refreshInterval); });

  // Tab switching
  function switchTab(tab) {
    activeTab = tab;
    if (tab === 'runs' && runs.length === 0) loadRuns();
    if (tab === 'settings' && settingsRows.length === 0) loadSettings();
  }

  // ─── Helpers ────────────────────────────────────────────────────────────────
  function scheduleLabel(j) {
    if (j.schedule_type === 'cron') return j.cron_expression || '—';
    if (j.schedule_type === 'interval') return j.interval_seconds ? `every ${j.interval_seconds}s` : '—';
    if (j.schedule_type === 'one_time') return 'one-time';
    return 'manual';
  }

  function relTime(iso) {
    if (!iso) return '—';
    const diff = Date.now() - new Date(iso).getTime();
    const abs = Math.abs(diff);
    const future = diff < 0;
    if (abs < 60000) return future ? 'soon' : 'just now';
    if (abs < 3600000) return (future ? 'in ' : '') + Math.floor(abs/60000) + 'm' + (future ? '' : ' ago');
    if (abs < 86400000) return (future ? 'in ' : '') + Math.floor(abs/3600000) + 'h' + (future ? '' : ' ago');
    return (future ? 'in ' : '') + Math.floor(abs/86400000) + 'd' + (future ? '' : ' ago');
  }

  function fmtDuration(ms) {
    if (!ms) return '—';
    if (ms < 1000) return ms + 'ms';
    if (ms < 60000) return (ms/1000).toFixed(1) + 's';
    return Math.floor(ms/60000) + 'm ' + Math.floor((ms%60000)/1000) + 's';
  }
</script>

<!-- ═══════════════════════════════════════════════════════════════════════════
     TOASTS
══════════════════════════════════════════════════════════════════════════════ -->
<div class="fixed top-4 right-4 z-[9999] flex flex-col gap-2 pointer-events-none">
  {#each toasts as t (t.id)}
    <div class="px-4 py-3 rounded-lg text-sm font-medium shadow-xl pointer-events-auto animate-fade-in
      {t.type === 'success' ? 'bg-emerald-900/90 text-emerald-300 border border-emerald-700/50' :
       t.type === 'error'   ? 'bg-red-900/90 text-red-300 border border-red-700/50' :
                              'bg-blue-900/90 text-blue-300 border border-blue-700/50'}">
      {t.msg}
    </div>
  {/each}
</div>

<!-- ═══════════════════════════════════════════════════════════════════════════
     PAGE
══════════════════════════════════════════════════════════════════════════════ -->
<div class="flex flex-col h-full overflow-hidden">

  <!-- Page Header -->
  <div class="flex items-center justify-between px-6 py-4 border-b shrink-0 gap-4">
    <div>
      <h1 class="text-xl font-bold flex items-center gap-2">
        <CalendarClock size={22} class="text-accent" />
        Job Scheduler
      </h1>
      <p class="text-xs text-muted mt-0.5">Manage scheduled tasks, async jobs, and execution history</p>
    </div>
    <div class="flex gap-2 items-center">
      {#if stats?.scheduler_paused}
        <span class="text-xs font-semibold px-3 py-1 rounded-full bg-amber-500/15 text-amber-400 border border-amber-500/30">
          ⏸ Scheduler Paused
        </span>
      {:else}
        <span class="text-xs font-semibold px-3 py-1 rounded-full bg-emerald-500/15 text-emerald-400 border border-emerald-500/30 flex items-center gap-1.5">
          <span class="w-1.5 h-1.5 bg-emerald-400 rounded-full animate-pulse"></span>
          Live
        </span>
      {/if}
      <Button variant="secondary" size="sm" onclick={() => { loadStats(); loadJobs(); }}>
        <RefreshCw size={14} />
        Refresh
      </Button>
      <Button variant="primary" size="sm" onclick={openCreate}>
        <Plus size={14} />
        New Job
      </Button>
    </div>
  </div>

  <!-- Stats Row -->
  {#if stats}
    <div class="grid grid-cols-6 gap-3 px-6 py-4 shrink-0 border-b">
      <Card variant="filled" padding="sm" class="flex flex-col gap-1">
        <span class="text-[10px] uppercase font-bold tracking-wider text-muted">Total Jobs</span>
        <span class="text-2xl font-bold text-accent">{stats.total_jobs ?? 0}</span>
        <span class="text-[10px] text-muted">{stats.enabled_jobs ?? 0} enabled</span>
      </Card>
      <Card variant="filled" padding="sm" class="flex flex-col gap-1">
        <span class="text-[10px] uppercase font-bold tracking-wider text-muted">Running</span>
        <span class="text-2xl font-bold text-purple-400">{stats.running_24h ?? 0}</span>
        <span class="text-[10px] text-muted">last 24h</span>
      </Card>
      <Card variant="filled" padding="sm" class="flex flex-col gap-1">
        <span class="text-[10px] uppercase font-bold tracking-wider text-muted">Pending</span>
        <span class="text-2xl font-bold text-amber-400">{stats.pending_24h ?? 0}</span>
        <span class="text-[10px] text-muted">queued</span>
      </Card>
      <Card variant="filled" padding="sm" class="flex flex-col gap-1">
        <span class="text-[10px] uppercase font-bold tracking-wider text-muted">Completed</span>
        <span class="text-2xl font-bold text-emerald-400">{stats.completed_24h ?? 0}</span>
        <span class="text-[10px] text-muted">successful</span>
      </Card>
      <Card variant="filled" padding="sm" class="flex flex-col gap-1">
        <span class="text-[10px] uppercase font-bold tracking-wider text-muted">Failed</span>
        <span class="text-2xl font-bold text-red-400">{stats.failed_24h ?? 0}</span>
        <span class="text-[10px] text-muted">errors</span>
      </Card>
      <Card variant="filled" padding="sm" class="flex flex-col gap-1">
        <span class="text-[10px] uppercase font-bold tracking-wider text-muted">Queue / DLQ</span>
        <span class="text-2xl font-bold text-blue-400">{stats.queue_depth ?? 0}</span>
        <span class="text-[10px] text-muted">{stats.dlq_depth ?? 0} in DLQ</span>
      </Card>
    </div>
  {/if}

  <!-- Tabs -->
  <div class="flex gap-1 px-6 pt-3 shrink-0 border-b">
    <button
      class="tab-btn px-4 py-2 text-sm font-medium rounded-t-lg border-b-2 transition-all
        {activeTab === 'jobs' ? 'border-accent text-accent' : 'border-transparent text-muted hover:text-foreground'}"
      onclick={() => switchTab('jobs')}
    >
      <span class="flex items-center gap-2"><Layers size={14} /> Jobs</span>
    </button>
    <button
      class="tab-btn px-4 py-2 text-sm font-medium rounded-t-lg border-b-2 transition-all
        {activeTab === 'runs' ? 'border-accent text-accent' : 'border-transparent text-muted hover:text-foreground'}"
      onclick={() => switchTab('runs')}
    >
      <span class="flex items-center gap-2"><Activity size={14} /> Run History</span>
    </button>
    <button
      class="tab-btn px-4 py-2 text-sm font-medium rounded-t-lg border-b-2 transition-all
        {activeTab === 'settings' ? 'border-accent text-accent' : 'border-transparent text-muted hover:text-foreground'}"
      onclick={() => switchTab('settings')}
    >
      <span class="flex items-center gap-2"><Settings2 size={14} /> Settings</span>
    </button>
  </div>

  <!-- Tab Content -->
  <div class="flex-1 overflow-hidden">

    <!-- ═══ JOBS TAB ═══ -->
    {#if activeTab === 'jobs'}
      <div class="flex flex-col h-full overflow-hidden">
        <!-- Filters -->
        <div class="flex gap-3 px-6 py-3 shrink-0 border-b items-center">
          <div class="relative flex-1 max-w-xs">
            <Search size={14} class="absolute left-3 top-1/2 -translate-y-1/2 text-muted" />
            <input
              type="text"
              placeholder="Search jobs..."
              bind:value={jobSearch}
              oninput={onSearchInput}
              class="input-base pl-9 w-full"
            />
          </div>
          <select bind:value={jobFilterStatus} onchange={() => { jobsOffset = 0; loadJobs(); }} class="input-base text-sm">
            <option value="">All Status</option>
            <option value="enabled">Enabled</option>
            <option value="disabled">Disabled</option>
          </select>
          <select bind:value={jobFilterType} onchange={() => { jobsOffset = 0; loadJobs(); }} class="input-base text-sm">
            <option value="">All Types</option>
            {#each jobTypes as t}
              <option value={t}>{t}</option>
            {/each}
          </select>
          <span class="text-xs text-muted ml-auto">{jobsTotal} total</span>
        </div>

        <!-- Table -->
        <div class="flex-1 overflow-auto">
          {#if jobsLoading}
            <div class="flex items-center justify-center h-32">
              <RefreshCw size={20} class="animate-spin text-muted" />
            </div>
          {:else if jobsError}
            <div class="p-6 text-center text-red-400 text-sm">{jobsError}</div>
          {:else if jobs.length === 0}
            <div class="flex flex-col items-center justify-center h-48 gap-3 text-muted">
              <CalendarClock size={40} class="opacity-30" />
              <p class="font-medium">No jobs found</p>
              <Button variant="primary" size="sm" onclick={openCreate}><Plus size={14} /> Create First Job</Button>
            </div>
          {:else}
            <table class="w-full text-sm">
              <thead class="sticky top-0 bg-surface z-10">
                <tr class="border-b">
                  <th class="text-left px-6 py-3 text-[11px] font-semibold uppercase tracking-wider text-muted">Job Name</th>
                  <th class="text-left px-4 py-3 text-[11px] font-semibold uppercase tracking-wider text-muted">Type</th>
                  <th class="text-left px-4 py-3 text-[11px] font-semibold uppercase tracking-wider text-muted">Schedule</th>
                  <th class="text-left px-4 py-3 text-[11px] font-semibold uppercase tracking-wider text-muted">Last Run</th>
                  <th class="text-left px-4 py-3 text-[11px] font-semibold uppercase tracking-wider text-muted">Runs</th>
                  <th class="text-left px-4 py-3 text-[11px] font-semibold uppercase tracking-wider text-muted">Status</th>
                  <th class="px-4 py-3"></th>
                </tr>
              </thead>
              <tbody>
                {#each jobs as job (job.id)}
                  <tr
                    class="border-b border-border/50 hover:bg-surface-hover transition-colors cursor-pointer group"
                    onclick={() => openDetail(job.id)}
                  >
                    <td class="px-6 py-3.5">
                      <div class="font-semibold">{job.name}</div>
                      {#if job.description}
                        <div class="text-[11px] text-muted truncate max-w-[200px]">{job.description}</div>
                      {/if}
                    </td>
                    <td class="px-4 py-3.5">
                      <span class="inline-block px-2 py-0.5 rounded-full text-[11px] font-mono font-medium bg-accent/10 text-accent border border-accent/20">
                        {job.job_type}
                      </span>
                    </td>
                    <td class="px-4 py-3.5">
                      <span class="font-mono text-[11px] bg-surface-hover px-2 py-0.5 rounded text-muted">{scheduleLabel(job)}</span>
                    </td>
                    <td class="px-4 py-3.5 text-sm text-muted">
                      {relTime(job.last_run_at)}
                      {#if job.last_run_status}
                        <div>
                          <span class="status-pill {job.last_run_status}">{job.last_run_status}</span>
                        </div>
                      {/if}
                    </td>
                    <td class="px-4 py-3.5 text-sm">
                      <span class="text-emerald-400">{job.success_count}</span>
                      <span class="text-muted mx-1">/</span>
                      <span class="text-red-400">{job.failure_count}</span>
                      <div class="text-[10px] text-muted">{job.run_count} total</div>
                    </td>
                    <td class="px-4 py-3.5">
                      <span class="status-pill {job.is_enabled ? 'enabled' : 'disabled'}">
                        {job.is_enabled ? 'enabled' : 'disabled'}
                      </span>
                    </td>
                    <td class="px-4 py-3.5" onclick={(e) => e.stopPropagation()}>
                      <div class="flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                        <button
                          class="p-1.5 rounded hover:bg-emerald-500/15 text-emerald-400 hover:text-emerald-300 transition-colors"
                          title="Trigger Now"
                          onclick={() => triggerJob(job.id, job.name)}
                        ><Play size={13} /></button>
                        {#if job.is_enabled}
                          <button class="p-1.5 rounded hover:bg-amber-500/15 text-amber-400 hover:text-amber-300 transition-colors" title="Pause" onclick={() => pauseJob(job.id)}>
                            <Pause size={13} />
                          </button>
                        {:else}
                          <button class="p-1.5 rounded hover:bg-emerald-500/15 text-emerald-400 hover:text-emerald-300 transition-colors" title="Resume" onclick={() => resumeJob(job.id)}>
                            <Play size={13} />
                          </button>
                        {/if}
                        <button class="p-1.5 rounded hover:bg-blue-500/15 text-blue-400 hover:text-blue-300 transition-colors" title="Edit" onclick={() => openEdit(job.id)}>
                          <Pencil size={13} />
                        </button>
                        <button class="p-1.5 rounded hover:bg-red-500/15 text-red-400 hover:text-red-300 transition-colors" title="Delete" onclick={() => deleteJob(job.id, job.name)}>
                          <Trash2 size={13} />
                        </button>
                      </div>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          {/if}
        </div>

        <!-- Pagination -->
        {#if jobsTotal > jobsLimit}
          <div class="flex items-center justify-between px-6 py-3 border-t shrink-0 text-sm text-muted">
            <span>Showing {jobsOffset + 1}–{Math.min(jobsOffset + jobsLimit, jobsTotal)} of {jobsTotal}</span>
            <div class="flex gap-2">
              <Button variant="secondary" size="sm" onclick={prevJobsPage} disabled={jobsOffset === 0}>
                <ChevronLeft size={14} /> Prev
              </Button>
              <Button variant="secondary" size="sm" onclick={nextJobsPage} disabled={jobsOffset + jobsLimit >= jobsTotal}>
                Next <ChevronRight size={14} />
              </Button>
            </div>
          </div>
        {/if}
      </div>
    {/if}

    <!-- ═══ RUNS TAB ═══ -->
    {#if activeTab === 'runs'}
      <div class="flex flex-col h-full overflow-hidden">
        <div class="flex gap-3 px-6 py-3 shrink-0 border-b items-center">
          <select bind:value={runsFilterStatus} onchange={() => { runsOffset = 0; loadRuns(); }} class="input-base text-sm">
            <option value="">All Status</option>
            <option value="running">Running</option>
            <option value="success">Success</option>
            <option value="failed">Failed</option>
            <option value="pending">Pending</option>
            <option value="timeout">Timeout</option>
            <option value="cancelled">Cancelled</option>
          </select>
          <span class="text-xs text-muted ml-auto">{runsTotal} total runs</span>
        </div>

        <div class="flex-1 overflow-auto">
          {#if runsLoading}
            <div class="flex items-center justify-center h-32"><RefreshCw size={20} class="animate-spin text-muted" /></div>
          {:else if runs.length === 0}
            <div class="flex flex-col items-center justify-center h-48 gap-2 text-muted">
              <Activity size={40} class="opacity-30" />
              <p class="font-medium">No run history yet</p>
            </div>
          {:else}
            <table class="w-full text-sm">
              <thead class="sticky top-0 bg-surface z-10">
                <tr class="border-b">
                  <th class="text-left px-6 py-3 text-[11px] font-semibold uppercase tracking-wider text-muted">Job</th>
                  <th class="text-left px-4 py-3 text-[11px] font-semibold uppercase tracking-wider text-muted">Status</th>
                  <th class="text-left px-4 py-3 text-[11px] font-semibold uppercase tracking-wider text-muted">Triggered By</th>
                  <th class="text-left px-4 py-3 text-[11px] font-semibold uppercase tracking-wider text-muted">Started</th>
                  <th class="text-left px-4 py-3 text-[11px] font-semibold uppercase tracking-wider text-muted">Duration</th>
                  <th class="text-left px-4 py-3 text-[11px] font-semibold uppercase tracking-wider text-muted">Output / Error</th>
                  <th class="px-4 py-3"></th>
                </tr>
              </thead>
              <tbody>
                {#each runs as run (run.id)}
                  <tr class="border-b border-border/50 hover:bg-surface-hover transition-colors">
                    <td class="px-6 py-3">
                      <div class="font-medium">{run.job_name || run.job_id.substring(0,8)}</div>
                      <div class="text-[10px] font-mono text-muted">{run.job_id.substring(0,8)}</div>
                    </td>
                    <td class="px-4 py-3"><span class="status-pill {run.status}">{run.status}</span></td>
                    <td class="px-4 py-3">
                      <span class="text-[11px] font-mono px-2 py-0.5 rounded bg-surface-hover text-muted">{run.triggered_by}</span>
                      <span class="text-[10px] text-muted ml-1">#{run.attempt}</span>
                    </td>
                    <td class="px-4 py-3 text-xs text-muted font-mono">{run.started_at ? relTime(run.started_at) : '—'}</td>
                    <td class="px-4 py-3 text-xs font-mono text-muted">{fmtDuration(run.duration_ms)}</td>
                    <td class="px-4 py-3 max-w-xs">
                      {#if run.error_message}
                        <div class="text-[11px] text-red-400 font-mono truncate" title={run.error_message}>✗ {run.error_message.substring(0,60)}{run.error_message.length > 60 ? '...' : ''}</div>
                      {:else if run.output}
                        <div class="text-[11px] text-muted font-mono truncate" title={run.output}>{run.output.substring(0,60)}{run.output.length > 60 ? '...' : ''}</div>
                      {/if}
                    </td>
                    <td class="px-4 py-3">
                      <button class="p-1.5 rounded hover:bg-red-500/15 text-red-400/50 hover:text-red-400 transition-colors" title="Delete run" onclick={() => deleteRun(run.id)}>
                        <Trash2 size={13} />
                      </button>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          {/if}
        </div>

        {#if runsTotal > runsLimit}
          <div class="flex items-center justify-between px-6 py-3 border-t shrink-0 text-sm text-muted">
            <span>{runsTotal} total runs</span>
            <div class="flex gap-2">
              <Button variant="secondary" size="sm" onclick={prevRunsPage} disabled={runsOffset === 0}>
                <ChevronLeft size={14} /> Prev
              </Button>
              <Button variant="secondary" size="sm" onclick={nextRunsPage} disabled={runsOffset + runsLimit >= runsTotal}>
                Next <ChevronRight size={14} />
              </Button>
            </div>
          </div>
        {/if}
      </div>
    {/if}

    <!-- ═══ SETTINGS TAB ═══ -->
    {#if activeTab === 'settings'}
      <div class="overflow-auto h-full">
        <div class="max-w-4xl mx-auto px-6 py-6 space-y-6">

          {#if settingsLoading}
            <div class="flex items-center justify-center h-32"><RefreshCw size={20} class="animate-spin text-muted" /></div>
          {:else}

          <!-- Execution Settings -->
          <Card variant="filled" padding="md">
            <div class="flex items-center gap-2 mb-5 pb-3 border-b">
              <Zap size={16} class="text-accent" />
              <h3 class="font-semibold text-sm uppercase tracking-wider text-muted">Execution</h3>
            </div>
            <div class="grid grid-cols-2 gap-4">
              <div>
                <label class="settings-label">Max Concurrent Jobs</label>
                <input type="number" bind:value={cfg.max_concurrent_jobs} min="1" max="1000" class="input-base w-full" />
                <p class="settings-hint">Goroutines running jobs simultaneously</p>
              </div>
              <div>
                <label class="settings-label">Job Timeout (seconds)</label>
                <input type="number" bind:value={cfg.job_timeout} min="0" class="input-base w-full" />
                <p class="settings-hint">0 = no timeout. Applied per execution</p>
              </div>
              <div>
                <label class="settings-label">Scheduler Timezone</label>
                <input type="text" bind:value={cfg.timezone} placeholder="UTC" class="input-base w-full" />
                <p class="settings-hint">IANA timezone (e.g. America/New_York)</p>
              </div>
              <div>
                <label class="settings-label">Heartbeat Interval (seconds)</label>
                <input type="number" bind:value={cfg.heartbeat_interval} min="5" class="input-base w-full" />
              </div>
            </div>
            <div class="mt-4 space-y-3">
              <div class="flex items-center justify-between py-2 border-b border-border/50">
                <div>
                  <div class="text-sm font-medium">Singleton Mode</div>
                  <div class="text-[11px] text-muted">Prevent overlapping runs of the same job by default</div>
                </div>
                <button
                  class="w-10 h-5 rounded-full border transition-all relative {cfg.singleton_mode ? 'bg-accent/30 border-accent' : 'bg-surface-hover border-border'}"
                  onclick={() => cfg.singleton_mode = !cfg.singleton_mode}
                >
                  <span class="absolute top-0.5 left-0.5 w-4 h-4 rounded-full transition-transform {cfg.singleton_mode ? 'translate-x-5 bg-accent' : 'bg-muted'}"></span>
                </button>
              </div>
              <div class="flex items-center justify-between py-2">
                <div>
                  <div class="text-sm font-semibold text-amber-400">🛑 Globally Pause Scheduler</div>
                  <div class="text-[11px] text-muted">All jobs stop running until unpaused. Individual jobs stay intact.</div>
                </div>
                <button
                  class="w-10 h-5 rounded-full border transition-all relative {cfg.paused ? 'bg-amber-500/30 border-amber-500' : 'bg-surface-hover border-border'}"
                  onclick={() => cfg.paused = !cfg.paused}
                >
                  <span class="absolute top-0.5 left-0.5 w-4 h-4 rounded-full transition-transform {cfg.paused ? 'translate-x-5 bg-amber-400' : 'bg-muted'}"></span>
                </button>
              </div>
            </div>
          </Card>

          <!-- Retry Policy -->
          <Card variant="filled" padding="md">
            <div class="flex items-center gap-2 mb-5 pb-3 border-b">
              <RotateCcw size={16} class="text-accent" />
              <h3 class="font-semibold text-sm uppercase tracking-wider text-muted">Retry Policy</h3>
            </div>
            <div class="grid grid-cols-3 gap-4">
              <div>
                <label class="settings-label">Max Retries</label>
                <input type="number" bind:value={cfg.max_retries} min="0" max="100" class="input-base w-full" />
                <p class="settings-hint">Attempts before sending to DLQ</p>
              </div>
              <div>
                <label class="settings-label">Backoff Strategy</label>
                <select bind:value={cfg.retry_backoff} class="input-base w-full">
                  <option value="exponential">Exponential (recommended)</option>
                  <option value="linear">Linear</option>
                  <option value="fixed">Fixed</option>
                </select>
              </div>
              <div>
                <label class="settings-label">Base Retry Delay (seconds)</label>
                <input type="number" bind:value={cfg.retry_delay} min="1" class="input-base w-full" />
                <p class="settings-hint">Multiplied by backoff strategy</p>
              </div>
            </div>
          </Card>

          <!-- Queue & DLQ -->
          <Card variant="filled" padding="md">
            <div class="flex items-center gap-2 mb-5 pb-3 border-b">
              <ListChecks size={16} class="text-accent" />
              <h3 class="font-semibold text-sm uppercase tracking-wider text-muted">Async Queue & Dead-Letter Queue</h3>
            </div>
            <div class="grid grid-cols-2 gap-4">
              <div>
                <label class="settings-label">Worker Pool Size</label>
                <input type="number" bind:value={cfg.worker_pool_size} min="1" max="100" class="input-base w-full" />
                <p class="settings-hint">Concurrent queue consumer goroutines</p>
              </div>
              <div>
                <label class="settings-label">Queue Redis Key</label>
                <input type="text" bind:value={cfg.queue_key} class="input-base w-full font-mono text-sm" />
              </div>
              <div>
                <label class="settings-label">DLQ Redis Key</label>
                <input type="text" bind:value={cfg.dlq_key} class="input-base w-full font-mono text-sm" />
              </div>
              <div>
                <label class="settings-label">DLQ TTL (seconds)</label>
                <input type="number" bind:value={cfg.dlq_ttl} min="0" class="input-base w-full" />
                <p class="settings-hint">604800 = 7 days</p>
              </div>
            </div>
            <div class="mt-4 flex items-center justify-between py-2">
              <div>
                <div class="text-sm font-medium">Enable Dead-Letter Queue</div>
                <div class="text-[11px] text-muted">Send permanently failed jobs to DLQ for later inspection</div>
              </div>
              <button
                class="w-10 h-5 rounded-full border transition-all relative {cfg.dlq_enabled ? 'bg-accent/30 border-accent' : 'bg-surface-hover border-border'}"
                onclick={() => cfg.dlq_enabled = !cfg.dlq_enabled}
              >
                <span class="absolute top-0.5 left-0.5 w-4 h-4 rounded-full transition-transform {cfg.dlq_enabled ? 'translate-x-5 bg-accent' : 'bg-muted'}"></span>
              </button>
            </div>
          </Card>

          <!-- Logging -->
          <Card variant="filled" padding="md">
            <div class="flex items-center gap-2 mb-5 pb-3 border-b">
              <Timer size={16} class="text-accent" />
              <h3 class="font-semibold text-sm uppercase tracking-wider text-muted">Logging & Maintenance</h3>
            </div>
            <div class="grid grid-cols-2 gap-4">
              <div>
                <label class="settings-label">Log Retention Days</label>
                <input type="number" bind:value={cfg.log_retention_days} min="1" max="365" class="input-base w-full" />
                <p class="settings-hint">How long to keep job_runs records in DB</p>
              </div>
            </div>
          </Card>

          <!-- Save + Operations -->
          <div class="flex items-center justify-between pt-2">
            <div class="flex gap-2">
              <Button variant="secondary" size="sm" onclick={restartScheduler}>
                <RotateCcw size={14} /> Reload Config
              </Button>
            </div>
            <div class="flex gap-2">
              <Button variant="secondary" size="sm" onclick={loadSettings}>↺ Reset</Button>
              <Button variant="primary" onclick={saveSettings} disabled={settingsSaving}>
                {#if settingsSaving}
                  <RefreshCw size={14} class="animate-spin" /> Saving...
                {:else}
                  <CheckCircle size={14} /> Save Settings
                {/if}
              </Button>
            </div>
          </div>

          {/if}
        </div>
      </div>
    {/if}

  </div>
</div>

<!-- ═══════════════════════════════════════════════════════════════════════════
     CREATE / EDIT MODAL
══════════════════════════════════════════════════════════════════════════════ -->
<Modal bind:open={showCreateModal} title={editJobId ? 'Edit Job' : 'Create New Job'}>
  <div class="space-y-4 max-h-[70vh] overflow-y-auto pr-1">
    <div class="grid grid-cols-2 gap-3">
      <div class="col-span-2">
        <label class="settings-label">Job Name *</label>
        <input type="text" bind:value={form.name} placeholder="e.g. Daily Cleanup" class="input-base w-full" />
      </div>
      <div class="col-span-2">
        <label class="settings-label">Description</label>
        <input type="text" bind:value={form.description} placeholder="Optional" class="input-base w-full" />
      </div>
      <div>
        <label class="settings-label">Job Type *</label>
        <select bind:value={form.job_type} class="input-base w-full">
          <option value="">Select type...</option>
          {#each jobTypes as t}
            <option value={t}>{t}</option>
          {/each}
        </select>
      </div>
      <div>
        <label class="settings-label">Schedule Type *</label>
        <select bind:value={form.schedule_type} class="input-base w-full">
          <option value="cron">Cron Expression</option>
          <option value="interval">Fixed Interval</option>
          <option value="one_time">One-Time</option>
          <option value="manual">Manual Only</option>
        </select>
      </div>

      {#if form.schedule_type === 'cron'}
        <div class="col-span-2">
          <label class="settings-label">Cron Expression</label>
          <input type="text" bind:value={form.cron_expression} placeholder="*/5 * * * *" class="input-base w-full font-mono" />
          {#if cronPreview}<p class="text-[11px] text-emerald-400 mt-1">→ {cronPreview}</p>{/if}
          <p class="settings-hint mt-1">
            Quick:
            <button class="text-accent hover:underline font-mono text-[11px]" onclick={() => form.cron_expression = '*/5 * * * *'}>*/5 * * * *</button> ·
            <button class="text-accent hover:underline font-mono text-[11px]" onclick={() => form.cron_expression = '0 * * * *'}>0 * * * *</button> ·
            <button class="text-accent hover:underline font-mono text-[11px]" onclick={() => form.cron_expression = '0 0 * * *'}>0 0 * * *</button> ·
            <button class="text-accent hover:underline font-mono text-[11px]" onclick={() => form.cron_expression = '0 0 * * 0'}>0 0 * * 0</button>
          </p>
        </div>
      {/if}

      {#if form.schedule_type === 'interval'}
        <div class="col-span-2">
          <label class="settings-label">Interval (seconds)</label>
          <input type="number" bind:value={form.interval_seconds} min="1" class="input-base w-full" />
        </div>
      {/if}

      {#if form.schedule_type === 'one_time'}
        <div class="col-span-2">
          <label class="settings-label">Run At</label>
          <input type="datetime-local" bind:value={form.run_at} class="input-base w-full" />
        </div>
      {/if}

      <div class="col-span-2">
        <label class="settings-label">Payload (JSON)</label>
        <textarea bind:value={form.payload} rows="4" class="input-base w-full font-mono text-xs resize-none" placeholder={'{"key": "value"}'} ></textarea>
      </div>

      <div>
        <label class="settings-label">Max Retries</label>
        <input type="number" bind:value={form.max_retries} min="0" class="input-base w-full" />
      </div>
      <div>
        <label class="settings-label">Timeout (seconds)</label>
        <input type="number" bind:value={form.timeout_seconds} min="0" class="input-base w-full" />
      </div>
      <div>
        <label class="settings-label">Timezone</label>
        <input type="text" bind:value={form.timezone} class="input-base w-full" />
      </div>
      <div>
        <label class="settings-label">Tags (comma-separated)</label>
        <input type="text" bind:value={form.tags} placeholder="cleanup, daily" class="input-base w-full" />
      </div>

      <div class="flex items-center justify-between py-2">
        <span class="text-sm font-medium">Enabled</span>
        <button class="w-10 h-5 rounded-full border transition-all relative {form.is_enabled ? 'bg-accent/30 border-accent' : 'bg-surface-hover border-border'}" onclick={() => form.is_enabled = !form.is_enabled}>
          <span class="absolute top-0.5 left-0.5 w-4 h-4 rounded-full transition-transform {form.is_enabled ? 'translate-x-5 bg-accent' : 'bg-muted'}"></span>
        </button>
      </div>
      <div class="flex items-center justify-between py-2">
        <span class="text-sm font-medium">Singleton Mode</span>
        <button class="w-10 h-5 rounded-full border transition-all relative {form.is_singleton ? 'bg-accent/30 border-accent' : 'bg-surface-hover border-border'}" onclick={() => form.is_singleton = !form.is_singleton}>
          <span class="absolute top-0.5 left-0.5 w-4 h-4 rounded-full transition-transform {form.is_singleton ? 'translate-x-5 bg-accent' : 'bg-muted'}"></span>
        </button>
      </div>
    </div>
  </div>
  <div slot="footer" class="flex justify-end gap-2">
    <Button variant="secondary" onclick={() => showCreateModal = false}>Cancel</Button>
    <Button variant="primary" onclick={submitForm} disabled={formSaving}>
      {#if formSaving}<RefreshCw size={14} class="animate-spin" /> Saving...
      {:else}{editJobId ? 'Save Changes' : 'Create Job'}{/if}
    </Button>
  </div>
</Modal>

<!-- ═══════════════════════════════════════════════════════════════════════════
     DETAIL MODAL
══════════════════════════════════════════════════════════════════════════════ -->
<Modal bind:open={showDetailModal} title={detailJob?.name || 'Job Details'}>
  {#if detailLoading}
    <div class="flex justify-center py-10"><RefreshCw size={20} class="animate-spin text-muted" /></div>
  {:else if detailJob}
    <div class="flex gap-2 mb-4 flex-wrap">
      <Button variant="primary" size="sm" onclick={() => { triggerJob(detailJob.id, detailJob.name); }}>
        <Play size={13} /> Trigger Now
      </Button>
      {#if detailJob.is_enabled}
        <Button variant="secondary" size="sm" onclick={() => { pauseJob(detailJob.id); showDetailModal = false; }}>
          <Pause size={13} /> Pause
        </Button>
      {:else}
        <Button variant="secondary" size="sm" onclick={() => { resumeJob(detailJob.id); showDetailModal = false; }}>
          <Play size={13} /> Resume
        </Button>
      {/if}
      <Button variant="secondary" size="sm" onclick={() => { showDetailModal = false; openEdit(detailJob.id); }}>
        <Pencil size={13} /> Edit
      </Button>
      <Button variant="secondary" size="sm" onclick={() => { deleteJob(detailJob.id, detailJob.name); showDetailModal = false; }}>
        <Trash2 size={13} class="text-red-400" /> Delete
      </Button>
    </div>

    <div class="grid grid-cols-2 gap-4">
      <!-- Info -->
      <div class="space-y-1 text-sm">
        {#each [
          ['Type', detailJob.job_type],
          ['Schedule', scheduleLabel(detailJob)],
          ['Timezone', detailJob.timezone || 'UTC'],
          ['Max Retries', detailJob.max_retries],
          ['Timeout', detailJob.timeout_seconds + 's'],
          ['Singleton', detailJob.is_singleton ? '✓ Yes' : '✗ No'],
          ['Run Count', detailJob.run_count],
          ['Success', detailJob.success_count],
          ['Failures', detailJob.failure_count],
          ['Last Run', relTime(detailJob.last_run_at)],
          ['Created', detailJob.created_at?.substring(0,19)],
        ] as [k, v]}
          <div class="flex justify-between py-1.5 border-b border-border/40">
            <span class="text-muted text-xs">{k}</span>
            <span class="font-mono text-xs">{v}</span>
          </div>
        {/each}
        <div class="flex justify-between py-1.5">
          <span class="text-muted text-xs">Status</span>
          <span class="status-pill {detailJob.is_enabled ? 'enabled' : 'disabled'}">{detailJob.is_enabled ? 'enabled' : 'disabled'}</span>
        </div>
      </div>

      <!-- Recent Runs -->
      <div>
        <div class="text-[11px] uppercase font-bold tracking-wider text-muted mb-2">Recent Runs</div>
        {#if detailRuns.length === 0}
          <p class="text-sm text-muted text-center py-6">No runs yet</p>
        {:else}
          <div class="space-y-1.5 max-h-64 overflow-y-auto">
            {#each detailRuns as r}
              <div class="rounded-lg p-2.5 bg-surface-hover border-l-2 {r.status === 'success' ? 'border-emerald-500' : r.status === 'failed' ? 'border-red-500' : r.status === 'running' ? 'border-purple-500' : 'border-amber-500'}">
                <div class="flex items-center justify-between mb-1">
                  <span class="status-pill {r.status}">{r.status}</span>
                  <span class="text-[10px] text-muted">{fmtDuration(r.duration_ms)}</span>
                </div>
                <div class="text-[10px] text-muted font-mono">{r.started_at ? relTime(r.started_at) : '—'} via {r.triggered_by}</div>
                {#if r.output}<div class="text-[10px] text-muted mt-1 truncate">{r.output.substring(0,80)}</div>{/if}
                {#if r.error_message}<div class="text-[10px] text-red-400 mt-1 truncate">{r.error_message.substring(0,80)}</div>{/if}
              </div>
            {/each}
          </div>
        {/if}
      </div>
    </div>
  {/if}
</Modal>

<style>
  .input-base {
    background: var(--color-surface-hover, rgba(255,255,255,0.05));
    border: 1px solid var(--color-border, rgba(255,255,255,0.1));
    border-radius: 8px;
    padding: 8px 12px;
    color: var(--color-foreground, #fff);
    font-size: 13px;
    font-family: inherit;
    outline: none;
    transition: border-color 0.15s;
  }

  .input-base:focus {
    border-color: var(--color-accent, #f97316);
    box-shadow: 0 0 0 2px rgba(249, 115, 22, 0.15);
  }

  select.input-base option {
    background: #1a1a2e;
    color: #fff;
  }

  .settings-label {
    display: block;
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--color-muted, #888);
    margin-bottom: 5px;
  }

  .settings-hint {
    font-size: 11px;
    color: var(--color-muted, #666);
    margin-top: 4px;
  }

  .status-pill {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 2px 8px;
    border-radius: 20px;
    font-size: 10px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    white-space: nowrap;
  }

  .status-pill.enabled  { background: rgba(16,185,129,0.12); color: #10b981; border: 1px solid rgba(16,185,129,0.25); }
  .status-pill.disabled { background: rgba(255,255,255,0.06); color: #6b7280; border: 1px solid rgba(255,255,255,0.1); }
  .status-pill.success  { background: rgba(16,185,129,0.12); color: #10b981; border: 1px solid rgba(16,185,129,0.25); }
  .status-pill.failed   { background: rgba(239,68,68,0.12);  color: #ef4444; border: 1px solid rgba(239,68,68,0.25); }
  .status-pill.running  { background: rgba(139,92,246,0.12); color: #8b5cf6; border: 1px solid rgba(139,92,246,0.25); }
  .status-pill.pending  { background: rgba(245,158,11,0.12); color: #f59e0b; border: 1px solid rgba(245,158,11,0.25); }
  .status-pill.timeout  { background: rgba(239,68,68,0.12);  color: #ef4444; border: 1px solid rgba(239,68,68,0.25); }
  .status-pill.cancelled{ background: rgba(255,255,255,0.06); color: #6b7280; border: 1px solid rgba(255,255,255,0.1); }

  .tab-btn { background: none; border: none; cursor: pointer; margin-bottom: -1px; }

  :global(.text-accent) { color: var(--color-accent, #f97316); }
  :global(.bg-surface) { background: var(--color-surface, #111); }
  :global(.bg-surface-hover) { background: var(--color-surface-hover, rgba(255,255,255,0.05)); }
  :global(.border-border) { border-color: var(--color-border, rgba(255,255,255,0.1)); }
  :global(.text-muted) { color: var(--color-muted, #6b7280); }
</style>
