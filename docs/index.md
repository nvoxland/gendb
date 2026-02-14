# AutoDB

**Synthetic database for development & testing** — AutoDB creates a shadow schema inside your PostgreSQL database populated with LLM-analyzed synthetic data, so developers can work against realistic data without production PII.

## Key Features

- **LLM-driven generation** — An LLM analyzes your schema (column names, types, constraints) and picks the best data generator for each column
- **Wire protocol proxy** — A PostgreSQL proxy intercepts connections and uses temporary views to route queries to real or generated data per table
- **AUTODB SQL** — Control commands using standard `CALL autodb.*()` syntax, sent through any PostgreSQL connection (`psql`, any driver) to generate data and toggle between real and generated data
- **Full CRUD against synthetic data** — The shadow schema contains real PostgreSQL tables; reads and writes work normally
- **Reproducible** — Set a seed for deterministic data generation across runs
- **Zero application changes** — Point your app at the proxy and switch between real and synthetic data per table

## Quick Start

```bash
# Start the proxy
autodb serve --db-database mydb --port 5433
```

Then connect through the proxy, generate data, and toggle routing per table:

```sql
CALL autodb.generate_data(table_name => 'users', rows => 500);
CALL autodb.return_generated(table_name => 'users');
-- queries against "users" now return generated data

CALL autodb.return_actual(table_name => 'users');
-- queries against "users" return real data again
```

[Get started →](getting-started.md)
