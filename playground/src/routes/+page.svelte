<script>
  import { onMount } from 'svelte';
  import { 
    Cpu, RefreshCw, Users, KeyRound, Shield, AlertTriangle, Activity, 
    TrendingUp, ShieldCheck, Zap, Coins, Clock, Database, BarChart2
  } from '@lucide/svelte';
  import { appState } from '$lib/state.svelte.js';
  import Button from '$lib/components/Button.svelte';
  import Card from '$lib/components/Card.svelte';
  import Input from '$lib/components/Input.svelte';

  let stats = $state(null);
  let loading = $state(false);
  let error = $state('');

  function adminHeaders() {
    return {
      'Authorization': `Bearer ${appState.getAdminKey()}`,
      'Content-Type': 'application/json'
    };
  }

  async function loadDashboardStats() {
    loading = true;
    error = '';
    appState.apiLoading = true;
    try {
      const res = await fetch('/api/v1/admin/metrics', { headers: adminHeaders() });
      if (res.ok) {
        stats = await res.json();
      } else {
        const err = await res.json();
        error = err.error || `Error ${res.status}`;
      }
    } catch (e) {
      error = `Network error: ${e.message}`;
    } finally {
      loading = false;
      appState.apiLoading = false;
    }
  }

  function connectAdminKey() {
    const key = appState.adminKey.trim();
    if (!key) return;
    localStorage.setItem('cag_admin_key', key);
    loadDashboardStats();
  }

  function formatUptime(seconds) {
    if (!seconds) return '—';
    const d = Math.floor(seconds / (3600 * 24));
    const h = Math.floor((seconds % (3600 * 24)) / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    const s = seconds % 60;
    
    const parts = [];
    if (d > 0) parts.push(`${d}d`);
    if (h > 0) parts.push(`${h}h`);
    if (m > 0) parts.push(`${m}m`);
    parts.push(`${s}s`);
    return parts.join(' ');
  }

  function formatNumber(num) {
    if (num >= 1e9) return (num / 1e9).toFixed(1) + 'B';
    if (num >= 1e6) return (num / 1e6).toFixed(1) + 'M';
    if (num >= 1e3) return (num / 1e3).toFixed(1) + 'K';
    return num.toString();
  }

  // Reactive calculations for SVG timeseries chart
  let maxCount = $derived(
    stats && stats.daily_stats && stats.daily_stats.length > 0 
      ? Math.max(...stats.daily_stats.map(d => d.total), 10)
      : 10
  );

  let chartPoints = $derived(
    stats && stats.daily_stats && stats.daily_stats.length > 0
      ? stats.daily_stats.map((d, index) => {
          const count = stats.daily_stats.length;
          const step = count > 1 ? 460 / (count - 1) : 460;
          const x = 20 + index * step;
          const y = 125 - (d.total / maxCount) * 105;
          return { x: Number(x.toFixed(1)), y: Number(y.toFixed(1)), date: d.date, total: d.total };
        })
      : []
  );

  let successPoints = $derived(
    stats && stats.daily_stats && stats.daily_stats.length > 0
      ? stats.daily_stats.map((d, index) => {
          const count = stats.daily_stats.length;
          const step = count > 1 ? 460 / (count - 1) : 460;
          const x = 20 + index * step;
          const y = 125 - (d.successful / maxCount) * 105;
          return { x: Number(x.toFixed(1)), y: Number(y.toFixed(1)) };
        })
      : []
  );

  let svgLinePath = $derived(
    chartPoints.map((p, i) => `${i === 0 ? 'M' : 'L'} ${p.x} ${p.y}`).join(' ')
  );

  let svgAreaPath = $derived(
    chartPoints.length > 0
      ? `${svgLinePath} L ${chartPoints[chartPoints.length - 1].x} 135 L ${chartPoints[0].x} 135 Z`
      : ''
  );

  let svgSuccessLinePath = $derived(
    successPoints.map((p, i) => `${i === 0 ? 'M' : 'L'} ${p.x} ${p.y}`).join(' ')
  );

  $effect(() => {
    const key = appState.getAdminKey();
    if (key && !stats && !loading) {
      loadDashboardStats();
    }
  });

  onMount(() => {
    if (appState.getAdminKey()) {
      loadDashboardStats();
    }
  });
</script>

<header class="header flex items-center justify-between px-6 py-4 border-b shrink-0">
  <div class="flex items-center gap-3">
    <Activity size={20} class="text-[#f97316]" />
    <span class="font-bold text-base">Gateway Operations Dashboard</span>
  </div>
  
  {#if appState.adminKey.trim()}
    <div class="flex items-center gap-2 animate-fade-in">
      <Button variant="secondary" size="sm" onclick={() => { loadDashboardStats(); appState.addToast('info', 'Refreshing dashboard stats...'); }}>
        <RefreshCw size={14} />
        Refresh Data
      </Button>
    </div>
  {/if}
</header>

{#if !appState.adminKey.trim()}
  <!-- Admin key prompt -->
  <div class="logs-key-prompt flex flex-col justify-center items-center flex-grow p-6">
    <Card variant="filled" padding="lg" class="logs-key-card flex flex-col items-center text-center">
      <Shield size={40} class="text-[#f97316] mb-4 animate-pulse" />
      <h2 class="font-bold text-lg mb-2 text-primary">Admin Key Required</h2>
      <p class="text-sm mb-6 text-secondary max-w-sm">Enter your Admin API Key to access real-time gateway statistics, logs database analysis, and config health diagnostics.</p>
      
      <div class="flex flex-col gap-3 w-full max-w-sm">
        <Input
          type="password"
          placeholder="Enter Admin API Key..."
          bind:value={appState.adminKey}
          onkeydown={(e) => { if (e.key === 'Enter') connectAdminKey(); }}
        />
        <Button variant="primary" size="md" onclick={connectAdminKey}>
          Connect
        </Button>
      </div>
      
      {#if error}
        <p class="text-red-500 text-sm font-semibold mt-4">{error}</p>
      {/if}
    </Card>
  </div>
{:else}
  <!-- Dashboard Main scroll container -->
  <div class="dashboard-scroll flex-grow overflow-y-auto p-6 flex flex-col gap-6">
    
    {#if loading && !stats}
      <div class="flex flex-col items-center justify-center h-96">
        <div class="animate-spin text-[#f97316] text-xl">⟳</div>
        <p class="text-sm mt-2 text-secondary">Gathering core diagnostics statistics...</p>
      </div>
    {:else if error}
      <div class="flex flex-col items-center justify-center h-96 text-center">
        <AlertTriangle size={40} class="text-red-500 mb-2" />
        <p class="text-red-500 text-sm font-semibold">{error}</p>
        <Button variant="primary" class="mt-4" onclick={loadDashboardStats}>Retry</Button>
      </div>
    {:else if stats}
      
      <!-- KPIs Grid -->
      <div class="metrics-grid">
        
        <!-- KPI 1: Requests -->
        <Card variant="filled" padding="md" class="kpi-card relative overflow-hidden">
          <div class="kpi-icon-wrap bg-[#f97316]/10 text-[#f97316]">
            <TrendingUp size={20} />
          </div>
          <div>
            <div class="kpi-label text-secondary">Total Requests</div>
            <div class="kpi-value text-primary font-bold">{formatNumber(stats.total_requests)}</div>
          </div>
          <div class="kpi-desc text-xs text-secondary mt-1">Accumulated across all API keys</div>
        </Card>

        <!-- KPI 2: Success Rate -->
        <Card variant="filled" padding="md" class="kpi-card relative overflow-hidden">
          <div class="kpi-icon-wrap bg-green-500/10 text-green-500">
            <ShieldCheck size={20} />
          </div>
          <div>
            <div class="kpi-label text-secondary">Success Rate</div>
            <div class="kpi-value text-primary font-bold">{stats.success_rate.toFixed(2)}%</div>
          </div>
          <div class="kpi-desc text-xs text-secondary mt-1">{stats.successful_requests} successful calls</div>
        </Card>

        <!-- KPI 3: Avg Latency -->
        <Card variant="filled" padding="md" class="kpi-card relative overflow-hidden">
          <div class="kpi-icon-wrap bg-purple-500/10 text-purple-500">
            <Zap size={20} />
          </div>
          <div>
            <div class="kpi-label text-secondary">Average Latency</div>
            <div class="kpi-value text-primary font-bold">{stats.avg_latency_ms.toFixed(0)}ms</div>
          </div>
          <div class="kpi-desc text-xs text-secondary mt-1">End-to-end response time</div>
        </Card>

        <!-- KPI 4: Tokens Processed -->
        <Card variant="filled" padding="md" class="kpi-card relative overflow-hidden">
          <div class="kpi-icon-wrap bg-blue-500/10 text-blue-500">
            <Coins size={20} />
          </div>
          <div>
            <div class="kpi-label text-secondary">Tokens Volume</div>
            <div class="kpi-value text-primary font-bold">{formatNumber(stats.total_tokens)}</div>
          </div>
          <div class="kpi-desc text-xs text-secondary mt-1">{formatNumber(stats.prompt_tokens)} prompt / {formatNumber(stats.completion_tokens)} completion</div>
        </Card>

      </div>

      <!-- Overview Cards Grid -->
      <div class="overview-grid">
        
        <!-- System Uptime -->
        <Card variant="filled" padding="sm" class="overview-item">
          <Clock size={16} class="text-[#f97316] shrink-0" />
          <div class="flex flex-col text-left">
            <span class="text-xs text-secondary font-medium">Core Engine Uptime</span>
            <span class="text-sm font-bold text-primary truncate mt-0.5" title={formatUptime(stats.uptime_seconds)}>{formatUptime(stats.uptime_seconds)}</span>
          </div>
        </Card>

        <!-- Active Tenants -->
        <Card variant="filled" padding="sm" class="overview-item">
          <Users size={16} class="text-[#f97316] shrink-0" />
          <div class="flex flex-col text-left">
            <span class="text-xs text-secondary font-medium">Active Tenants</span>
            <span class="text-sm font-bold text-primary mt-0.5">{stats.active_tenants} Accounts</span>
          </div>
        </Card>

        <!-- Model Pools -->
        <Card variant="filled" padding="sm" class="overview-item">
          <Database size={16} class="text-[#f97316] shrink-0" />
          <div class="flex flex-col text-left">
            <span class="text-xs text-secondary font-medium">Registered Pools</span>
            <span class="text-sm font-bold text-primary mt-0.5">{stats.total_pools} Pools</span>
          </div>
        </Card>

        <!-- Credentials Health -->
        <Card variant="filled" padding="sm" class="overview-item">
          <KeyRound size={16} class="text-[#f97316] shrink-0" />
          <div class="flex flex-col text-left">
            <span class="text-xs text-secondary font-medium">Provider API Keys Health</span>
            <span class="text-sm font-bold text-primary mt-0.5">{stats.healthy_credentials} / {stats.total_credentials} Healthy</span>
          </div>
        </Card>

      </div>

      <!-- Timeseries Chart and Core Lists Container -->
      <div class="dashboard-row-split">
        
        <!-- SVG Chart Column -->
        <Card variant="filled" padding="lg" class="chart-card flex flex-col justify-between">
          <div class="flex items-center justify-between mb-4">
            <div class="flex items-center gap-2">
              <BarChart2 size={18} class="text-[#f97316]" />
              <h3 class="font-bold text-base text-primary">Gateway Traffic Overview</h3>
            </div>
            <span class="text-xs text-secondary font-medium uppercase tracking-wider">Past 7 Days Time-Series</span>
          </div>

          <!-- SVG Vector Chart -->
          <div class="chart-wrapper relative border rounded-xl overflow-hidden p-3.5 bg-gray-light/30">
            {#if stats.daily_stats.length === 0}
              <div class="flex items-center justify-center h-36 text-sm text-secondary opacity-60">No recent traffic records found</div>
            {:else}
              <svg viewBox="0 0 500 140" class="w-full h-36 overflow-visible">
                <defs>
                  <linearGradient id="chartGradient" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stop-color="#f97316" stop-opacity="0.22" />
                    <stop offset="100%" stop-color="#f97316" stop-opacity="0.0" />
                  </linearGradient>
                </defs>
                
                <!-- Horizontal Gridlines -->
                <line x1="15" y1="20" x2="485" y2="20" stroke="var(--border-color)" stroke-width="1" stroke-dasharray="3 3" opacity="0.5" />
                <line x1="15" y1="60" x2="485" y2="60" stroke="var(--border-color)" stroke-width="1" stroke-dasharray="3 3" opacity="0.5" />
                <line x1="15" y1="100" x2="485" y2="100" stroke="var(--border-color)" stroke-width="1" stroke-dasharray="3 3" opacity="0.5" />
                <line x1="15" y1="135" x2="485" y2="135" stroke="var(--border-color)" stroke-width="1" opacity="0.7" />

                <!-- Gradient Filled Area -->
                <path d={svgAreaPath} fill="url(#chartGradient)" />

                <!-- Request total line -->
                <path d={svgLinePath} fill="none" stroke="#f97316" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" />

                <!-- Request success line -->
                <path d={svgSuccessLinePath} fill="none" stroke="#10b981" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" stroke-dasharray="4 3" />

                <!-- Data Points Glow Circles -->
                {#each chartPoints as pt}
                  <circle cx={pt.x} cy={pt.y} r="3.5" fill="#f97316" stroke="var(--card-bg)" stroke-width="2" />
                {/each}
              </svg>

              <!-- Chart X Axis Labels -->
              <div class="flex justify-between mt-3 px-3 font-mono text-[11px] font-medium text-secondary opacity-70">
                {#each stats.daily_stats as day}
                  <span>{day.date.slice(5)}</span>
                {/each}
              </div>
            {/if}
          </div>
          
          <div class="flex items-center gap-4 mt-3 pt-3 border-t border-[var(--border-color)] text-xs text-secondary justify-end">
            <span class="flex items-center gap-1.5"><span class="w-2.5 h-2.5 bg-[#f97316] rounded-full"></span> Total Requests</span>
            <span class="flex items-center gap-1.5"><span class="w-2.5 h-2.5 bg-[#10b981] rounded-full"></span> Success Requests</span>
          </div>
        </Card>

        <!-- Top Models Breakdown Card -->
        <Card variant="filled" padding="lg" class="breakdown-card">
          <h3 class="font-bold text-base text-primary mb-4">Top Models (Traffic)</h3>
          {#if stats.top_models.length === 0}
            <p class="text-xs text-secondary opacity-60">No routed requests telemetry</p>
          {:else}
            <div class="flex flex-col gap-3.5">
              {#each stats.top_models as model}
                <div class="flex flex-col gap-1 select-text">
                  <div class="flex items-center justify-between text-xs font-mono">
                    <span class="font-bold text-primary truncate max-w-[170px]" title={model.model}>{model.model}</span>
                    <span class="text-secondary">{model.requests} requests</span>
                  </div>
                  
                  <!-- Progress usage bar -->
                  <div class="w-full bg-gray-light border h-1.5 rounded-full overflow-hidden">
                    <div class="bg-[#f97316] h-full rounded-full" style="width: {(model.requests / stats.total_requests * 100).toFixed(0)}%"></div>
                  </div>
                  <span class="text-[10px] text-secondary mt-0.5 opacity-75">Avg Latency: {model.avg_latency_ms.toFixed(0)}ms</span>
                </div>
              {/each}
            </div>
          {/if}
        </Card>

      </div>

      <!-- Row 4: Top Tenants Accounts -->
      <div class="grid grid-cols-1">
        <Card variant="filled" padding="lg" class="breakdown-card">
          <h3 class="font-bold text-base text-primary mb-4">Top Active Tenants (Tokens Volume)</h3>
          {#if stats.top_tenants.length === 0}
            <p class="text-xs text-secondary opacity-60">No tenant request activity logs</p>
          {:else}
            <div class="providers-table-container">
              <table class="providers-table">
                <thead>
                  <tr>
                    <th style="font-size: 11px;">Tenant Account</th>
                    <th style="font-size: 11px;">Requests Route Total</th>
                    <th style="font-size: 11px;">Consumed Token Volume</th>
                  </tr>
                </thead>
                <tbody>
                  {#each stats.top_tenants as tenant}
                    <tr class="provider-row select-text">
                      <td class="font-bold text-sm text-[#f97316]">{tenant.name}</td>
                      <td class="font-mono text-sm">{tenant.requests}</td>
                      <td class="font-mono text-sm font-semibold">{formatNumber(tenant.total_tokens)} tokens</td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          {/if}
        </Card>
      </div>

    {/if}

  </div>
{/if}

<style>
  /* KPI and Dashboard Custom Styles */
  .metrics-grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 20px;
    width: 100%;
    box-sizing: border-box;
  }

  .overview-grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 16px;
    width: 100%;
    box-sizing: border-box;
  }

  .dashboard-row-split {
    display: grid;
    grid-template-columns: 2fr 1fr;
    gap: 24px;
    width: 100%;
    box-sizing: border-box;
  }

  @media (max-width: 1024px) {
    .metrics-grid {
      grid-template-columns: repeat(2, 1fr);
    }
    .overview-grid {
      grid-template-columns: repeat(2, 1fr);
    }
    .dashboard-row-split {
      grid-template-columns: 1fr;
    }
  }

  @media (max-width: 640px) {
    .metrics-grid {
      grid-template-columns: 1fr;
    }
    .overview-grid {
      grid-template-columns: 1fr;
    }
  }

  :global(.kpi-card) {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    border: 1px solid var(--border-color) !important;
    background-color: var(--card-bg) !important;
    box-shadow: 0 4px 18px var(--shadow-color) !important;
    border-radius: 16px !important;
    box-sizing: border-box;
    min-height: 150px;
    justify-content: space-between;
    padding: 20px !important;
    transition: transform 0.2s ease, box-shadow 0.2s ease;
  }
  
  :global(.kpi-card:hover) {
    transform: translateY(-2px);
    box-shadow: 0 10px 25px var(--shadow-color) !important;
  }

  .kpi-icon-wrap {
    width: 40px;
    height: 40px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 12px;
    margin-bottom: 8px;
  }

  .kpi-label {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-secondary);
  }

  .kpi-value {
    font-size: 26px;
    letter-spacing: -0.02em;
    line-height: 1.1;
    margin-top: 2px;
  }

  .kpi-desc {
    opacity: 0.6;
    font-size: 11px;
  }

  :global(.overview-item) {
    display: flex;
    align-items: center;
    gap: 12px;
    border: 1px solid var(--border-color) !important;
    background-color: var(--card-bg) !important;
    border-radius: 12px !important;
    box-shadow: 0 2px 10px var(--shadow-color) !important;
    box-sizing: border-box;
    height: 64px;
    padding: 12px 16px !important;
  }

  :global(.chart-card), :global(.breakdown-card) {
    border: 1px solid var(--border-color) !important;
    background-color: var(--card-bg) !important;
    border-radius: 16px !important;
    box-shadow: 0 4px 18px var(--shadow-color) !important;
    box-sizing: border-box;
    padding: 24px !important;
  }

  .chart-wrapper {
    margin-top: 16px;
    padding: 16px;
    border-radius: 12px;
    border: 1px solid var(--border-color);
  }

  /* Core metrics scroll bar style */
  .dashboard-scroll {
    box-sizing: border-box;
    width: 100%;
  }

  svg {
    stroke-linecap: round;
  }
</style>
