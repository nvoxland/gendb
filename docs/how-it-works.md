# How It Works

This page describes AutoDB's architecture and internal mechanics.

## Architecture Overview

```
┌──────────────┐     ┌──────────────────────────┐     ┌──────────────────────┐
│              │     │      AutoDB Proxy         │     │   Real PostgreSQL    │
│  Application ├────►│                           ├────►│                      │
│  (psql, app) │     │  ┌────────────────────┐   │     │  ├── public.users    │
│              │◄────┤  │ AUTODB SQL Engine   │   │◄────┤  ├── public.orders   │
└──────────────┘     │  │ - Parse DSL         │   │     │  ├── public_autodb   │
                     │  │ - Generate data     │   │     │  │   ├── .users      │
                     │  │ - Temp view routing  │   │     │  │   └── .orders     │
                     │  └────────────────────┘   │     └──────────────────────┘
                     └──────────────────────────┘
```

## Schema-Based Shadow

The shadow database is a PostgreSQL schema (`public_autodb` by default) created inside your real database:

- **No external dependencies** — no Docker, no separate database instance
- **Same database** — the shadow schema coexists with your real `public` schema
- **Schema cloning** — your real table structure is reconstructed as `public_autodb.<table>` with synthetic data

### How Routing Works

AutoDB uses temporary views to route queries per table:

- **`return_generated`** creates a temporary view with the same name as the real table, pointing at the shadow schema table. Since temporary views take priority over base tables in PostgreSQL's resolution, subsequent queries against that table name return generated data.
- **`return_actual`** drops the temporary view, restoring normal resolution to the real table.

This approach provides per-table routing with no impact on other sessions or tables.

## Schema Introspection

AutoDB introspects your real database to understand its structure:

1. **`information_schema` + `pg_catalog`** — Queries `information_schema.tables`, `information_schema.columns`, and `pg_catalog` views to discover tables, columns, data types, primary keys, foreign keys, and unique constraints.

2. **DDL reconstruction** — AutoDB reconstructs schema-qualified `CREATE TABLE` statements from the introspected metadata, targeting the shadow schema.

3. **Schema exclusion** — During introspection, the shadow schema is excluded so shadow tables don't appear as "real" tables.

## Two-Phase Generation

Data generation happens in two phases:

### Phase 1: LLM Schema Analysis

AutoDB sends your schema to the configured LLM with a prompt that describes all available [generators](generators.md). The LLM returns a JSON generation plan:

```json
{
  "tables": {
    "users": {
      "columns": {
        "id": {"generator": "skip"},
        "first_name": {"generator": "person.first_name"},
        "last_name": {"generator": "person.last_name"},
        "email": {"generator": "internet.email",
                  "template": "{first_name}.{last_name}@example.com"},
        "created_at": {"generator": "time.recent", "params": {"days": "365"}}
      }
    }
  }
}
```

The LLM understands column semantics — it knows that `first_name` should use `person.first_name`, not a random string.

### Phase 2: Local Generation

The generation plan is executed locally using [gofakeit](https://github.com/brianvoe/gofakeit). No LLM calls are made during this phase (unless a column uses `generator: llm`). This makes generation fast and free.

Config overrides (from `autodb.yaml` table/column settings and column rules) are applied on top of the LLM's plan before execution.

## Topological Ordering

Tables are generated in topological order based on foreign key relationships:

1. AutoDB builds a dependency graph from FK constraints
2. Tables with no dependencies are generated first
3. Dependent tables are generated after their referenced tables
4. FK column values are populated by randomly selecting from the referenced table's already-generated primary key values

This ensures referential integrity without disabling FK constraints.

## Bulk Insert via COPY

Generated data is inserted using PostgreSQL's `COPY` protocol (`pgx.CopyFrom`), which is significantly faster than individual INSERT statements. A single COPY call inserts all rows for a table. Inserts target schema-qualified table names (e.g., `public_autodb.users`).

## Proxy: Byte-Level Relay

The proxy operates at the PostgreSQL wire protocol level:

1. Accepts TCP connections on the configured port
2. Connects to the real database
3. For each incoming message, checks if it starts with `CALL autodb.` (case-insensitive)
4. **AUTODB commands** are parsed and executed internally
5. **Standard SQL** is forwarded as raw bytes to the real database
6. Responses from the database are relayed back to the client as-is

This design means the proxy adds minimal latency and has zero SQL compatibility issues — it never parses your queries.
