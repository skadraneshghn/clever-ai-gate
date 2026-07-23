import { browser } from '$app/environment';
import { goto } from '$app/navigation';

class AppState {
  theme = $state('light');
  apiKey = $state('');
  models = $state([]);
  selectedModel = $state('');
  chats = $state([]);
  currentChatId = $state(null);
  messages = $state([]);
  
  tenantName = $state('');
  tenantBalance = $state(0);
  tenantRateLimit = $state(0);
  
  statusHUD = $state('Enter API Key');
  providerHUD = $state('—');
  modelHUD = $state('—');
  ttftHUD = $state('—');
  latencyHUD = $state('—');
  speedHUD = $state('—');
  
  inputText = $state('');
  isDeeperResearch = $state(false);
  isSending = $state(false);
  showSettingsModal = $state(false);
  showModelDropdown = $state(false);
  visibleApiKey = $state(false);
  connectError = $state('');
  isConnecting = $state(false);
  isInitializing = $state(true);
  apiLoading = $state(false);
  
  activeSettingsTab = $state('tenant');
  adminApiKey = $state('');
  nvidiaApiKey = $state('');
  nvidiaBaseUrl = $state('https://integrate.api.nvidia.com/v1');
  isAdminConnecting = $state(false);
  adminConnectSuccess = $state('');
  adminConnectError = $state('');
  
  showCodePanel = $state(false);
  activeCodeTab = $state('curl');
  
  adminKey = $state('');
  
  logLines = $state([]);
  logsStreaming = $state(false);
  logsAutoScroll = $state(true);
  logsError = $state('');
  
  toasts = $state([]);
  toastCounter = 0;

  logsAbortController = null;

  tenantInitials = $derived.by(() => {
    if (!this.tenantName) return 'T';
    const cleanName = this.tenantName.replace(/[^a-zA-Z0-9\s-_]/g, '');
    const parts = cleanName.split(/[\s\-_]+/);
    const initials = parts.map(p => p[0]).filter(Boolean).join('');
    return initials ? initials.substring(0, 2).toUpperCase() : 'T';
  });

  sidebarChats = $derived.by(() => {
    const today = [];
    const yesterday = [];
    const older = [];
    
    this.chats.forEach(c => {
      const date = new Date(c.updated_at || c.created_at);
      const diffTime = Math.abs(new Date() - date);
      const diffDays = Math.ceil(diffTime / (1000 * 60 * 60 * 24));
      
      if (diffDays <= 1) {
        today.push(c);
      } else if (diffDays <= 2) {
        yesterday.push(c);
      } else {
        older.push(c);
      }
    });

    return { today, yesterday, older };
  });

  constructor() {
    if (browser) {
      const savedAdminKey = localStorage.getItem('cag_admin_key');
      if (savedAdminKey) {
        this.adminKey = savedAdminKey;
        this.adminApiKey = savedAdminKey;
      }
      const savedTenantKey = localStorage.getItem('cag_playground_api_key');
      if (savedTenantKey) {
        this.apiKey = savedTenantKey;
      }
      const savedTheme = localStorage.getItem('cag_playground_theme');
      if (savedTheme) {
        this.theme = savedTheme;
        this.applyTheme(savedTheme);
      }
      this.init();
    }
  }

  getAdminKey() {
    if (this.adminKey && this.adminKey.trim()) {
      return this.adminKey.trim();
    }
    if (browser) {
      const saved = localStorage.getItem('cag_admin_key') || '';
      if (saved) {
        this.adminKey = saved;
        this.adminApiKey = saved;
      }
      return saved;
    }
    return '';
  }

  async init() {
    try {
      const res = await fetch('/api/v1/playground/config');
      if (res.ok) {
        const data = await res.json();
        if (data.tenant_key) {
          localStorage.setItem('cag_playground_api_key', data.tenant_key);
          if (!this.apiKey) this.apiKey = data.tenant_key;
        }
        if (data.admin_key) {
          localStorage.setItem('cag_admin_key', data.admin_key);
          if (!this.adminKey) {
            this.adminKey = data.admin_key;
            this.adminApiKey = data.admin_key;
          }
        }
      }
    } catch (e) {
      console.warn('Failed to load default config from backend:', e);
    }

    const savedKey = this.apiKey || (browser ? localStorage.getItem('cag_playground_api_key') : '');
    if (savedKey) {
      this.statusHUD = 'Verifying...';
      const isValid = await this.loadTenantInfo(savedKey);
      if (isValid) {
        this.apiKey = savedKey;
        this.statusHUD = 'Ready';
        this.loadModels();
        this.loadChats();
      } else {
        localStorage.removeItem('cag_playground_api_key');
        this.apiKey = '';
        this.statusHUD = 'Enter API Key';
        this.showSettingsModal = true;
      }
    } else {
      this.showSettingsModal = true;
    }

    this.isInitializing = false;
  }

  addToast(type, message, timeout = 4000) {
    const id = ++this.toastCounter;
    this.toasts = [...this.toasts, { id, type, message }];
    setTimeout(() => {
      this.toasts = this.toasts.filter(t => t.id !== id);
    }, timeout);
  }

  removeToast(id) {
    this.toasts = this.toasts.filter(t => t.id !== id);
  }

  applyTheme(newTheme) {
    this.theme = newTheme;
    if (browser) {
      localStorage.setItem('cag_playground_theme', newTheme);
      if (newTheme === 'dark') {
        document.documentElement.classList.add('dark');
      } else {
        document.documentElement.classList.remove('dark');
      }
    }
  }

  async handleSaveKey() {
    const keyToSave = this.apiKey.trim();
    if (!keyToSave) {
      this.connectError = 'API key cannot be empty.';
      return;
    }

    this.connectError = '';
    this.isConnecting = true;
    this.statusHUD = 'Connecting...';

    const isValid = await this.loadTenantInfo(keyToSave);
    if (isValid) {
      this.apiKey = keyToSave;
      if (browser) {
        localStorage.setItem('cag_playground_api_key', keyToSave);
      }
      this.showSettingsModal = false;
      this.statusHUD = 'Ready';
      this.loadModels();
      this.loadChats();
    } else {
      this.connectError = 'Invalid API key. Please check your credentials and try again.';
      this.statusHUD = 'Error';
    }
    this.isConnecting = false;
  }

  async handleRegisterNvidia() {
    const adminKeyToUse = this.adminApiKey.trim();
    const nvKeyToUse = this.nvidiaApiKey.trim();
    const baseUrlToUse = this.nvidiaBaseUrl.trim();

    if (!adminKeyToUse || !nvKeyToUse || !baseUrlToUse) {
      this.adminConnectError = 'All fields are required.';
      return;
    }

    this.adminConnectError = '';
    this.adminConnectSuccess = '';
    this.isAdminConnecting = true;

    try {
      const res = await fetch('/api/v1/admin/providers/nvidia', {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${adminKeyToUse}`,
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({
          provider: 'nvidia',
          api_key: nvKeyToUse,
          base_url: baseUrlToUse,
          weight: 1
        })
      });

      if (res.status === 200) {
        const data = await res.json();
        this.adminConnectSuccess = `Successfully registered ${data.models_count || 0} models!`;
        this.adminKey = adminKeyToUse;
        if (browser) {
          localStorage.setItem('cag_admin_key', adminKeyToUse);
        }
        if (this.apiKey) {
          this.loadModels();
        }
      } else {
        const errData = await res.json();
        this.adminConnectError = errData.details || errData.error || 'Failed to register provider.';
      }
    } catch (e) {
      this.adminConnectError = `Network error: ${e.message}`;
    } finally {
      this.isAdminConnecting = false;
    }
  }

  handleModelPickerClick() {
    if (this.models.length === 0) {
      this.activeSettingsTab = 'admin';
      this.showSettingsModal = true;
    } else {
      this.showModelDropdown = !this.showModelDropdown;
    }
  }

  async loadTenantInfo(keyToUse = this.apiKey) {
    if (!keyToUse.trim()) return false;
    this.apiLoading = true;
    try {
      const res = await fetch('/api/v1/playground/tenant', {
        headers: {
          'Authorization': `Bearer ${keyToUse}`
        }
      });
      if (res.status === 200) {
        const data = await res.json();
        this.tenantName = data.name || 'Tenant';
        this.tenantBalance = data.token_balance || 0;
        this.tenantRateLimit = data.rate_limit_rpm || 0;
        return true;
      }
      return false;
    } catch (e) {
      console.error('Failed to load tenant info', e);
      return false;
    } finally {
      this.apiLoading = false;
    }
  }

  formatBalance(num) {
    if (num >= 1e9) return (num / 1e9).toFixed(1) + 'B';
    if (num >= 1e6) return (num / 1e6).toFixed(1) + 'M';
    if (num >= 1e3) return (num / 1e3).toFixed(1) + 'K';
    return num.toString();
  }

  async loadModels() {
    this.apiLoading = true;
    try {
      const res = await fetch('/v1/models', {
        headers: {
          'Authorization': `Bearer ${this.apiKey}`
        }
      });
      if (res.status === 200) {
        const data = await res.json();
        this.models = data.data || [];
        if (this.models.length > 0 && !this.selectedModel) {
          this.selectedModel = this.models[0].id;
        }
        this.statusHUD = 'Ready';
      } else {
        this.statusHUD = `Error: ${res.statusText}`;
      }
    } catch (e) {
      this.statusHUD = 'Failed to fetch models';
    } finally {
      this.apiLoading = false;
    }
  }

  async loadChats() {
    this.apiLoading = true;
    try {
      const res = await fetch('/api/v1/playground/chats', {
        headers: {
          'Authorization': `Bearer ${this.apiKey}`
        }
      });
      if (res.status === 200) {
        this.chats = await res.json();
      }
    } catch (e) {
      console.error('Failed to load chat sessions', e);
    } finally {
      this.apiLoading = false;
    }
  }

  async selectChat(id) {
    this.currentChatId = id;
    this.apiLoading = true;
    if (browser) {
      goto('/playground/chat');
    }
    try {
      const res = await fetch(`/api/v1/playground/chats/${id}`, {
        headers: {
          'Authorization': `Bearer ${this.apiKey}`
        }
      });
      if (res.status === 200) {
        const data = await res.json();
        this.messages = data.messages || [];
      }
    } catch (e) {
      console.error('Failed to fetch conversation details', e);
    } finally {
      this.apiLoading = false;
    }
  }

  async startNewChat() {
    this.currentChatId = null;
    this.messages = [];
    this.inputText = '';
    if (browser) {
      goto('/playground/chat');
    }
  }

  async deleteChat(id, e) {
    if (e) e.stopPropagation();
    this.apiLoading = true;
    try {
      const res = await fetch(`/api/v1/playground/chats/${id}`, {
        method: 'DELETE',
        headers: {
          'Authorization': `Bearer ${this.apiKey}`
        }
      });
      if (res.status === 200) {
        this.loadChats();
        if (this.currentChatId === id) {
          this.startNewChat();
        }
      }
    } catch (err) {
      console.error('Failed to delete chat', err);
    } finally {
      this.apiLoading = false;
    }
  }

  applyPreset(text) {
    this.inputText = text;
  }

  async submitPrompt() {
    if (!this.inputText.trim() || this.isSending) return;
    
    const userMsg = { role: 'user', content: this.inputText };
    this.messages = [...this.messages, userMsg];
    const originalInput = this.inputText;
    this.inputText = '';
    this.isSending = true;

    this.statusHUD = 'Streaming...';
    this.providerHUD = '—';
    this.modelHUD = this.selectedModel;
    this.ttftHUD = 'Calculating...';
    this.latencyHUD = 'Calculating...';
    this.speedHUD = 'Calculating...';

    const startTime = performance.now();
    let ttftTime = 0;
    let firstTokenReceived = false;
    let tokenCount = 0;
    
    const assistantPlaceholder = { role: 'assistant', content: '', reasoning_content: '' };
    this.messages = [...this.messages, assistantPlaceholder];
    const assistantIndex = this.messages.length - 1;

    try {
      const response = await fetch('/v1/chat/completions', {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${this.apiKey}`,
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({
          model: this.selectedModel,
          messages: this.messages.slice(0, -1).map(m => ({ role: m.role, content: m.content })),
          stream: true,
          temperature: 0.7
        })
      });

      if (response.status !== 200) {
        const errorText = await response.text();
        this.messages[assistantIndex].content = `Error: ${errorText}`;
        this.statusHUD = `Error [${response.status}]`;
        this.isSending = false;
        return;
      }

      const gwProvider = response.headers.get('X-Gateway-Provider');
      const gwModel = response.headers.get('X-Gateway-Model-Pattern');
      if (gwProvider) this.providerHUD = gwProvider.toUpperCase();
      if (gwModel) this.modelHUD = gwModel;

      const reader = response.body.getReader();
      const decoder = new TextDecoder('utf-8');
      let buffer = '';

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        if (!firstTokenReceived) {
          firstTokenReceived = true;
          ttftTime = performance.now() - startTime;
          this.ttftHUD = `${Math.round(ttftTime)}ms`;
        }

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop();

        for (const line of lines) {
          const cleanedLine = line.trim();
          if (!cleanedLine) continue;

          if (cleanedLine.startsWith('data: ')) {
            const dataStr = cleanedLine.slice(6);
            if (dataStr === '[DONE]') continue;

            try {
              const parsed = JSON.parse(dataStr);
              const delta = parsed.choices[0].delta;
              
              if (delta.reasoning_content) {
                this.messages[assistantIndex].reasoning_content += delta.reasoning_content;
                tokenCount++;
              } else if (delta.content) {
                this.messages[assistantIndex].content += delta.content;
                tokenCount++;
              }
            } catch (e) {
              // Parse error
            }
          }
        }

        const elapsed = (performance.now() - startTime) / 1000;
        if (elapsed > 0) {
          this.speedHUD = `${Math.round(tokenCount / elapsed)} tok/s`;
        }
      }

      const totalElapsed = performance.now() - startTime;
      this.latencyHUD = `${Math.round(totalElapsed)}ms`;
      this.statusHUD = 'Done';

      // Update tenant balance after request completes
      await this.loadTenantInfo();

      await this.saveConversation(originalInput);

    } catch (err) {
      this.statusHUD = 'Connection Failed';
      this.messages[assistantIndex].content = `Connection failed: ${err.message}`;
    } finally {
      this.isSending = false;
    }
  }

  async saveConversation(firstPrompt) {
    if (this.messages.length === 0) return;
    const title = this.messages[0].content.substring(0, 35) + (this.messages[0].content.length > 35 ? '...' : '');
    
    try {
      if (this.currentChatId) {
        await fetch(`/api/v1/playground/chats/${this.currentChatId}`, {
          method: 'PUT',
          headers: {
            'Authorization': `Bearer ${this.apiKey}`,
            'Content-Type': 'application/json'
          },
          body: JSON.stringify({
            title: title,
            messages: this.messages
          })
        });
      } else {
        const res = await fetch('/api/v1/playground/chats', {
          method: 'POST',
          headers: {
            'Authorization': `Bearer ${this.apiKey}`,
            'Content-Type': 'application/json'
          },
          body: JSON.stringify({
            title: title,
            messages: this.messages
          })
        });
        if (res.status === 201) {
          const data = await res.json();
          this.currentChatId = data.id;
        }
      }
      this.loadChats();
    } catch (e) {
      console.error('Failed to auto-save conversation', e);
    }
  }

  async startLogsStream() {
    if (this.logsStreaming) return;
    const key = this.getAdminKey();
    if (!key) {
      this.logsError = 'Admin API key is required to stream logs.';
      return;
    }
    if (browser) {
      localStorage.setItem('cag_admin_key', key);
    }
    this.logsError = '';
    this.logsStreaming = true;
    this.logsAbortController = new AbortController();

    try {
      const resp = await fetch('/api/v1/admin/logs/stream', {
        headers: { 'Authorization': `Bearer ${key}` },
        signal: this.logsAbortController.signal,
      });

      if (!resp.ok) {
        const errText = await resp.text();
        this.logsError = `Server error ${resp.status}: ${errText}`;
        this.logsStreaming = false;
        return;
      }

      const reader = resp.body.getReader();
      const decoder = new TextDecoder();
      let buf = '';

      while (this.logsStreaming) {
        const { value, done } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });
        const frames = buf.split('\n\n');
        buf = frames.pop();

        for (const frame of frames) {
          const trimmed = frame.trim();
          if (!trimmed || trimmed.startsWith(': ')) continue;
          if (trimmed.startsWith('data: ')) {
            const raw = trimmed.slice(6).trim();
            try {
              const parsed = JSON.parse(raw);
              this.logLines = [...this.logLines.slice(-499), parsed];
            } catch {
              // Ignore non-JSON
            }
          }
        }
      }
    } catch (err) {
      if (err.name !== 'AbortError') {
        this.logsError = `Stream error: ${err.message}`;
        setTimeout(() => {
          if (!this.logsStreaming) this.startLogsStream();
        }, 3000);
      }
    } finally {
      this.logsStreaming = false;
    }
  }

  stopLogsStream() {
    this.logsStreaming = false;
    this.logsAbortController?.abort();
    this.logsAbortController = null;
  }

  clearLogs() {
    this.logLines = [];
  }

  async downloadTodayLog() {
    const key = this.getAdminKey();
    if (!key) { this.logsError = 'Admin API key required.'; return; }
    this.apiLoading = true;
    try {
      const resp = await fetch('/api/v1/admin/logs/download', {
        headers: { 'Authorization': `Bearer ${key}` },
      });
      if (!resp.ok) {
        this.logsError = `Download failed: ${resp.status}`;
        return;
      }
      const blob = await resp.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      const today = new Date().toISOString().slice(0, 10);
      a.href = url;
      a.download = `gateway-${today}.log`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    } catch (err) {
      this.logsError = `Download error: ${err.message}`;
    } finally {
      this.apiLoading = false;
    }
  }

  logLevelClass(level) {
    switch ((level || '').toLowerCase()) {
      case 'debug': return 'lvl-debug';
      case 'warn':  return 'lvl-warn';
      case 'error': return 'lvl-error';
      case 'fatal': return 'lvl-fatal';
      default:      return 'lvl-info';
    }
  }

  formatLogTime(ts) {
    if (!ts) return '';
    try {
      const d = new Date(ts);
      return d.toTimeString().slice(0, 8) + '.' + String(d.getMilliseconds()).padStart(3, '0');
    } catch { return ts; }
  }
}

export const appState = new AppState();
