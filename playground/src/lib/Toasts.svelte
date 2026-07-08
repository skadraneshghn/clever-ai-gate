<script>
  import { X, Check, AlertTriangle, Activity } from '@lucide/svelte';

  let { toasts = [], removeToast } = $props();
</script>

{#if toasts.length > 0}
  <div class="toast-container">
    {#each toasts as toast (toast.id)}
      <div class="toast toast-{toast.type}">
        <div class="flex items-center gap-2">
          {#if toast.type === 'success'}
            <Check size={14} />
          {:else if toast.type === 'error'}
            <AlertTriangle size={14} />
          {:else}
            <Activity size={14} />
          {/if}
          <span class="text-xs font-medium">{toast.message}</span>
        </div>
        <button class="toast-close" onclick={() => removeToast(toast.id)}>
          <X size={12} />
        </button>
      </div>
    {/each}
  </div>
{/if}
