<script>
  import { onMount, onDestroy } from 'svelte';
  import {
    CalendarClock, Plus, RefreshCw, Play, Pause, Trash2, Pencil,
    Settings2, Activity, Clock, CheckCircle, XCircle,
    RotateCcw, ChevronLeft, ChevronRight, Search, Zap,
    ListChecks, Timer, TriangleAlert, Layers, Box,
    ArrowRight, Cpu, Database
  } from '@lucide/svelte';
  import { appState } from '$lib/state.svelte.js';
  import Button from '$lib/components/Button.svelte';
  import Card from '$lib/components/Card.svelte';
  import Modal from '$lib/components/Modal.svelte';

  // ─── API ────────────────────────────────────────────────────────────────────
  async function api(method, path, body) {
    const res = await fetch('/api/v1/admin' + path, {
      method,
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${appState.getAdminKey()}`,
      },
      body: body ? JSON.stringify(body) : undefined,
    });
    if (res.status === 204) return null;
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || data.message || res.statusText);
    return data;
  }

  // ─── Tab ────────────────────────────────────────────────────────────────────
  let activeTab = $state('jobs');

  // ─── Stats ──────────────────────────────────────────────────────────────────
  let stats = $state(null);
  async function loadStats() {
    try { stats = await api('GET', '/scheduler/stats'); } catch {}
  }

  // ─── Job Types ──────────────────────────────────────────────────────────────
  let jobTypes = $state([]);
  async function loadTypes() {
    try { jobTypes = await api('GET', '/scheduler/types') || []; } catch {}
  }

  // ─── Jobs ───────────────────────────────────────────────────────────────────
  let jobs = $state([]);
  let jobsTotal = $state(0);
  const JOBS_LIMIT = 20;
  let jobsOffset = $state(0);
  let jobsLoading = $state(false);
  let jobSearch = $state('');
  let jobFilterStatus = $state('');
  let jobFilterType = $state('');
  let searchTimer;

  async function loadJobs() {
    jobsLoading = true;
    try {
      const p = new URLSearchParams({ limit: JOBS_LIMIT, offset: jobsOffset });
      if (jobSearch) p.set('search', jobSearch);
      if (jobFilterStatus) p.set('enabled', jobFilterStatus === 'enabled' ? 'true' : 'false');
      if (jobFilterType) p.set('job_type', jobFilterType);
      const d = await api('GET', '/jobs?' + p);
      jobs = d.data || [];
      jobsTotal = d.total || 0;
    } catch(e) { appState.addToast('error', e.message); }
    finally { jobsLoading = false; }
  }

  function onSearch() {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => { jobsOffset = 0; loadJobs(); }, 280);
  }

  // ─── Runs ───────────────────────────────────────────────────────────────────
  let runs = $state([]);
  let runsTotal = $state(0);
  const RUNS_LIMIT = 50;
  let runsOffset = $state(0);
  let runsLoading = $state(false);
  let runsFilterStatus = $state('');

  async function loadRuns() {
    runsLoading = true;
    try {
      const p = new URLSearchParams({ limit: RUNS_LIMIT, offset: runsOffset });
      if (runsFilterStatus) p.set('status', runsFilterStatus);
      const d = await api('GET', '/jobs/runs?' + p);
      runs = d.data || [];
      runsTotal = d.total || 0;
    } catch(e) {} finally { runsLoading = false; }
  }

  async function deleteRun(id) {
    try { await api('DELETE', '/jobs/runs/' + id); loadRuns(); appState.addToast('success', 'Run deleted'); }
    catch(e) { appState.addToast('error', e.message); }
  }

  // ─── Settings ───────────────────────────────────────────────────────────────
  let settingsLoading = $state(false);
  let settingsSaving = $state(false);
  let cfg = $state({
    max_concurrent_jobs: 10, job_timeout: 300, timezone: 'UTC',
    singleton_mode: true, paused: false, max_retries: 3,
    retry_backoff: 'exponential', retry_delay: 30, worker_pool_size: 5,
    queue_key: 'cag:jobs:queue', dlq_enabled: true, dlq_key: 'cag:jobs:dlq',
    dlq_ttl: 604800, log_retention_days: 30, heartbeat_interval: 30,
  });

  async function loadSettings() {
    settingsLoading = true;
    try {
      const rows = await api('GET', '/scheduler/settings') || [];
      const kv = {};
      rows.forEach(r => kv[r.key] = r.value);
      const g = (k, d) => kv[k] !== undefined ? kv[k] : String(d);
      cfg = {
        max_concurrent_jobs: +g('max_concurrent_jobs', 10),
        job_timeout: +g('job_timeout', 300),
        timezone: g('timezone', 'UTC'),
        singleton_mode: g('singleton_mode', 'true') === 'true',
        paused: g('paused', 'false') === 'true',
        max_retries: +g('max_retries', 3),
        retry_backoff: g('retry_backoff', 'exponential'),
        retry_delay: +g('retry_delay', 30),
        worker_pool_size: +g('worker_pool_size', 5),
        queue_key: g('queue_key', 'cag:jobs:queue'),
        dlq_enabled: g('dlq_enabled', 'true') === 'true',
        dlq_key: g('dlq_key', 'cag:jobs:dlq'),
        dlq_ttl: +g('dlq_ttl', 604800),
        log_retention_days: +g('log_retention_days', 30),
        heartbeat_interval: +g('heartbeat_interval', 30),
      };
    } catch(e) { appState.addToast('error', e.message); }
    finally { settingsLoading = false; }
  }

  async function saveSettings() {
    settingsSaving = true;
    try {
      await api('PUT', '/scheduler/settings', { settings: cfg });
      appState.addToast('success', 'Settings saved & live-reloaded');
      loadStats();
    } catch(e) { appState.addToast('error', e.message); }
    finally { settingsSaving = false; }
  }

  // ─── Job Actions ─────────────────────────────────────────────────────────────
  async function triggerJob(id, name) {
    try {
      const r = await api('POST', '/jobs/' + id + '/trigger');
      appState.addToast('success', `▶ "${name}" triggered`);
      setTimeout(loadJobs, 1200);
    } catch(e) { appState.addToast('error', e.message); }
  }

  async function pauseJob(id) {
    try { await api('POST', '/jobs/' + id + '/pause'); appState.addToast('info', 'Job paused'); loadJobs(); }
    catch(e) { appState.addToast('error', e.message); }
  }

  async function resumeJob(id) {
    try { await api('POST', '/jobs/' + id + '/resume'); appState.addToast('success', 'Job resumed'); loadJobs(); }
    catch(e) { appState.addToast('error', e.message); }
  }

  async function deleteJob(id, name) {
    if (!confirm(`Delete "${name}" and all its run history?`)) return;
    try { await api('DELETE', '/jobs/' + id); appState.addToast('success', `Deleted "${name}"`); loadJobs(); loadStats(); }
    catch(e) { appState.addToast('error', e.message); }
  }

  // ─── Create/Edit Modal ───────────────────────────────────────────────────────
  let showJobModal = $state(false);
  let editJobId = $state(null);
  let formSaving = $state(false);
  let form = $state({
    name: '', description: '', job_type: '', schedule_type: 'cron',
    cron_expression: '', interval_seconds: 300, run_at: '',
    payload: '{}', max_retries: 3, timeout_seconds: 300,
    timezone: 'UTC', tags: '', is_enabled: true, is_singleton: true,
  });

  function openCreate() {
    editJobId = null;
    form = { name:'', description:'', job_type:'', schedule_type:'cron',
      cron_expression:'', interval_seconds:300, run_at:'',
      payload:'{}', max_retries:3, timeout_seconds:300,
      timezone:'UTC', tags:'', is_enabled:true, is_singleton:true };
    showJobModal = true;
  }

  async function openEdit(id) {
    editJobId = id;
    try {
      const j = await api('GET', '/jobs/' + id);
      form = { name:j.name, description:j.description||'', job_type:j.job_type,
        schedule_type:j.schedule_type, cron_expression:j.cron_expression||'',
        interval_seconds:j.interval_seconds||300, run_at:'',
        payload:JSON.stringify(j.payload||{},null,2), max_retries:j.max_retries,
        timeout_seconds:j.timeout_seconds, timezone:j.timezone||'UTC',
        tags:(j.tags||[]).join(', '), is_enabled:j.is_enabled, is_singleton:j.is_singleton };
      showJobModal = true;
    } catch(e) { appState.addToast('error', e.message); }
  }

  async function submitForm() {
    if (!form.name.trim()) { appState.addToast('error', 'Job name is required'); return; }
    if (!form.job_type) { appState.addToast('error', 'Select a job type'); return; }
    formSaving = true;
    try {
      let payload = {};
      try { payload = JSON.parse(form.payload || '{}'); }
      catch { appState.addToast('error', 'Payload must be valid JSON'); formSaving = false; return; }

      const body = {
        name: form.name.trim(), description: form.description,
        job_type: form.job_type, schedule_type: form.schedule_type,
        cron_expression: form.cron_expression,
        interval_seconds: +form.interval_seconds || 0,
        run_at: form.run_at || undefined,
        payload, max_retries: +form.max_retries,
        timeout_seconds: +form.timeout_seconds,
        timezone: form.timezone,
        tags: form.tags.split(',').map(t=>t.trim()).filter(Boolean),
        is_enabled: form.is_enabled, is_singleton: form.is_singleton,
      };

      if (editJobId) { await api('PUT', '/jobs/' + editJobId, body); appState.addToast('success', 'Job updated'); }
      else { await api('POST', '/jobs', body); appState.addToast('success', 'Job created'); }

      showJobModal = false;
      loadJobs(); loadStats();
    } catch(e) { appState.addToast('error', e.message); }
    finally { formSaving = false; }
  }

  // ─── Detail Panel ────────────────────────────────────────────────────────────
  let showDetail = $state(false);
  let detailJob = $state(null);
  let detailRuns = $state([]);
  let detailRunsTotal = $state(0);
  let detailLoading = $state(false);

  async function openDetail(id) {
    showDetail = true;
    detailLoading = true;
    detailJob = null; detailRuns = [];
    try {
      const [j, r] = await Promise.all([
        api('GET', '/jobs/' + id),
        api('GET', `/jobs/${id}/runs?limit=10`)
      ]);
      detailJob = j;
      detailRuns = r.data || [];
      detailRunsTotal = r.total || 0;
    } catch(e) { appState.addToast('error', e.message); }
    finally { detailLoading = false; }
  }

  // ─── Cron quick-fill ─────────────────────────────────────────────────────────
  const CRON_PRESETS = [
    { label: 'Every 5 min', expr: '*/5 * * * *' },
    { label: 'Every 15 min', expr: '*/15 * * * *' },
    { label: 'Every hour', expr: '0 * * * *' },
    { label: 'Every 6 hours', expr: '0 */6 * * *' },
    { label: 'Daily midnight', expr: '0 0 * * *' },
    { label: 'Every Sunday', expr: '0 0 * * 0' },
  ];
  const CRON_DESC = {
    '*/1 * * * *':'every minute','*/5 * * * *':'every 5 min',
    '*/15 * * * *':'every 15 min','*/30 * * * *':'every 30 min',
    '0 * * * *':'every hour','0 */2 * * *':'every 2h',
    '0 */6 * * *':'every 6h','0 */12 * * *':'every 12h',
    '0 0 * * *':'daily at midnight','0 0 * * 0':'every Sunday','0 0 1 * *':'1st of month',
  };
  let cronDesc = $derived(CRON_DESC[form.cron_expression] || '');

  // ─── Helpers ─────────────────────────────────────────────────────────────────
  function schedLabel(j) {
    if (!j) return '';
    if (j.schedule_type === 'cron') return j.cron_expression || '—';
    if (j.schedule_type === 'interval') return j.interval_seconds ? `every ${j.interval_seconds}s` : '—';
    if (j.schedule_type === 'one_time') return 'one-time';
    return 'manual';
  }

  function relTime(iso) {
    if (!iso) return '—';
    const diff = Date.now() - new Date(iso);
    const a = Math.abs(diff), f = diff < 0;
    if (a < 60000) return f ? 'soon' : 'just now';
    if (a < 3600000) return (f?'in ':'') + Math.floor(a/60000) + 'm' + (f?'':' ago');
    if (a < 86400000) return (f?'in ':'') + Math.floor(a/3600000) + 'h' + (f?'':' ago');
    return (f?'in ':'') + Math.floor(a/86400000) + 'd' + (f?'':' ago');
  }

  function fmtDuration(ms) {
    if (!ms) return '—';
    if (ms < 1000) return ms + 'ms';
    if (ms < 60000) return (ms/1000).toFixed(1) + 's';
    return Math.floor(ms/60000) + 'm ' + Math.floor((ms%60000)/1000) + 's';
  }

  function successRate(j) {
    if (!j.run_count) return 0;
    return Math.round((j.success_count / j.run_count) * 100);
  }

  // ─── Lifecycle ───────────────────────────────────────────────────────────────
  let refreshInterval;
  onMount(() => {
    loadStats(); loadJobs(); loadTypes();
    refreshInterval = setInterval(() => {
      loadStats();
      if (activeTab === 'jobs') loadJobs();
      else if (activeTab === 'runs') loadRuns();
    }, 10000);
  });
  onDestroy(() => clearInterval(refreshInterval));

  function switchTab(t) {
    activeTab = t;
    if (t === 'runs' && !runs.length) loadRuns();
    if (t === 'settings' && !cfg.timezone) loadSettings();
    else if (t === 'settings') loadSettings();
  }
</script>

<!-- ══════════════════════════════════════════════════════════════════
     FULL PAGE LAYOUT
═══════════════════════════════════════════════════════════════════ -->
<div class="page-root">

  <!-- ── PAGE HEADER ──────────────────────────────────────────────── -->
  <div class="page-header">
    <div class="page-header-left">
      <div class="page-icon">
        <CalendarClock size={20} />
      </div>
      <div>
        <h1 class="page-title">Job Scheduler</h1>
        <p class="page-subtitle">Scheduled tasks · async queues · execution history</p>
      </div>
    </div>
    <div class="page-header-right">
      {#if stats?.scheduler_paused}
        <div class="status-badge-paused">
          <Pause size={12} />
          Scheduler Paused
        </div>
      {:else}
        <div class="status-badge-live">
          <span class="live-dot"></span>
          Live
        </div>
      {/if}
      <button class="icon-btn" onclick={() => { loadStats(); loadJobs(); }} title="Refresh">
        <RefreshCw size={15} />
      </button>
      <button class="primary-btn" onclick={openCreate}>
        <Plus size={15} />
        New Job
      </button>
    </div>
  </div>

  <!-- ── STATS STRIP ───────────────────────────────────────────────── -->
  {#if stats}
    <div class="stats-strip">
      <div class="stat-tile stat-tile--primary">
        <div class="stat-tile-top">
          <span class="stat-tile-label">Total Jobs</span>
          <Box size={14} class="stat-tile-icon" />
        </div>
        <div class="stat-tile-value">{stats.total_jobs ?? 0}</div>
        <div class="stat-tile-sub">{stats.enabled_jobs ?? 0} enabled</div>
      </div>
      <div class="stat-tile stat-tile--purple">
        <div class="stat-tile-top">
          <span class="stat-tile-label">Running</span>
          <Cpu size={14} class="stat-tile-icon" />
        </div>
        <div class="stat-tile-value">{stats.running_24h ?? 0}</div>
        <div class="stat-tile-sub">active now</div>
      </div>
      <div class="stat-tile stat-tile--amber">
        <div class="stat-tile-top">
          <span class="stat-tile-label">Pending</span>
          <Clock size={14} class="stat-tile-icon" />
        </div>
        <div class="stat-tile-value">{stats.pending_24h ?? 0}</div>
        <div class="stat-tile-sub">in queue</div>
      </div>
      <div class="stat-tile stat-tile--green">
        <div class="stat-tile-top">
          <span class="stat-tile-label">Completed</span>
          <CheckCircle size={14} class="stat-tile-icon" />
        </div>
        <div class="stat-tile-value">{stats.completed_24h ?? 0}</div>
        <div class="stat-tile-sub">last 24h</div>
        {#if (stats.completed_24h + stats.failed_24h) > 0}
          <div class="stat-tile-bar">
            <div class="stat-tile-bar-fill" style="width:{Math.round(stats.completed_24h/(stats.completed_24h+stats.failed_24h)*100)}%;background:#10b981"></div>
          </div>
        {/if}
      </div>
      <div class="stat-tile stat-tile--red">
        <div class="stat-tile-top">
          <span class="stat-tile-label">Failed</span>
          <XCircle size={14} class="stat-tile-icon" />
        </div>
        <div class="stat-tile-value">{stats.failed_24h ?? 0}</div>
        <div class="stat-tile-sub">last 24h</div>
        {#if (stats.completed_24h + stats.failed_24h) > 0}
          <div class="stat-tile-bar">
            <div class="stat-tile-bar-fill" style="width:{Math.round(stats.failed_24h/(stats.completed_24h+stats.failed_24h)*100)}%;background:#ef4444"></div>
          </div>
        {/if}
      </div>
      <div class="stat-tile stat-tile--blue">
        <div class="stat-tile-top">
          <span class="stat-tile-label">Queue / DLQ</span>
          <Database size={14} class="stat-tile-icon" />
        </div>
        <div class="stat-tile-value">{stats.queue_depth ?? 0}</div>
        <div class="stat-tile-sub">{stats.dlq_depth ?? 0} dead-letter</div>
      </div>
    </div>
  {/if}

  <!-- ── TABS ─────────────────────────────────────────────────────── -->
  <div class="tabs-bar">
    <button class="tab {activeTab==='jobs' ? 'tab--active':''}" onclick={() => switchTab('jobs')}>
      <Layers size={14} /> Jobs
    </button>
    <button class="tab {activeTab==='runs' ? 'tab--active':''}" onclick={() => switchTab('runs')}>
      <Activity size={14} /> Run History
    </button>
    <button class="tab {activeTab==='settings' ? 'tab--active':''}" onclick={() => switchTab('settings')}>
      <Settings2 size={14} /> Settings
    </button>
  </div>

  <!-- ── TAB CONTENT ───────────────────────────────────────────────── -->
  <div class="tab-content">

    <!-- ════ JOBS TAB ════ -->
    {#if activeTab === 'jobs'}
      <div class="jobs-layout">

        <!-- Filter bar -->
        <div class="filter-bar">
          <div class="search-wrap">
            <Search size={14} class="search-icon" />
            <input
              type="text"
              placeholder="Search by name, type, description…"
              bind:value={jobSearch}
              oninput={onSearch}
              class="search-input"
            />
          </div>
          <select bind:value={jobFilterStatus} onchange={() => { jobsOffset=0; loadJobs(); }} class="filter-select">
            <option value="">All Status</option>
            <option value="enabled">Enabled</option>
            <option value="disabled">Disabled</option>
          </select>
          <select bind:value={jobFilterType} onchange={() => { jobsOffset=0; loadJobs(); }} class="filter-select">
            <option value="">All Types</option>
            {#each jobTypes as t}<option value={t}>{t}</option>{/each}
          </select>
          <span class="filter-count">{jobsTotal} job{jobsTotal !== 1 ? 's' : ''}</span>
        </div>

        <!-- Jobs Table -->
        {#if jobsLoading && !jobs.length}
          <div class="loading-center">
            <RefreshCw size={22} class="spin" />
            <span>Loading jobs…</span>
          </div>
        {:else if !jobs.length}
          <div class="empty-state">
            <div class="empty-icon"><CalendarClock size={36} /></div>
            <h3>No jobs yet</h3>
            <p>Create your first scheduled job to get started</p>
            <button class="primary-btn" onclick={openCreate}><Plus size={14} /> Create Job</button>
          </div>
        {:else}
          <div class="table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Type</th>
                  <th>Schedule</th>
                  <th>Last Run</th>
                  <th>Success Rate</th>
                  <th>Status</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {#each jobs as job (job.id)}
                  <tr class="data-row" onclick={() => openDetail(job.id)}>
                    <td>
                      <div class="job-name">{job.name}</div>
                      {#if job.description}
                        <div class="job-desc">{job.description}</div>
                      {/if}
                      {#if job.tags?.length}
                        <div class="tag-row">
                          {#each (job.tags||[]).slice(0,3) as tag}
                            <span class="tag">{tag}</span>
                          {/each}
                        </div>
                      {/if}
                    </td>
                    <td>
                      <span class="type-badge">{job.job_type}</span>
                    </td>
                    <td>
                      <span class="schedule-badge">{schedLabel(job)}</span>
                    </td>
                    <td>
                      <div class="last-run-time">{relTime(job.last_run_at)}</div>
                      {#if job.last_run_status}
                        <span class="status-dot status-dot--{job.last_run_status}">{job.last_run_status}</span>
                      {/if}
                    </td>
                    <td>
                      <div class="rate-wrap">
                        <div class="rate-bar">
                          <div class="rate-fill {successRate(job) < 50 ? 'rate-fill--bad' : successRate(job) < 80 ? 'rate-fill--med' : 'rate-fill--good'}" style="width:{successRate(job)}%"></div>
                        </div>
                        <span class="rate-pct">{successRate(job)}%</span>
                      </div>
                      <div class="run-counts">
                        <span class="cnt-ok">{job.success_count}✓</span>
                        <span class="cnt-err">{job.failure_count}✗</span>
                        <span class="cnt-tot">{job.run_count} total</span>
                      </div>
                    </td>
                    <td>
                      <span class="pill pill--{job.is_enabled ? 'enabled' : 'disabled'}">
                        {job.is_enabled ? 'Enabled' : 'Disabled'}
                      </span>
                    </td>
                    <td onclick={(e)=>e.stopPropagation()}>
                      <div class="row-actions">
                        <button class="act-btn act-btn--green" title="Trigger now" onclick={() => triggerJob(job.id, job.name)}>
                          <Play size={13} />
                        </button>
                        {#if job.is_enabled}
                          <button class="act-btn act-btn--amber" title="Pause" onclick={() => pauseJob(job.id)}>
                            <Pause size={13} />
                          </button>
                        {:else}
                          <button class="act-btn act-btn--green" title="Resume" onclick={() => resumeJob(job.id)}>
                            <Play size={13} />
                          </button>
                        {/if}
                        <button class="act-btn act-btn--blue" title="Edit" onclick={() => openEdit(job.id)}>
                          <Pencil size={13} />
                        </button>
                        <button class="act-btn act-btn--red" title="Delete" onclick={() => deleteJob(job.id, job.name)}>
                          <Trash2 size={13} />
                        </button>
                      </div>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>

          <!-- Pagination -->
          {#if jobsTotal > JOBS_LIMIT}
            <div class="pagination">
              <span class="pag-info">Showing {jobsOffset+1}–{Math.min(jobsOffset+JOBS_LIMIT, jobsTotal)} of {jobsTotal}</span>
              <div class="pag-btns">
                <button class="pag-btn" disabled={jobsOffset===0} onclick={() => { jobsOffset=Math.max(0,jobsOffset-JOBS_LIMIT); loadJobs(); }}>
                  <ChevronLeft size={14} /> Prev
                </button>
                <button class="pag-btn" disabled={jobsOffset+JOBS_LIMIT>=jobsTotal} onclick={() => { jobsOffset+=JOBS_LIMIT; loadJobs(); }}>
                  Next <ChevronRight size={14} />
                </button>
              </div>
            </div>
          {/if}
        {/if}
      </div>
    {/if}

    <!-- ════ RUNS TAB ════ -->
    {#if activeTab === 'runs'}
      <div class="runs-layout">
        <div class="filter-bar">
          <select bind:value={runsFilterStatus} onchange={() => { runsOffset=0; loadRuns(); }} class="filter-select">
            <option value="">All Status</option>
            <option value="running">Running</option>
            <option value="success">Success</option>
            <option value="failed">Failed</option>
            <option value="pending">Pending</option>
            <option value="timeout">Timeout</option>
          </select>
          <button class="ghost-btn" onclick={loadRuns}>
            <RefreshCw size={13} /> Refresh
          </button>
          <span class="filter-count">{runsTotal} runs</span>
        </div>

        {#if runsLoading && !runs.length}
          <div class="loading-center"><RefreshCw size={22} class="spin" /><span>Loading…</span></div>
        {:else if !runs.length}
          <div class="empty-state">
            <div class="empty-icon"><Activity size={36} /></div>
            <h3>No runs yet</h3>
            <p>Trigger a job to see execution history here</p>
          </div>
        {:else}
          <div class="table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th>Job</th>
                  <th>Status</th>
                  <th>Triggered By</th>
                  <th>Started</th>
                  <th>Duration</th>
                  <th>Output / Error</th>
                  <th>Host</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {#each runs as run (run.id)}
                  <tr class="data-row">
                    <td>
                      <div class="job-name">{run.job_name || '—'}</div>
                      <div class="job-desc mono">{run.job_id.substring(0,8)}…</div>
                    </td>
                    <td><span class="pill pill--{run.status}">{run.status}</span></td>
                    <td>
                      <span class="type-badge">{run.triggered_by}</span>
                      <span class="attempt-badge">#{run.attempt}</span>
                    </td>
                    <td class="mono text-sm">{relTime(run.started_at)}</td>
                    <td class="mono text-sm">{fmtDuration(run.duration_ms)}</td>
                    <td class="output-cell">
                      {#if run.error_message}
                        <span class="output-err" title={run.error_message}>{run.error_message.substring(0,60)}{run.error_message.length>60?'…':''}</span>
                      {:else if run.output}
                        <span class="output-ok" title={run.output}>{run.output.substring(0,60)}{run.output.length>60?'…':''}</span>
                      {:else}
                        <span class="output-none">—</span>
                      {/if}
                    </td>
                    <td class="mono text-sm">{run.host || '—'}</td>
                    <td>
                      <button class="act-btn act-btn--red" title="Delete run" onclick={() => deleteRun(run.id)}>
                        <Trash2 size={13} />
                      </button>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>

          {#if runsTotal > RUNS_LIMIT}
            <div class="pagination">
              <span class="pag-info">{runsTotal} total runs</span>
              <div class="pag-btns">
                <button class="pag-btn" disabled={runsOffset===0} onclick={() => { runsOffset=Math.max(0,runsOffset-RUNS_LIMIT); loadRuns(); }}>
                  <ChevronLeft size={14} /> Prev
                </button>
                <button class="pag-btn" disabled={runsOffset+RUNS_LIMIT>=runsTotal} onclick={() => { runsOffset+=RUNS_LIMIT; loadRuns(); }}>
                  Next <ChevronRight size={14} />
                </button>
              </div>
            </div>
          {/if}
        {/if}
      </div>
    {/if}

    <!-- ════ SETTINGS TAB ════ -->
    {#if activeTab === 'settings'}
      <div class="settings-layout">
        {#if settingsLoading}
          <div class="loading-center"><RefreshCw size={22} class="spin" /><span>Loading settings…</span></div>
        {:else}
          <div class="settings-cols">

            <!-- LEFT COLUMN -->
            <div class="settings-col">

              <!-- Execution -->
              <div class="settings-card">
                <div class="settings-card-header">
                  <Zap size={15} class="settings-card-icon" />
                  <span>Execution</span>
                </div>
                <div class="settings-card-body">
                  <div class="field-row">
                    <label class="field-label">Max Concurrent Jobs</label>
                    <input type="number" bind:value={cfg.max_concurrent_jobs} min="1" max="1000" class="field-input" />
                    <p class="field-hint">Goroutines running jobs in parallel</p>
                  </div>
                  <div class="field-row">
                    <label class="field-label">Job Timeout <span class="field-unit">seconds</span></label>
                    <input type="number" bind:value={cfg.job_timeout} min="0" class="field-input" />
                    <p class="field-hint">0 = no timeout. Applied to each execution</p>
                  </div>
                  <div class="field-row">
                    <label class="field-label">Timezone</label>
                    <input type="text" bind:value={cfg.timezone} placeholder="UTC" class="field-input" />
                    <p class="field-hint">IANA name — e.g. America/New_York</p>
                  </div>
                  <div class="field-row">
                    <label class="field-label">Heartbeat Interval <span class="field-unit">seconds</span></label>
                    <input type="number" bind:value={cfg.heartbeat_interval} min="5" class="field-input" />
                  </div>
                  <div class="toggle-row">
                    <div>
                      <div class="toggle-label">Singleton Mode</div>
                      <div class="toggle-hint">Prevent overlapping runs of the same job</div>
                    </div>
                    <label class="toggle-switch">
                      <input type="checkbox" bind:checked={cfg.singleton_mode} />
                      <span class="toggle-slider"></span>
                    </label>
                  </div>
                  <div class="toggle-row toggle-row--danger">
                    <div>
                      <div class="toggle-label">🛑 Globally Pause Scheduler</div>
                      <div class="toggle-hint">Stops all job execution. Individual jobs remain intact.</div>
                    </div>
                    <label class="toggle-switch">
                      <input type="checkbox" bind:checked={cfg.paused} />
                      <span class="toggle-slider toggle-slider--amber"></span>
                    </label>
                  </div>
                </div>
              </div>

              <!-- Retry Policy -->
              <div class="settings-card">
                <div class="settings-card-header">
                  <RotateCcw size={15} class="settings-card-icon" />
                  <span>Retry Policy</span>
                </div>
                <div class="settings-card-body">
                  <div class="field-row">
                    <label class="field-label">Max Retries</label>
                    <input type="number" bind:value={cfg.max_retries} min="0" max="100" class="field-input" />
                    <p class="field-hint">Attempts before moving to DLQ</p>
                  </div>
                  <div class="field-row">
                    <label class="field-label">Backoff Strategy</label>
                    <select bind:value={cfg.retry_backoff} class="field-input field-select">
                      <option value="exponential">Exponential (recommended)</option>
                      <option value="linear">Linear</option>
                      <option value="fixed">Fixed</option>
                    </select>
                    <p class="field-hint">How delay grows between retries</p>
                  </div>
                  <div class="field-row">
                    <label class="field-label">Base Retry Delay <span class="field-unit">seconds</span></label>
                    <input type="number" bind:value={cfg.retry_delay} min="1" class="field-input" />
                    <p class="field-hint">Multiplied by backoff on each attempt</p>
                  </div>
                </div>
              </div>

            </div>

            <!-- RIGHT COLUMN -->
            <div class="settings-col">

              <!-- Queue & DLQ -->
              <div class="settings-card">
                <div class="settings-card-header">
                  <ListChecks size={15} class="settings-card-icon" />
                  <span>Async Queue & Dead-Letter Queue</span>
                </div>
                <div class="settings-card-body">
                  <div class="field-row">
                    <label class="field-label">Worker Pool Size</label>
                    <input type="number" bind:value={cfg.worker_pool_size} min="1" max="100" class="field-input" />
                    <p class="field-hint">Concurrent Redis queue consumer goroutines</p>
                  </div>
                  <div class="field-row">
                    <label class="field-label">Queue Redis Key</label>
                    <input type="text" bind:value={cfg.queue_key} class="field-input mono" />
                  </div>
                  <div class="toggle-row">
                    <div>
                      <div class="toggle-label">Enable Dead-Letter Queue</div>
                      <div class="toggle-hint">Failed jobs are sent to DLQ for inspection</div>
                    </div>
                    <label class="toggle-switch">
                      <input type="checkbox" bind:checked={cfg.dlq_enabled} />
                      <span class="toggle-slider"></span>
                    </label>
                  </div>
                  <div class="field-row">
                    <label class="field-label">DLQ Redis Key</label>
                    <input type="text" bind:value={cfg.dlq_key} class="field-input mono" />
                  </div>
                  <div class="field-row">
                    <label class="field-label">DLQ TTL <span class="field-unit">seconds</span></label>
                    <input type="number" bind:value={cfg.dlq_ttl} min="0" class="field-input" />
                    <p class="field-hint">604800 = 7 days retention</p>
                  </div>
                </div>
              </div>

              <!-- Logging -->
              <div class="settings-card">
                <div class="settings-card-header">
                  <Timer size={15} class="settings-card-icon" />
                  <span>Logging & Maintenance</span>
                </div>
                <div class="settings-card-body">
                  <div class="field-row">
                    <label class="field-label">Log Retention <span class="field-unit">days</span></label>
                    <input type="number" bind:value={cfg.log_retention_days} min="1" max="365" class="field-input" />
                    <p class="field-hint">How long to keep job_runs records in the database</p>
                  </div>
                  <div class="settings-ops">
                    <button class="ghost-btn" onclick={async () => { try { await api('POST','/scheduler/restart'); appState.addToast('success','Config reloaded'); } catch(e) { appState.addToast('error',e.message); } }}>
                      <RotateCcw size={13} /> Reload Config
                    </button>
                  </div>
                </div>
              </div>

              <!-- Save actions -->
              <div class="settings-actions">
                <button class="ghost-btn" onclick={loadSettings}>↺ Reset</button>
                <button class="primary-btn" onclick={saveSettings} disabled={settingsSaving}>
                  {#if settingsSaving}
                    <RefreshCw size={14} class="spin" /> Saving…
                  {:else}
                    <CheckCircle size={14} /> Save Settings
                  {/if}
                </button>
              </div>

            </div>
          </div>
        {/if}
      </div>
    {/if}
  </div>
</div>

<!-- ══ DETAIL SIDE PANEL ══════════════════════════════════════════════ -->
{#if showDetail}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <div class="panel-backdrop" onclick={() => showDetail = false}></div>
  <div class="detail-panel">
    <div class="panel-header">
      <div>
        <div class="panel-title">{detailJob?.name ?? '…'}</div>
        {#if detailJob}
          <div class="panel-subtitle mono">{detailJob.job_type}</div>
        {/if}
      </div>
      <button class="icon-btn" onclick={() => showDetail = false}>✕</button>
    </div>

    {#if detailLoading}
      <div class="loading-center" style="flex:1"><RefreshCw size={20} class="spin" /></div>
    {:else if detailJob}
      <div class="panel-actions">
        <Button variant="success" size="sm" onclick={() => triggerJob(detailJob.id, detailJob.name)} title="Trigger now">
          <Play size={14} />
          <span>Trigger</span>
        </Button>
        {#if detailJob.is_enabled}
          <Button variant="secondary" size="sm" onclick={() => { pauseJob(detailJob.id); showDetail=false; }} title="Pause job">
            <Pause size={14} class="text-amber-500" />
            <span>Pause</span>
          </Button>
        {:else}
          <Button variant="success" size="sm" onclick={() => { resumeJob(detailJob.id); showDetail=false; }} title="Resume job">
            <Play size={14} />
            <span>Resume</span>
          </Button>
        {/if}
        <Button variant="outline" size="sm" onclick={() => { showDetail=false; openEdit(detailJob.id); }} title="Edit job">
          <Pencil size={14} />
          <span>Edit</span>
        </Button>
        <Button variant="danger" size="sm" onclick={() => { deleteJob(detailJob.id, detailJob.name); showDetail=false; }} title="Delete job">
          <Trash2 size={14} />
        </Button>
      </div>

      <div class="panel-body">
        <!-- Info Grid -->
        <div class="info-grid">
          {#each [
            ['Schedule', schedLabel(detailJob)],
            ['Timezone', detailJob.timezone||'UTC'],
            ['Max Retries', detailJob.max_retries],
            ['Timeout', detailJob.timeout_seconds+'s'],
            ['Singleton', detailJob.is_singleton ? 'Yes' : 'No'],
            ['Run Count', detailJob.run_count],
            ['Success', detailJob.success_count],
            ['Failures', detailJob.failure_count],
            ['Last Run', relTime(detailJob.last_run_at)],
          ] as [k, v]}
            <div class="info-kv">
              <span class="info-key">{k}</span>
              <span class="info-val mono">{v}</span>
            </div>
          {/each}
          <div class="info-kv">
            <span class="info-key">Status</span>
            <span class="pill pill--{detailJob.is_enabled?'enabled':'disabled'}">{detailJob.is_enabled?'Enabled':'Disabled'}</span>
          </div>
        </div>

        {#if detailJob.payload && Object.keys(detailJob.payload).length}
          <div class="payload-block">
            <div class="section-label">Payload</div>
            <pre class="payload-pre">{JSON.stringify(detailJob.payload, null, 2)}</pre>
          </div>
        {/if}

        <!-- Recent Runs -->
        <div class="section-label" style="margin-top:20px">
          Recent Runs
          <span class="run-total-badge">{detailRunsTotal} total</span>
        </div>
        {#if !detailRuns.length}
          <p class="empty-runs">No runs yet</p>
        {:else}
          <div class="run-list">
            {#each detailRuns as r}
              <div class="run-entry run-entry--{r.status}">
                <div class="run-entry-top">
                  <span class="pill pill--{r.status}">{r.status}</span>
                  <span class="run-dur">{fmtDuration(r.duration_ms)}</span>
                </div>
                <div class="run-meta">
                  {relTime(r.started_at)} · via {r.triggered_by} · attempt #{r.attempt}
                  {#if r.host} · {r.host}{/if}
                </div>
                {#if r.output}
                  <div class="run-out">{r.output.substring(0,100)}</div>
                {/if}
                {#if r.error_message}
                  <div class="run-err">{r.error_message.substring(0,100)}</div>
                {/if}
              </div>
            {/each}
          </div>
        {/if}
      </div>
    {/if}
  </div>
{/if}

<!-- ══ CREATE / EDIT MODAL ════════════════════════════════════════════ -->
<Modal bind:show={showJobModal} title={editJobId ? 'Edit Job' : 'Create New Job'} maxWidth="lg">
  <div class="modal-form">
    <div class="form-cols-2">
      <div class="form-group">
        <label>Job Name *</label>
        <input type="text" bind:value={form.name} placeholder="e.g. Daily Cleanup" class="input-box" />
      </div>
      <div class="form-group">
        <label>Description</label>
        <input type="text" bind:value={form.description} placeholder="Optional description" class="input-box" />
      </div>
    </div>
    <div class="form-cols-2">
      <div class="form-group">
        <label>Job Type *</label>
        <select bind:value={form.job_type} class="input-box">
          <option value="">— Select type —</option>
          {#each jobTypes as t}<option value={t}>{t}</option>{/each}
        </select>
      </div>
      <div class="form-group">
        <label>Schedule Type *</label>
        <select bind:value={form.schedule_type} class="input-box">
          <option value="cron">Cron Expression</option>
          <option value="interval">Fixed Interval</option>
          <option value="one_time">One-Time</option>
          <option value="manual">Manual Only</option>
        </select>
      </div>
    </div>

    {#if form.schedule_type === 'cron'}
      <div class="form-group">
        <label>Cron Expression</label>
        <input type="text" bind:value={form.cron_expression} placeholder="*/5 * * * *" class="input-box mono" />
        {#if cronDesc}<div class="cron-preview">→ {cronDesc}</div>{/if}
        <div class="cron-presets">
          {#each CRON_PRESETS as p}
            <button class="preset-chip" onclick={() => form.cron_expression = p.expr}>{p.label}</button>
          {/each}
        </div>
      </div>
    {/if}

    {#if form.schedule_type === 'interval'}
      <div class="form-group">
        <label>Interval (seconds)</label>
        <input type="number" bind:value={form.interval_seconds} min="1" class="input-box" />
      </div>
    {/if}

    {#if form.schedule_type === 'one_time'}
      <div class="form-group">
        <label>Run At</label>
        <input type="datetime-local" bind:value={form.run_at} class="input-box" />
      </div>
    {/if}

    <div class="form-group">
      <label>Payload (JSON)</label>
      <textarea bind:value={form.payload} rows="5" class="input-box mono"></textarea>
    </div>

    <div class="form-cols-3">
      <div class="form-group">
        <label>Max Retries</label>
        <input type="number" bind:value={form.max_retries} min="0" class="input-box" />
      </div>
      <div class="form-group">
        <label>Timeout (s)</label>
        <input type="number" bind:value={form.timeout_seconds} min="0" class="input-box" />
      </div>
      <div class="form-group">
        <label>Timezone</label>
        <input type="text" bind:value={form.timezone} class="input-box" />
      </div>
    </div>

    <div class="form-group">
      <label>Tags (comma-separated)</label>
      <input type="text" bind:value={form.tags} placeholder="maintenance, nightly, cleanup" class="input-box" />
    </div>

    <div class="form-toggles">
      <div class="toggle-row">
        <div>
          <div class="toggle-label">Enabled</div>
          <div class="toggle-hint">Job runs on its schedule</div>
        </div>
        <label class="toggle-switch">
          <input type="checkbox" bind:checked={form.is_enabled} />
          <span class="toggle-slider"></span>
        </label>
      </div>
      <div class="toggle-row">
        <div>
          <div class="toggle-label">Singleton Mode</div>
          <div class="toggle-hint">Prevent overlapping runs</div>
        </div>
        <label class="toggle-switch">
          <input type="checkbox" bind:checked={form.is_singleton} />
          <span class="toggle-slider"></span>
        </label>
      </div>
    </div>
  </div>

  {#snippet footer()}
    <button class="ghost-btn" onclick={() => showJobModal = false}>Cancel</button>
    <button class="primary-btn" onclick={submitForm} disabled={formSaving}>
      {#if formSaving}
        <RefreshCw size={14} class="spin" /> Saving…
      {:else}
        {editJobId ? 'Save Changes' : 'Create Job'}
      {/if}
    </button>
  {/snippet}
</Modal>

<!-- ══════════════════════════════════════════════════════════════════
     STYLES — all scoped, using the app's CSS variables
═══════════════════════════════════════════════════════════════════ -->
<style>
  /* ── Root layout ── */
  .page-root {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
    background-color: var(--main-bg);
    color: var(--text-primary);
  }

  /* ── Page Header ── */
  .page-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 18px 28px;
    border-bottom: 1px solid var(--border-color);
    flex-shrink: 0;
    gap: 16px;
  }
  .page-header-left {
    display: flex;
    align-items: center;
    gap: 14px;
  }
  .page-icon {
    width: 40px;
    height: 40px;
    border-radius: 12px;
    background: linear-gradient(135deg, rgba(249,115,22,0.2), rgba(249,115,22,0.05));
    border: 1px solid rgba(249,115,22,0.2);
    display: flex;
    align-items: center;
    justify-content: center;
    color: #f97316;
    flex-shrink: 0;
  }
  .page-title {
    font-size: 18px;
    font-weight: 700;
    margin: 0;
    letter-spacing: -0.01em;
  }
  .page-subtitle {
    font-size: 12px;
    color: var(--text-secondary);
    margin: 2px 0 0;
  }
  .page-header-right {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-shrink: 0;
  }

  /* ── Status Badges ── */
  .status-badge-live {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    font-weight: 600;
    color: #10b981;
    background: rgba(16,185,129,0.1);
    border: 1px solid rgba(16,185,129,0.25);
    padding: 5px 12px;
    border-radius: 999px;
  }
  .live-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: #10b981;
    animation: blink 1.8s ease-in-out infinite;
  }
  .status-badge-paused {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    font-weight: 600;
    color: #f59e0b;
    background: rgba(245,158,11,0.1);
    border: 1px solid rgba(245,158,11,0.25);
    padding: 5px 12px;
    border-radius: 999px;
  }

  @keyframes blink {
    0%,100% { opacity:1; } 50% { opacity:0.35; }
  }

  /* ── Buttons ── */
  .primary-btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 9px 18px;
    background: #f97316;
    color: #fff;
    border: none;
    border-radius: 10px;
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
    font-family: inherit;
    transition: background 0.15s, transform 0.1s;
  }
  .primary-btn:hover { background: #ea6c10; transform: translateY(-1px); }
  .primary-btn:active { transform: translateY(0); }
  .primary-btn:disabled { opacity: 0.5; cursor: not-allowed; transform: none; }

  .ghost-btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 8px 14px;
    background: transparent;
    color: var(--text-secondary);
    border: 1px solid var(--border-color);
    border-radius: 10px;
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    font-family: inherit;
    transition: all 0.15s;
  }
  .ghost-btn:hover { color: var(--text-primary); border-color: rgba(249,115,22,0.3); background: rgba(249,115,22,0.04); }

  .icon-btn {
    width: 34px;
    height: 34px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    border: 1px solid var(--border-color);
    border-radius: 8px;
    color: var(--text-secondary);
    cursor: pointer;
    transition: all 0.15s;
  }
  .icon-btn:hover { color: var(--text-primary); background: var(--item-hover); }

  /* ── Stats Strip ── */
  .stats-strip {
    display: grid;
    grid-template-columns: repeat(6, 1fr);
    gap: 1px;
    background: var(--border-color);
    border-bottom: 1px solid var(--border-color);
    flex-shrink: 0;
  }

  .stat-tile {
    background: var(--main-bg);
    padding: 16px 20px;
    display: flex;
    flex-direction: column;
    gap: 2px;
    position: relative;
    overflow: hidden;
  }
  .stat-tile::before {
    content: '';
    position: absolute;
    top: 0; left: 0; right: 0;
    height: 3px;
    border-radius: 0;
  }
  .stat-tile--primary::before { background: linear-gradient(90deg, #f97316, #fb923c); }
  .stat-tile--purple::before  { background: linear-gradient(90deg, #8b5cf6, #a78bfa); }
  .stat-tile--amber::before   { background: linear-gradient(90deg, #f59e0b, #fbbf24); }
  .stat-tile--green::before   { background: linear-gradient(90deg, #10b981, #34d399); }
  .stat-tile--red::before     { background: linear-gradient(90deg, #ef4444, #f87171); }
  .stat-tile--blue::before    { background: linear-gradient(90deg, #3b82f6, #60a5fa); }

  .stat-tile-top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 4px;
  }
  .stat-tile-label {
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-secondary);
  }
  :global(.stat-tile-icon) { color: var(--text-secondary); opacity: 0.5; }

  .stat-tile-value {
    font-size: 28px;
    font-weight: 800;
    letter-spacing: -0.02em;
    line-height: 1;
    margin: 2px 0;
  }
  .stat-tile--primary .stat-tile-value { color: #f97316; }
  .stat-tile--purple .stat-tile-value  { color: #8b5cf6; }
  .stat-tile--amber .stat-tile-value   { color: #f59e0b; }
  .stat-tile--green .stat-tile-value   { color: #10b981; }
  .stat-tile--red .stat-tile-value     { color: #ef4444; }
  .stat-tile--blue .stat-tile-value    { color: #3b82f6; }

  .stat-tile-sub {
    font-size: 11px;
    color: var(--text-secondary);
    opacity: 0.7;
  }
  .stat-tile-bar {
    height: 3px;
    background: var(--border-color);
    border-radius: 2px;
    margin-top: 8px;
    overflow: hidden;
  }
  .stat-tile-bar-fill {
    height: 100%;
    border-radius: 2px;
    transition: width 0.6s ease;
  }

  /* ── Tabs ── */
  .tabs-bar {
    display: flex;
    gap: 2px;
    padding: 0 28px;
    border-bottom: 1px solid var(--border-color);
    flex-shrink: 0;
    background: var(--main-bg);
  }
  .tab {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    padding: 12px 16px;
    font-size: 13px;
    font-weight: 500;
    color: var(--text-secondary);
    border: none;
    background: none;
    cursor: pointer;
    border-bottom: 2px solid transparent;
    margin-bottom: -1px;
    transition: all 0.15s;
    font-family: inherit;
  }
  .tab:hover { color: var(--text-primary); }
  .tab--active { color: #f97316; border-bottom-color: #f97316; font-weight: 600; }

  /* ── Tab content ── */
  .tab-content { flex: 1; overflow: hidden; display: flex; flex-direction: column; }

  /* ── Filter bar ── */
  .filter-bar {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 14px 28px;
    border-bottom: 1px solid var(--border-color);
    flex-shrink: 0;
    background: var(--sidebar-bg);
  }
  .search-wrap { position: relative; flex: 1; max-width: 340px; }
  :global(.search-icon) { position: absolute; left: 11px; top: 50%; transform: translateY(-50%); color: var(--text-secondary); pointer-events: none; }
  .search-input {
    width: 100%;
    padding: 9px 12px 9px 34px;
    background: var(--frame-bg);
    border: 1px solid var(--border-color);
    border-radius: 10px;
    color: var(--text-primary);
    font-size: 13px;
    font-family: inherit;
    outline: none;
    transition: border-color 0.15s;
  }
  .search-input:focus { border-color: #f97316; box-shadow: 0 0 0 3px rgba(249,115,22,0.1); }

  .filter-select {
    padding: 9px 12px;
    background: var(--frame-bg);
    border: 1px solid var(--border-color);
    border-radius: 10px;
    color: var(--text-primary);
    font-size: 13px;
    font-family: inherit;
    outline: none;
    cursor: pointer;
    transition: border-color 0.15s;
  }
  .filter-select:focus { border-color: #f97316; }

  .filter-count {
    margin-left: auto;
    font-size: 12px;
    color: var(--text-secondary);
    font-weight: 500;
  }

  /* ── Jobs layout ── */
  .jobs-layout, .runs-layout { display: flex; flex-direction: column; flex: 1; overflow: hidden; }

  /* ── Table ── */
  .table-wrap { flex: 1; overflow-y: auto; }
  .data-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 13px;
  }
  .data-table thead { position: sticky; top: 0; z-index: 5; background: var(--sidebar-bg); }
  .data-table thead th {
    padding: 11px 20px;
    text-align: left;
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-secondary);
    border-bottom: 1px solid var(--border-color);
    white-space: nowrap;
  }
  .data-row {
    border-bottom: 1px solid var(--border-color);
    transition: background 0.1s;
    cursor: pointer;
  }
  .data-row:hover { background: var(--item-hover); }
  .data-row td { padding: 12px 20px; vertical-align: middle; }

  .job-name { font-weight: 600; font-size: 14px; }
  .job-desc { font-size: 12px; color: var(--text-secondary); margin-top: 2px; }
  .tag-row { display: flex; flex-wrap: wrap; gap: 4px; margin-top: 4px; }
  .tag {
    font-size: 10px;
    padding: 1px 7px;
    border-radius: 999px;
    background: var(--item-hover);
    color: var(--text-secondary);
    border: 1px solid var(--border-color);
  }

  .type-badge {
    display: inline-block;
    font-size: 11px;
    font-family: ui-monospace, SFMono-Regular, monospace;
    font-weight: 500;
    padding: 3px 9px;
    border-radius: 7px;
    background: rgba(249,115,22,0.08);
    color: #f97316;
    border: 1px solid rgba(249,115,22,0.2);
  }
  .attempt-badge {
    display: inline-block;
    font-size: 11px;
    font-family: ui-monospace, SFMono-Regular, monospace;
    padding: 2px 6px;
    border-radius: 6px;
    background: var(--item-hover);
    color: var(--text-secondary);
    margin-left: 5px;
  }

  .schedule-badge {
    font-size: 11px;
    font-family: ui-monospace, SFMono-Regular, monospace;
    color: var(--text-secondary);
    background: var(--item-hover);
    padding: 3px 8px;
    border-radius: 7px;
    border: 1px solid var(--border-color);
  }

  .last-run-time { font-size: 12px; color: var(--text-secondary); }
  .status-dot {
    display: inline-block;
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    margin-top: 3px;
  }

  /* ── Pills ── */
  .pill {
    display: inline-flex;
    align-items: center;
    padding: 3px 10px;
    border-radius: 999px;
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    white-space: nowrap;
  }
  .pill--enabled   { background: rgba(16,185,129,0.1);  color: #10b981; border: 1px solid rgba(16,185,129,0.25); }
  .pill--disabled  { background: rgba(107,114,128,0.08); color: #6b7280; border: 1px solid rgba(107,114,128,0.2); }
  .pill--success   { background: rgba(16,185,129,0.1);  color: #10b981; border: 1px solid rgba(16,185,129,0.25); }
  .pill--failed    { background: rgba(239,68,68,0.1);   color: #ef4444; border: 1px solid rgba(239,68,68,0.25); }
  .pill--running   { background: rgba(139,92,246,0.1);  color: #8b5cf6; border: 1px solid rgba(139,92,246,0.25); }
  .pill--pending   { background: rgba(245,158,11,0.1);  color: #f59e0b; border: 1px solid rgba(245,158,11,0.25); }
  .pill--timeout   { background: rgba(239,68,68,0.1);   color: #ef4444; border: 1px solid rgba(239,68,68,0.25); }
  .pill--cancelled { background: rgba(107,114,128,0.08); color: #6b7280; border: 1px solid rgba(107,114,128,0.2); }

  /* ── Success Rate ── */
  .rate-wrap { display: flex; align-items: center; gap: 8px; margin-bottom: 4px; }
  .rate-bar { flex: 1; height: 5px; background: var(--border-color); border-radius: 3px; overflow: hidden; }
  .rate-fill { height: 100%; border-radius: 3px; transition: width 0.5s ease; }
  .rate-fill--good { background: #10b981; }
  .rate-fill--med  { background: #f59e0b; }
  .rate-fill--bad  { background: #ef4444; }
  .rate-pct { font-size: 11px; font-weight: 700; color: var(--text-secondary); white-space: nowrap; }
  .run-counts { display: flex; gap: 8px; font-size: 11px; }
  .cnt-ok  { color: #10b981; font-weight: 600; }
  .cnt-err { color: #ef4444; font-weight: 600; }
  .cnt-tot { color: var(--text-secondary); }

  /* ── Row actions ── */
  .row-actions { display: flex; gap: 4px; opacity: 0; transition: opacity 0.15s; }
  .data-row:hover .row-actions { opacity: 1; }
  .act-btn {
    width: 28px;
    height: 28px;
    border: none;
    border-radius: 7px;
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    font-family: inherit;
    transition: all 0.15s;
  }
  .act-btn--green { background: rgba(16,185,129,0.1);  color: #10b981; }
  .act-btn--green:hover { background: rgba(16,185,129,0.2); }
  .act-btn--amber { background: rgba(245,158,11,0.1);  color: #f59e0b; }
  .act-btn--amber:hover { background: rgba(245,158,11,0.2); }
  .act-btn--blue  { background: rgba(59,130,246,0.1);  color: #3b82f6; }
  .act-btn--blue:hover  { background: rgba(59,130,246,0.2); }
  .act-btn--red   { background: rgba(239,68,68,0.1);   color: #ef4444; }
  .act-btn--red:hover   { background: rgba(239,68,68,0.2); }

  /* ── Pagination ── */
  .pagination {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px 28px;
    border-top: 1px solid var(--border-color);
    flex-shrink: 0;
    background: var(--sidebar-bg);
  }
  .pag-info { font-size: 12px; color: var(--text-secondary); }
  .pag-btns { display: flex; gap: 6px; }
  .pag-btn {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 6px 12px;
    font-size: 12px;
    font-weight: 500;
    border: 1px solid var(--border-color);
    background: var(--frame-bg);
    color: var(--text-secondary);
    border-radius: 8px;
    cursor: pointer;
    font-family: inherit;
    transition: all 0.15s;
  }
  .pag-btn:hover:not(:disabled) { color: var(--text-primary); border-color: rgba(249,115,22,0.3); }
  .pag-btn:disabled { opacity: 0.4; cursor: not-allowed; }

  /* ── Empty / Loading ── */
  .empty-state {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 12px;
    color: var(--text-secondary);
    padding: 60px 24px;
  }
  .empty-icon {
    width: 72px;
    height: 72px;
    border-radius: 16px;
    background: var(--item-hover);
    border: 1px solid var(--border-color);
    display: flex;
    align-items: center;
    justify-content: center;
    opacity: 0.5;
  }
  .empty-state h3 { font-size: 16px; font-weight: 700; color: var(--text-primary); margin: 0; }
  .empty-state p  { font-size: 13px; color: var(--text-secondary); margin: 0; text-align: center; }

  .loading-center {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 12px;
    padding: 60px 24px;
    color: var(--text-secondary);
    font-size: 14px;
  }
  :global(.spin) { animation: spin 0.7s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }

  /* ── Runs ── */
  .output-cell { max-width: 220px; }
  .output-ok  { font-size: 12px; font-family: monospace; color: var(--text-secondary); display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .output-err { font-size: 12px; font-family: monospace; color: #ef4444; display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .output-none { color: var(--text-secondary); opacity: 0.4; }
  .mono { font-family: ui-monospace, SFMono-Regular, monospace; }
  .text-sm { font-size: 12px; }

  /* ── Detail Side Panel ── */
  .panel-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0,0,0,0.25);
    z-index: 200;
    animation: fade 0.2s ease;
  }
  .detail-panel {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    width: 420px;
    background: var(--card-bg);
    border-left: 1px solid var(--border-color);
    box-shadow: -4px 0 32px rgba(0,0,0,0.15);
    z-index: 300;
    display: flex;
    flex-direction: column;
    animation: slideLeft 0.25s cubic-bezier(0.4, 0, 0.2, 1);
    overflow: hidden;
  }
  @keyframes slideLeft {
    from { transform: translateX(60px); opacity: 0; }
    to   { transform: translateX(0);    opacity: 1; }
  }
  @keyframes fade { from { opacity: 0; } to { opacity: 1; } }

  .panel-header {
    padding: 20px 20px;
    border-bottom: 1px solid var(--border-color);
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    flex-shrink: 0;
  }
  .panel-title { font-size: 16px; font-weight: 700; }
  .panel-subtitle { font-size: 11px; color: var(--text-secondary); font-family: monospace; margin-top: 3px; }

  .panel-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 12px 20px;
    border-bottom: 1px solid var(--border-color);
    flex-shrink: 0;
    flex-wrap: wrap;
  }

  .panel-body {
    flex: 1;
    overflow-y: auto;
    padding: 20px;
  }

  .info-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 1px; background: var(--border-color); border-radius: 12px; overflow: hidden; margin-bottom: 16px; }
  .info-kv {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 10px 12px;
    background: var(--card-bg);
  }
  .info-key { font-size: 10px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.05em; color: var(--text-secondary); }
  .info-val { font-size: 12px; font-family: ui-monospace, SFMono-Regular, monospace; color: var(--text-primary); }

  .payload-block { margin-top: 14px; }
  .payload-pre {
    background: var(--sidebar-bg);
    border: 1px solid var(--border-color);
    border-radius: 10px;
    padding: 12px;
    font-size: 11px;
    font-family: ui-monospace, SFMono-Regular, monospace;
    color: var(--text-secondary);
    overflow-x: auto;
    margin-top: 6px;
    white-space: pre-wrap;
    word-break: break-all;
  }

  .section-label {
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-secondary);
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 10px;
  }
  .run-total-badge {
    font-size: 10px;
    padding: 1px 7px;
    border-radius: 999px;
    background: var(--item-hover);
    color: var(--text-secondary);
    border: 1px solid var(--border-color);
    font-weight: 500;
    text-transform: none;
    letter-spacing: normal;
  }

  .empty-runs { font-size: 13px; color: var(--text-secondary); text-align: center; padding: 20px; }

  .run-list { display: flex; flex-direction: column; gap: 6px; }
  .run-entry {
    padding: 10px 12px;
    border-radius: 10px;
    background: var(--sidebar-bg);
    border-left: 3px solid transparent;
    border: 1px solid var(--border-color);
    border-left-width: 3px;
  }
  .run-entry--success  { border-left-color: #10b981; }
  .run-entry--failed   { border-left-color: #ef4444; }
  .run-entry--running  { border-left-color: #8b5cf6; }
  .run-entry--pending  { border-left-color: #f59e0b; }
  .run-entry--timeout  { border-left-color: #ef4444; }
  .run-entry--cancelled{ border-left-color: #6b7280; }

  .run-entry-top { display: flex; align-items: center; justify-content: space-between; margin-bottom: 4px; }
  .run-dur { font-size: 11px; color: var(--text-secondary); font-family: monospace; }
  .run-meta { font-size: 11px; color: var(--text-secondary); }
  .run-out { font-size: 11px; color: var(--text-secondary); font-family: monospace; margin-top: 4px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .run-err { font-size: 11px; color: #ef4444; font-family: monospace; margin-top: 4px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  /* ── Settings ── */
  .settings-layout { flex: 1; overflow-y: auto; padding: 24px 28px; }
  .settings-cols { display: grid; grid-template-columns: 1fr 1fr; gap: 20px; }
  .settings-col { display: flex; flex-direction: column; gap: 20px; }

  .settings-card {
    background: var(--card-bg);
    border: 1px solid var(--border-color);
    border-radius: 16px;
    overflow: hidden;
    box-shadow: 0 2px 12px var(--shadow-color);
  }
  .settings-card-header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 14px 20px;
    border-bottom: 1px solid var(--border-color);
    font-size: 12px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-secondary);
    background: var(--sidebar-bg);
  }
  :global(.settings-card-icon) { color: #f97316; }
  .settings-card-body { padding: 20px; display: flex; flex-direction: column; gap: 14px; }

  .field-row { display: flex; flex-direction: column; gap: 5px; }
  .field-label { font-size: 12px; font-weight: 700; color: var(--text-secondary); text-transform: uppercase; letter-spacing: 0.04em; }
  .field-unit { font-size: 10px; font-weight: 500; color: var(--text-secondary); opacity: 0.6; text-transform: none; letter-spacing: normal; }
  .field-input {
    padding: 9px 12px;
    background: var(--frame-bg);
    border: 1px solid var(--border-color);
    border-radius: 10px;
    color: var(--text-primary);
    font-size: 13px;
    font-family: inherit;
    outline: none;
    transition: border-color 0.15s;
    width: 100%;
  }
  .field-input:focus { border-color: #f97316; box-shadow: 0 0 0 3px rgba(249,115,22,0.1); }
  .field-input.mono { font-family: ui-monospace, SFMono-Regular, monospace; font-size: 12px; }
  .field-select { cursor: pointer; }
  .field-hint { font-size: 11px; color: var(--text-secondary); opacity: 0.7; margin: 0; }

  .toggle-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 0;
    border-top: 1px solid var(--border-color);
    gap: 12px;
  }
  .toggle-row--danger { background: rgba(245,158,11,0.04); padding: 10px 12px; margin: 0 -8px; border-radius: 10px; border-top: 1px solid rgba(245,158,11,0.15); }
  .toggle-label { font-size: 13px; font-weight: 600; color: var(--text-primary); }
  .toggle-hint  { font-size: 11px; color: var(--text-secondary); margin-top: 2px; }

  /* Reuse app.css toggle-switch */
  .toggle-switch { position: relative; display: inline-block; width: 44px; height: 24px; flex-shrink: 0; }
  .toggle-switch input { opacity: 0; width: 0; height: 0; }
  .toggle-slider {
    position: absolute; cursor: pointer;
    top: 0; left: 0; right: 0; bottom: 0;
    background-color: rgba(107, 114, 128, 0.3);
    transition: 0.2s; border-radius: 24px;
  }
  .toggle-slider:before {
    position: absolute; content: '';
    height: 18px; width: 18px;
    left: 3px; bottom: 3px;
    background-color: white;
    transition: 0.2s; border-radius: 50%;
  }
  .toggle-switch input:checked + .toggle-slider { background-color: #10b981; }
  .toggle-switch input:checked + .toggle-slider--amber { background-color: #f59e0b !important; }
  .toggle-switch input:checked + .toggle-slider:before { transform: translateX(20px); }

  .settings-ops { padding-top: 10px; border-top: 1px solid var(--border-color); }
  .settings-actions { display: flex; justify-content: flex-end; gap: 10px; padding: 4px 0; }

  /* ── Modal form ── */
  .modal-form { display: flex; flex-direction: column; gap: 16px; }
  .form-cols-2 { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
  .form-cols-3 { display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 14px; }
  .form-group { display: flex; flex-direction: column; gap: 6px; }
  .form-group label { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.05em; color: var(--text-secondary); }

  .cron-preview { font-size: 12px; color: #10b981; font-family: monospace; }
  .cron-presets { display: flex; flex-wrap: wrap; gap: 5px; margin-top: 6px; }
  .preset-chip {
    font-size: 11px;
    padding: 4px 10px;
    border-radius: 7px;
    background: rgba(249,115,22,0.07);
    color: #f97316;
    border: 1px solid rgba(249,115,22,0.2);
    cursor: pointer;
    font-family: inherit;
    transition: background 0.12s;
  }
  .preset-chip:hover { background: rgba(249,115,22,0.15); }

  .form-toggles {
    background: var(--sidebar-bg);
    border: 1px solid var(--border-color);
    border-radius: 12px;
    padding: 4px 16px;
  }
  .form-toggles .toggle-row:first-child { border-top: none; }
</style>
