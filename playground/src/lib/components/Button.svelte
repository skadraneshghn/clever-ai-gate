<script>
  let {
    variant = 'primary', // 'primary' | 'secondary' | 'success' | 'danger' | 'outline' | 'ghost' | 'text'
    size = 'md',         // 'sm' | 'md' | 'lg'
    align = 'center',    // 'center' | 'left' | 'right' | 'between'
    disabled = false,
    type = 'button',
    class: className = '',
    onclick,
    href,
    title,
    children,
    ...rest
  } = $props();

  const alignMap = {
    left: 'flex-start',
    right: 'flex-end',
    between: 'space-between',
    center: 'center'
  };

  let alignVal = $derived(alignMap[align] || 'center');
</script>

{#if href}
  <a
    {href}
    class="btn btn-{variant} btn-{size} {className}"
    class:disabled
    {title}
    style="--btn-align: {alignVal};"
    onclick={(e) => { if (disabled) e.preventDefault(); else onclick?.(e); }}
    {...rest}
  >
    {@render children?.()}
  </a>
{:else}
  <button
    {type}
    class="btn btn-{variant} btn-{size} {className}"
    {disabled}
    {title}
    style="--btn-align: {alignVal};"
    onclick={onclick}
    {...rest}
  >
    {@render children?.()}
  </button>
{/if}

<style>
  .btn {
    display: inline-flex;
    align-items: center;
    justify-content: var(--btn-align, center);
    gap: 8px;
    font-family: inherit;
    font-weight: 600;
    border-radius: 12px;
    border: 1px solid transparent;
    cursor: pointer;
    transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1);
    user-select: none;
    text-decoration: none;
    white-space: nowrap;
  }

  /* Sizes */
  .btn-sm {
    padding: 6px 12px;
    font-size: 13px;
    border-radius: 10px;
    height: 34px;
  }

  .btn-md {
    padding: 10px 18px;
    font-size: 14px;
    border-radius: 12px;
    height: 42px;
  }

  .btn-lg {
    padding: 14px 24px;
    font-size: 16px;
    border-radius: 14px;
    height: 50px;
  }

  /* Variants */
  .btn-primary {
    background-color: #f97316;
    color: #ffffff;
    box-shadow: 0 4px 12px rgba(249, 115, 22, 0.2);
  }
  .btn-primary:hover:not(:disabled) {
    background-color: #ea580c;
    transform: translateY(-1px);
    box-shadow: 0 6px 16px rgba(249, 115, 22, 0.3);
  }
  .btn-primary:active:not(:disabled) {
    transform: translateY(0) scale(0.98);
  }

  .btn-secondary {
    background-color: var(--item-hover, rgba(0, 0, 0, 0.05));
    color: var(--text-primary);
    border-color: var(--border-color);
  }
  .btn-secondary:hover:not(:disabled) {
    background-color: var(--border-color);
    transform: translateY(-1px);
  }
  .btn-secondary:active:not(:disabled) {
    transform: translateY(0) scale(0.98);
  }

  .btn-success {
    background-color: #10b981;
    color: #ffffff;
    box-shadow: 0 4px 12px rgba(16, 185, 129, 0.15);
  }
  .btn-success:hover:not(:disabled) {
    background-color: #059669;
    transform: translateY(-1px);
    box-shadow: 0 6px 16px rgba(16, 185, 129, 0.25);
  }
  .btn-success:active:not(:disabled) {
    transform: translateY(0) scale(0.98);
  }

  .btn-danger {
    background-color: #ef4444;
    color: #ffffff;
    box-shadow: 0 4px 12px rgba(239, 68, 68, 0.15);
  }
  .btn-danger:hover:not(:disabled) {
    background-color: #dc2626;
    transform: translateY(-1px);
    box-shadow: 0 6px 16px rgba(239, 68, 68, 0.25);
  }
  .btn-danger:active:not(:disabled) {
    transform: translateY(0) scale(0.98);
  }

  .btn-outline {
    background: transparent;
    color: var(--text-primary);
    border: 1px solid var(--border-color);
  }
  .btn-outline:hover:not(:disabled) {
    background-color: var(--item-hover);
    transform: translateY(-1px);
  }
  .btn-outline:active:not(:disabled) {
    transform: translateY(0) scale(0.98);
  }

  .btn-ghost {
    background: transparent;
    color: var(--text-secondary);
  }
  .btn-ghost:hover:not(:disabled) {
    background-color: var(--item-hover);
    color: var(--text-primary);
  }
  .btn-ghost:active:not(:disabled) {
    transform: scale(0.97);
  }

  .btn-text {
    background: transparent;
    color: #f97316;
    padding: 4px 8px;
    height: auto;
    border-radius: 6px;
  }
  .btn-text:hover:not(:disabled) {
    text-decoration: underline;
    background-color: rgba(249, 115, 22, 0.05);
  }

  /* Disabled State */
  :disabled, .disabled {
    cursor: not-allowed;
    opacity: 0.5;
    transform: none !important;
    box-shadow: none !important;
  }
</style>
