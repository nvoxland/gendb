# How It Works

This page describes AutoDB's architecture and internal mechanics.

## Architecture Overview

```
┌──────────────┐     ┌──────────────────────────┐     ┌──────────────────────┐
│              │     │      AutoDB Proxy         │     │   Real PostgreSQL    │
│  Application ├────►│                           ├────►│                      │
│  (psql, app) │     │  ┌────────────────────┐   │     │  ├── public.users    │
│              │◄────┤  │ AUTODB SQL Engine   │   │◄────┤  ├── public.orders   │
└──────────────┘     │  │ - Parse DSL         │   │     │  ├── autodb_shadow   │
                     │  │ - Mode switching    │   │     │  │   ├── .users      │
                     │  │ - search_path ctrl  │   │     │  │   └── .orders     │
                     │  └────────────────────┘   │     └──────────────────────┘
                     │                           │
                     │  SYNTHETIC: search_path   │
                     │    → autodb_shadow, public │
                     │  REAL: search_path        │
                     │    → public               │
                     └──────────────────────────┘
```

## Schema-Based Shadow

The shadow database is a PostgreSQL schema (`autodb_shadow` by default) created inside your real database:

- **No external dependencies** — no Docker, no separate database instance
- **Same database** — the shadow schema coexists with your real `public` schema
- **Schema cloning** — your real table structure is reconstructed as `autodb_shadow.<table>` with synthetic data
- **Search path routing** — the proxy uses `SET search_path` to control which schema queries resolve against

### How Search Path Works

When the proxy switches to `SYNTHETIC` mode, it sends:
```sql
SET search_path TO autodb_shadow, public
```

Unqualified queries like `SELECT * FROM users` now resolve to `autodb_shadow.users`. If a table doesn't exist in `autodb_shadow`, it falls through to `public` — this enables per-table routing naturally.

When switching back to `REAL` mode:
```sql
SET search_path TO public
```

### Per-Table Routing

For per-table mode (`CALL autodb.mode(mode => 'synthetic', tables => 'users')`), only the specified tables exist in the shadow schema. Other tables fall through to `public` via the search path.

## Schema Introspection

AutoDB introspects your real database to understand its structure:

1. **`information_schema` + `pg_catalog`** — Queries `information_schema.tables`, `information_schema.columns`, and `pg_catalog` views to discover tables, columns, data types, primary keys, foreign keys, and unique constraints.

2. **DDL reconstruction** — AutoDB reconstructs schema-qualified `CREATE TABLE` statements from the introspected metadata, targeting the shadow schema.

3. **Schema exclusion** — During introspection, the `autodb_shadow` schema is excluded so shadow tables don't appear as "real" tables.

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

Generated data is inserted using PostgreSQL's `COPY` protocol (`pgx.CopyFrom`), which is significantly faster than individual INSERT statements. A single COPY call inserts all rows for a table. Inserts target schema-qualified table names (e.g., `autodb_shadow.users`).

## Proxy: Byte-Level Relay

The proxy operates at the PostgreSQL wire protocol level:

1. Accepts TCP connections on the configured port
2. Connects to the real database and injects `SET search_path` based on the current mode
3. For each incoming message, checks if it starts with `CALL autodb.` (case-insensitive)
4. **AUTODB commands** are parsed and executed internally
5. **Standard SQL** is forwarded as raw bytes to the real database
6. Responses from the database are relayed back to the client as-is
7. On mode changes, a new `SET search_path` is injected transparently

This design means the proxy adds minimal latency and has zero SQL compatibility issues — it never parses your queries.
