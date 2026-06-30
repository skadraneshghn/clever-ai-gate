<script>
  let {
    type = 'text', // 'text' | 'password' | 'number' | 'textarea' | 'select'
    value = $bindable(''),
    label = '',
    placeholder = '',
    error = '',
    disabled = false,
    class: className = '',
    rows = 3,
    options = [], // [{value, label}]
    onkeydown,
    onchange,
    id,
    children,
    ...rest
  } = $props();

  let inputId = $derived(id || `input-${Math.random().toString(36).substr(2, 9)}`);
</script>

<div class="input-wrapper {className}">
  {#if label}
    <label for={inputId} class="input-label">{label}</label>
  {/if}

  <div class="field-container">
    {#if type === 'textarea'}
      <textarea
        id={inputId}
        class="input-field textarea-field"
        class:has-error={error}
        {placeholder}
        {disabled}
        {rows}
        bind:value={value}
        onkeydown={onkeydown}
        onchange={onchange}
        {...rest}
      ></textarea>
    {:else if type === 'select'}
      <select
        id={inputId}
        class="input-field select-field"
        class:has-error={error}
        {disabled}
        bind:value={value}
        onchange={onchange}
        {...rest}
      >
        {#if placeholder}
          <option value="" disabled selected={value === ''}>{placeholder}</option>
        {/if}
        {#each options as opt}
          <option value={opt.value}>{opt.label || opt.value}</option>
        {/each}
        {@render children?.()}
      </select>
    {:else}
      <input
        {type}
        id={inputId}
        class="input-field"
        class:has-error={error}
        {placeholder}
        {disabled}
        bind:value={value}
        onkeydown={onkeydown}
        onchange={onchange}
        {...rest}
      />
    {/if}
  </div>

  {#if error}
    <span class="error-message">{error}</span>
  {/if}
</div>

<style>
  .input-wrapper {
    display: flex;
    flex-direction: column;
    gap: 6px;
    width: 100%;
    text-align: left;
  }

  .input-label {
    font-size: 12px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-secondary);
  }

  .field-container {
    position: relative;
    width: 100%;
  }

  .input-field {
    width: 100%;
    padding: 12px 16px;
    font-size: 14px;
    line-height: 1.5;
    font-family: inherit;
    color: var(--text-primary);
    background-color: var(--frame-bg);
    border: 1px solid var(--border-color);
    border-radius: 12px;
    transition: all 0.2s ease;
    box-sizing: border-box;
    outline: none;
  }

  .input-field::placeholder {
    color: var(--text-secondary);
    opacity: 0.5;
  }

  .input-field:focus {
    border-color: #f97316;
    box-shadow: 0 0 0 3px rgba(249, 115, 22, 0.15);
    background-color: var(--card-bg);
  }

  .input-field:disabled {
    cursor: not-allowed;
    opacity: 0.6;
    background-color: var(--item-hover);
  }

  .textarea-field {
    resize: vertical;
    min-height: 80px;
  }

  .select-field {
    appearance: none;
    background-image: url("data:image/svg+xml,%3csvg xmlns='http://www.w3.org/2000/svg' fill='none' viewBox='0 0 20 20'%3e%3cpath stroke='%236b7280' stroke-linecap='round' stroke-linejoin='round' stroke-width='1.5' d='M6 8l4 4 4-4'/%3e%3c/svg%3e");
    background-position: right 14px center;
    background-repeat: no-repeat;
    background-size: 18px 18px;
    padding-right: 38px;
    cursor: pointer;
  }

  .has-error {
    border-color: #ef4444 !important;
  }
  .has-error:focus {
    box-shadow: 0 0 0 3px rgba(239, 68, 68, 0.15) !important;
  }

  .error-message {
    font-size: 12px;
    font-weight: 500;
    color: #ef4444;
    margin-top: 2px;
  }
</style>
