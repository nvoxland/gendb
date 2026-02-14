# Getting Started

## Prerequisites

- **Go 1.21+** (for building from source)
- **PostgreSQL** — a real database to introspect
- **Ollama** (recommended for local LLM) — or an OpenAI API key

## Installation

### From source

```bash
go install github.com/nvoxland/autodb/cmd/autodb@latest
```

### Build locally

```bash
git clone https://github.com/nvoxland/autodb.git
cd autodb
make build
# binary is at bin/autodb
```

## Walkthrough

### 1. Start the proxy

Point AutoDB at your real database and start the proxy:

```bash
autodb serve --db-database mydb --port 5433
```

### 2. Connect through the proxy

```bash
psql -h localhost -p 5433 -U user mydb
```

### 3. Switch modes

From any connection through the proxy, send AUTODB SQL commands:

```sql
-- Switch to synthetic data (all queries use autodb_shadow schema)
CALL autodb.mode(mode => 'synthetic');

-- Switch back to real data
CALL autodb.mode(mode => 'real');

-- Switch only specific tables
CALL autodb.mode(mode => 'synthetic', tables => 'users,orders');
```

## Next Steps

- [CLI Reference](cli.md) — all commands and flags
- [AUTODB SQL Reference](autodb-sql.md) — the full DSL
- [Configuration](configuration.md) — customize `autodb.yaml`
- [LLM Setup](llm.md) — configure Ollama, OpenAI, or custom providers
- [Data Generators](generators.md) — available generators and how they're selected
