# AutoDB

**Synthetic database for development & testing** — AutoDB creates a shadow schema inside your PostgreSQL database populated with LLM-analyzed synthetic data, so developers can work against realistic data without production PII.

## Key Features

- **LLM-driven generation** — An LLM analyzes your schema (column names, types, constraints) and picks the best data generator for each column
- **Wire protocol proxy** — A PostgreSQL proxy intercepts connections and routes queries to your real or shadow database
- **AUTODB SQL** — A control language you send through any standard PostgreSQL connection (`psql`, any driver) to switch modes, regenerate data, and configure generators on the fly
- **Full CRUD against synthetic data** — The shadow schema contains real PostgreSQL tables; reads and writes work normally
- **Reproducible** — Set a seed for deterministic data generation across runs
- **Zero application changes** — Point your app at the proxy and switch between real and synthetic data with a single command

## Quick Start

```bash
# Initialize from your real database
autodb init --url postgres://user:pass@localhost:5432/mydb

# Create the shadow schema, clone tables, generate data
autodb up
```

Then query shadow data directly (`SELECT * FROM autodb_shadow.<table>`), or start the proxy and switch modes on the fly.

[Get started →](getting-started.md)
