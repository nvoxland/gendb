# How It Works

This page describes GenDB's architecture and internal mechanics.

## Architecture Overview

```
┌──────────────┐     ┌──────────────────────────┐     ┌──────────────────────┐
│              │     │      GenDB Proxy          │     │   Real PostgreSQL    │
│  Application ├────►│                           ├────►│                      │
│  (psql, app) │     │  ┌────────────────────┐   │     │  ├── public.users    │
│              │◄────┤  │ GENDB SQL Engine    │   │◄────┤  ├── public.orders   │
└──────────────┘     │  │ - Parse DSL         │   │     │  ├── public_gendb    │
                     │  │ - Generate data     │   │     │  │   ├── .users      │
                     │  │ - Temp view routing  │   │     │  │   └── .orders     │
                     │  └────────────────────┘   │     └──────────────────────┘
                     └──────────────────────────┘
```

## Schema-Based Synthetic Database

The synthetic database is a PostgreSQL schema (`public_gendb` by default) created inside your real database:

- **No external dependencies** — no Docker, no separate database instance
- **Same database** — the synthetic schema coexists with your real `public` schema
- **Schema cloning** — your real table structure is reconstructed as `public_gendb.<table>` with synthetic data

### How Routing Works

GenDB uses temporary views to route queries per table:

- **`return_generated`** creates a temporary view with the same name as the real table, pointing at the synthetic schema table. Since temporary views take priority over base tables in PostgreSQL's resolution, subsequent queries against that table name return generated data.
- **`return_actual`** drops the temporary view, restoring normal resolution to the real table.

This approach provides per-table routing with no impact on other sessions or tables.

## Schema Introspection

GenDB introspects your real database to understand its structure:

1. **`information_schema` + `pg_catalog`** — Queries `information_schema.tables`, `information_schema.columns`, and `pg_catalog` views to discover tables, columns, data types, primary keys, foreign keys, and unique constraints.

2. **DDL reconstruction** — GenDB reconstructs schema-qualified `CREATE TABLE` statements from the introspected metadata, targeting the synthetic schema.

3. **Schema exclusion** — During introspection, the synthetic schema is excluded so synthetic tables don't appear as "real" tables.

## LLM-Based Data Generation

GenDB sends your schema to the configured LLM, which generates all data values directly as JSON. The LLM produces realistic, semantically coherent rows based on column names, types, and constraints. Data is generated in batches of up to 50 rows per LLM call.

Config overrides (from `gendb.yaml` table/column settings and column rules) are included as instructions in the LLM prompt.

## Topological Ordering

Tables are generated in topological order based on foreign key relationships:

1. GenDB builds a dependency graph from FK constraints
2. Tables with no dependencies are generated first
3. Dependent tables are generated after their referenced tables
4. FK column values are populated by randomly selecting from the referenced table's already-generated primary key values

This ensures referential integrity without disabling FK constraints.

## Bulk Insert via COPY

Generated data is inserted using PostgreSQL's `COPY` protocol (`pgx.CopyFrom`), which is significantly faster than individual INSERT statements. A single COPY call inserts all rows for a table. Inserts target schema-qualified table names (e.g., `public_gendb.users`).

## Proxy: Byte-Level Relay

The proxy operates at the PostgreSQL wire protocol level:

1. Accepts TCP connections on the configured port
2. Connects to the real database
3. For each incoming message, checks if it starts with `CALL gendb.` (case-insensitive)
4. **GENDB commands** are parsed and executed internally
5. **Standard SQL** is forwarded as raw bytes to the real database
6. Responses from the database are relayed back to the client as-is

This design means the proxy adds minimal latency and has zero SQL compatibility issues — it never parses your queries.
