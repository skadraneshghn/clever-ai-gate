<script>
  import { X } from '@lucide/svelte';

  let {
    show = $bindable(false),
    title = '',
    maxWidth = 'md', // 'sm' | 'md' | 'lg' | 'xl'
    class: className = '',
    header,
    footer,
    children
  } = $props();

  function close() {
    show = false;
  }

  function handleBackdropClick(e) {
    if (e.target === e.currentTarget) {
      close();
    }
  }

  // Handle escape key to close modal
  function handleKeydown(e) {
    if (e.key === 'Escape' && show) {
      close();
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

{#if show}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <div
    class="modal-backdrop"
    onclick={handleBackdropClick}
  >
    <div
      class="modal-container modal-{maxWidth} {className}"
      role="dialog"
      aria-modal="true"
    >
      <!-- Header -->
      {#if header}
        <div class="modal-header">
          {@render header()}
          <button class="close-btn" onclick={close} aria-label="Close modal">
            <X size={18} />
          </button>
        </div>
      {:else if title}
        <div class="modal-header">
          <h3 class="modal-title">{title}</h3>
          <button class="close-btn" onclick={close} aria-label="Close modal">
            <X size={18} />
          </button>
        </div>
      {/if}

      <!-- Body Content -->
      <div class="modal-body">
        {@render children?.()}
      </div>

      <!-- Footer -->
      {#if footer}
        <div class="modal-footer">
          {@render footer()}
        </div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .modal-backdrop {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background-color: rgba(0, 0, 0, 0.45);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px;
    z-index: 1000;
    animation: fade-in 0.25s cubic-bezier(0.16, 1, 0.3, 1);
  }

  .modal-container {
    background-color: var(--card-bg);
    border: 1px solid var(--border-color);
    border-radius: 20px;
    box-shadow: 0 20px 50px rgba(0, 0, 0, 0.3);
    display: flex;
    flex-direction: column;
    max-height: 90vh;
    width: 100%;
    overflow: hidden;
    position: relative;
    animation: slide-up 0.35s cubic-bezier(0.16, 1, 0.3, 1);
  }
  :global(.dark) .modal-container {
    box-shadow: 0 24px 64px rgba(0, 0, 0, 0.6);
  }

  /* Max Widths */
  .modal-sm { max-width: 380px; }
  .modal-md { max-width: 520px; }
  .modal-lg { max-width: 800px; }
  .modal-xl { max-width: 1140px; }

  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 20px 24px;
    border-bottom: 1px solid var(--border-color);
  }

  .modal-title {
    font-size: 18px;
    font-weight: 700;
    color: var(--text-primary);
    margin: 0;
  }

  .close-btn {
    color: var(--text-secondary);
    padding: 8px;
    border-radius: 50%;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition: all 0.2s ease;
    cursor: pointer;
    background: transparent;
    border: none;
  }

  .close-btn:hover {
    color: var(--text-primary);
    background-color: var(--item-hover);
  }

  .modal-body {
    padding: 24px;
    overflow-y: auto;
    flex-grow: 1;
    color: var(--text-primary);
    font-size: 14px;
    line-height: 1.6;
  }

  .modal-footer {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 12px;
    padding: 16px 24px;
    border-top: 1px solid var(--border-color);
    background-color: var(--sidebar-bg);
  }

  @keyframes fade-in {
    from { opacity: 0; }
    to { opacity: 1; }
  }

  @keyframes slide-up {
    from {
      opacity: 0;
      transform: translateY(20px) scale(0.96);
    }
    to {
      opacity: 1;
      transform: translateY(0) scale(1);
    }
  }
</style>
