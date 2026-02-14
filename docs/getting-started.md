# Getting Started

## Prerequisites

- **Go 1.21+** (for building from source)
- **PostgreSQL** — a real database to introspect
- **Ollama** (recommended for local LLM) — or an OpenAI API key

## Installation

### From source

```bash
go install github.com/nvoxland/gendb/cmd/gendb@latest
```

### Build locally

```bash
git clone https://github.com/nvoxland/gendb.git
cd gendb
make build
# binary is at bin/gendb
```

## Walkthrough

### 1. Start the proxy

Point GenDB at your real database and start the proxy:

```bash
gendb serve --db-database mydb --port 5433
```

### 2. Connect through the proxy

```bash
psql -h localhost -p 5433 -U user mydb
```

### 3. Generate data and toggle routing

From any connection through the proxy, send GENDB SQL commands:

```sql
-- Generate synthetic data for a table
CALL gendb.generate_data(table_name => 'users', rows => 500);

-- Route queries for "users" to the generated data
CALL gendb.return_generated(table_name => 'users');

-- Switch back to real data
CALL gendb.return_actual(table_name => 'users');
```

## Next Steps

- [CLI Reference](cli.md) — all commands and flags
- [GENDB SQL Reference](gendb-sql.md) — the full DSL
- [Configuration](configuration.md) — customize `gendb.yaml`
- [LLM Setup](llm.md) — configure Ollama, OpenAI, or custom providers
- [Data Generators](generators.md) — available generators and how they're selected
