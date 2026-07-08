# 🚀 Clever AI Gate

**High-performance AI Router & Orchestration Core** — A blazing-fast bridge between your applications and 1600+ AI models from any provider.

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

---

## ⚡ Performance Characteristics

| Metric | Target |
|--------|--------|
| Internal routing overhead | **< 1ms** p99 |
| Memory per request (hot-path) | **0** heap allocations |
| Credential selection | Lock-free, **< 100ns** |
| Cache lookup | **< 200ns** (Ristretto TinyLFU) |
| Stream TTFT overhead | **< 5ms** added |
| Binary size | **~8MB** (stripped) |

## 🏗️ Architecture

```
[ Client / IDE Extension ]
         │
         │ OpenAI-compatible API
         ▼
[ Clever AI Gate ]
  ├── Zero-alloc JSON extraction (buger/jsonparser)
  ├── Lock-free credential rotation (atomic round-robin)
  ├── Automatic failover with cooldown
  ├── SSE stream transmuxing (any provider → OpenAI format)
  └── Async telemetry pipeline (non-blocking)
         │
         ├──→ OpenAI     ├──→ Anthropic
         ├──→ Google AI   ├──→ DeepSeek
         ├──→ Groq        ├──→ Together
         ├──→ Mistral     ├──→ Azure OpenAI
         ├──→ AWS Bedrock ├──→ Cohere
         └──→ Any OpenAI-compatible provider
```

## ✨ Key Features

- **🔥 Sub-millisecond routing** — Zero-allocation hot-path with `sync.Pool` buffers
- **🔄 Automatic failover** — Transparent retry with credential cooldown on 429/500/503
- **🎯 Lock-free load balancing** — Atomic round-robin across provider API keys
- **📡 Stream transmuxing** — Converts Anthropic/Gemini/DeepSeek streams to OpenAI SSE format
- **🧠 Reasoning support** — `<think>` tags and native `reasoning_content` normalized automatically
- **🔐 Encrypted credentials** — AES-256-GCM encryption for provider API keys at rest
- **📊 Async telemetry** — Non-blocking logging with bulk PostgreSQL writes
- **🔌 Hot reload** — PostgreSQL LISTEN/NOTIFY for zero-downtime config updates
- **📖 Swagger UI** — Interactive API documentation at `/swagger/index.html`

## 🚀 Quick Start

### Prerequisites

- Go 1.22+
- PostgreSQL 16+
- Docker & Docker Compose (optional)

### Using Docker Compose (Recommended)

```bash
# Clone the repository
git clone https://github.com/skadraneshghn/clever-ai-gate.git
cd clever-ai-gate

# Start everything
docker compose up --build -d

# View logs
docker compose logs -f app
```

The server will be available at `http://localhost:8080`.

### Local Development

```bash
# Copy environment template
cp .env.example .env
# Edit .env with your database URL and encryption key

# Run database (if not using Docker)
# createdb clever_ai_gate

# Start the server
go run ./cmd/server

# Or use Make
make run
```

## 📖 API Usage

### 1. Create a Tenant (Admin API)

```bash
curl -X POST http://localhost:8080/api/v1/admin/tenants \
  -H "Authorization: Bearer YOUR_ADMIN_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name": "My App", "rate_limit_rpm": 120}'
```

**Response** (save the `api_key` — shown only once):
```json
{
  "id": "550e8400-...",
  "name": "My App",
  "api_key": "cag_a1b2c3d4e5f6...",
  "token_balance": 1000000000,
  "rate_limit_rpm": 120
}
```

### 2. Create a Model Pool & Add Credentials

```bash
# Create a pool for GPT-4o
curl -X POST http://localhost:8080/api/v1/admin/pools \
  -H "Authorization: Bearer YOUR_ADMIN_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model_pattern": "gpt-4o", "strategy": "round-robin"}'

# Add an OpenAI credential
curl -X POST http://localhost:8080/api/v1/admin/credentials \
  -H "Authorization: Bearer YOUR_ADMIN_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "pool_id": 1,
    "provider": "openai",
    "api_key": "sk-your-openai-key",
    "base_url": "https://api.openai.com",
    "weight": 1
  }'
```

### 3. Send AI Requests (Proxy API)

```bash
# Non-streaming
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer cag_your_tenant_key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'

# Streaming
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer cag_your_tenant_key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Hello!"}],
    "stream": true
  }'
```

### 4. Configure IDE Extensions

Point your IDE extension (Cline, Continue, etc.) to `http://your-server:8080/v1` and use your tenant API key.

## 🏢 Supported Providers

| Provider | Format | Auth | Streaming |
|----------|--------|------|-----------|
| OpenAI | Passthrough | Bearer | ✅ SSE |
| Anthropic | Translated | x-api-key | ✅ Event SSE |
| Google Gemini | Translated | Bearer/API Key | ✅ REST/SSE |
| DeepSeek | Passthrough | Bearer | ✅ SSE |
| Groq | Passthrough | Bearer | ✅ SSE |
| Together | Passthrough | Bearer | ✅ SSE |
| Mistral | Passthrough | Bearer | ✅ SSE |
| Azure OpenAI | Translated | api-key header | ✅ SSE |
| AWS Bedrock | Translated | SigV4/Bearer | ✅ SSE |
| Cohere | Translated | Bearer | ✅ SSE |
| xAI (Grok) | Passthrough | Bearer | ✅ SSE |
| Fireworks | Passthrough | Bearer | ✅ SSE |
| Perplexity | Passthrough | Bearer | ✅ SSE |
| OpenRouter | Passthrough | Bearer | ✅ SSE |
| 1min.ai | Translated | API-KEY | ✅ SSE |

## 📂 Project Structure

```
clever-ai-gate/
├── cmd/server/main.go              # Entry point & bootstrap
├── api/
│   ├── admin/                      # Admin CRUD handlers
│   └── dto/                        # Request/response DTOs
├── internal/
│   ├── cache/                      # Ristretto cache wrapper
│   ├── config/                     # Environment config loader
│   ├── credentials/                # Lock-free pool & AES vault
│   ├── database/                   # pgx pool, migrations, queries
│   ├── health/                     # Liveness & readiness probes
│   ├── middleware/                  # Auth, rate limiting, CORS
│   ├── proxy/                      # Hot-path handler & transport
│   ├── quota/                      # Sliding window estimator
│   ├── router/                     # Gin engine factory
│   └── transmux/                   # Stream format translators
├── Dockerfile                      # Multi-stage production build
├── docker-compose.yml              # Local dev stack
├── Makefile                        # Build & dev commands
└── .env.example                    # Config template
```

## 🧪 Testing

```bash
# Run all tests with race detection
make test

# Run benchmarks
make bench

# Run specific benchmarks
go test ./internal/credentials/... -bench=BenchmarkAcquireToken -benchmem
```

## 🐳 Deployment (Clever Cloud)

1. Create a Docker application on Clever Cloud
2. Add a PostgreSQL add-on
3. Set environment variables:
   - `DATABASE_URL` — from PostgreSQL add-on
   - `MASTER_ENCRYPTION_KEY` — `openssl rand -hex 32`
   - `ADMIN_API_KEY` — `openssl rand -hex 24`
4. Push to deploy:
   ```bash
   clever deploy
   ```

## 📜 License

MIT License — see [LICENSE](LICENSE) for details.
