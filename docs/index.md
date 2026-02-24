# GenDB

**Synthetic database for development & testing** — GenDB creates a synthetic schema inside your PostgreSQL database populated with LLM-analyzed synthetic data, so developers can work against realistic data without production PII.

## Key Features

- **LLM-driven generation** — An LLM analyzes your schema (column names, types, constraints) and picks the best data generator for each column
- **Wire protocol proxy** — A PostgreSQL proxy intercepts connections and uses temporary views to route queries to real or generated data per table
- **GENDB SQL** — Control commands using standard `CALL gendb.*()` syntax, sent through any PostgreSQL connection (`psql`, any driver) to generate data and toggle between real and generated data
- **Full CRUD against synthetic data** — The synthetic schema contains real PostgreSQL tables; reads and writes work normally
- **Reproducible** — Set a seed for deterministic data generation across runs
- **Zero application changes** — Point your app at the proxy and switch between real and synthetic data per table

## Quick Start

```bash
# Start the proxy
gendb serve --db-database mydb --port 5433
```

Then connect through the proxy, generate data, and toggle routing per table:

```sql
CALL gendb.generate_data(include => 'users', rows => 500);
CALL gendb.return_generated(include => 'users');
-- queries against "users" now return generated data

CALL gendb.return_actual(include => 'users');
-- queries against "users" return real data again
```

[Get started →](getting-started/index.md)
