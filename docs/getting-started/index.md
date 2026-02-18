# Getting Started

## Prerequisites

- **Go 1.21+** (for building from source)
- **PostgreSQL** — a real database to introspect
- **Ollama** (recommended for local LLM) — or an OpenAI API key

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
CALL gendb.generate_data(include_tables => 'users', rows => 500);

-- Route queries for "users" to the generated data
CALL gendb.return_generated(table_name => 'users');

-- Switch back to real data
CALL gendb.return_actual(table_name => 'users');
```

## Next Steps

- [CLI Reference](../guide/cli.md) — all commands and flags
- [GENDB SQL Reference](../guide/gendb-sql.md) — the full DSL
- [Configuration](../guide/configuration.md) — customize `gendb.yaml`
- [LLM Setup](../guide/llm.md) — configure Ollama, OpenAI, or custom providers
- [Data Generators](../guide/generators.md) — available generators and how they're selected
