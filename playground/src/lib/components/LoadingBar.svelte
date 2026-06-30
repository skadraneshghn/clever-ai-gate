<script>
  import { navigating } from '$app/stores';
  import { appState } from '../state.svelte.js';

  let active = $derived(
    !!$navigating || 
    appState.isSending || 
    appState.isConnecting || 
    appState.isAdminConnecting
  );

  let width = $state(0);
  let status = $state('idle'); // 'idle' | 'loading' | 'success'

  $effect(() => {
    if (active) {
      status = 'loading';
      width = 0;
      const interval = setInterval(() => {
        if (width < 30) {
          width += 6;
        } else if (width < 70) {
          width += 3;
        } else if (width < 92) {
          width += 0.8;
        }
      }, 120);

      return () => {
        clearInterval(interval);
      };
    } else {
      if (status === 'loading') {
        status = 'success';
        width = 100;
        const timeout = setTimeout(() => {
          status = 'idle';
          width = 0;
        }, 500);
        return () => clearTimeout(timeout);
      }
    }
  });
</script>

{#if status !== 'idle'}
  <div 
    class="loading-bar {status}" 
    style="width: {width}%"
  ></div>
{/if}

<style>
  .loading-bar {
    position: fixed;
    top: 0;
    left: 0;
    height: 3px;
    background: linear-gradient(90deg, #f97316 0%, #ec4899 100%);
    z-index: 9999;
    transition: width 0.2s ease-out, opacity 0.3s ease-in-out;
    box-shadow: 0 0 8px rgba(249, 115, 22, 0.6);
  }

  .loading-bar.success {
    opacity: 0;
    transition: width 0.1s ease-out, opacity 0.4s ease-in-out 0.1s;
  }
</style>
