<script>
  let {
    variant = 'filled', // 'filled' | 'outlined' | 'glass'
    padding = 'md',      // 'none' | 'sm' | 'md' | 'lg'
    interactive = false,
    class: className = '',
    onclick,
    title,
    children,
    ...rest
  } = $props();
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<!-- svelte-ignore a11y_click_events_have_key_events -->
<div
  class="card card-{variant} pad-{padding} {className}"
  class:interactive
  {title}
  onclick={onclick}
  {...rest}
>
  {@render children?.()}
</div>

<style>
  .card {
    border-radius: 16px;
    transition: all 0.25s cubic-bezier(0.4, 0, 0.2, 1);
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    position: relative;
    overflow: hidden;
    flex-shrink: 0;
  }

  /* Padding */
  .pad-none { padding: 0; }
  .pad-sm { padding: 12px; }
  .pad-md { padding: 20px; }
  .pad-lg { padding: 28px; }

  /* Variants */
  .card-filled {
    background-color: var(--card-bg);
    border: 1px solid var(--border-color);
    box-shadow: 0 4px 20px var(--shadow-color);
  }

  .card-outlined {
    background-color: transparent;
    border: 1.5px solid var(--border-color);
  }

  .card-glass {
    background: rgba(255, 255, 255, 0.03);
    backdrop-filter: blur(16px);
    -webkit-backdrop-filter: blur(16px);
    border: 1px solid rgba(255, 255, 255, 0.08);
    box-shadow: 0 8px 32px var(--shadow-color);
  }
  :global(.dark) .card-glass {
    background: rgba(19, 19, 26, 0.4);
    border: 1px solid rgba(255, 255, 255, 0.04);
  }

  /* Interactive behavior */
  .interactive {
    cursor: pointer;
  }
  .interactive:hover {
    transform: translateY(-2px);
    box-shadow: 0 8px 30px var(--shadow-color);
    border-color: rgba(249, 115, 22, 0.3);
  }
  .interactive:active {
    transform: translateY(0) scale(0.99);
  }
</style>
