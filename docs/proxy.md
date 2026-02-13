# Proxy

The AutoDB proxy is a PostgreSQL wire protocol relay that sits between your application and your database. It intercepts AUTODB SQL commands and uses `SET search_path` to route standard SQL to either real or synthetic data.

## What the Proxy Does

- **Byte-level relay** — Standard SQL queries are forwarded as raw bytes with no parsing or modification
- **AUTODB command interception** — Queries starting with `AUTODB` are intercepted, parsed, and executed locally by the proxy
- **Search path routing** — Based on the current mode (real or synthetic), the proxy sets `search_path` to control which schema queries resolve against
- **Per-table routing** — Different tables can be routed to different schemas simultaneously via PostgreSQL's schema resolution

## Starting the Proxy

```bash
autodb proxy [--port 5433]
```

The proxy requires `autodb.yaml` to be present (created by `autodb init`). It reads the real database URL and shadow schema name from the config.

## Connecting Through the Proxy

Connect to the proxy using any PostgreSQL client, with the same credentials as your real database:

```bash
psql -h localhost -p 5433 -U myuser mydb
```

From your application, change only the host and port in the connection string to point at the proxy.

## Mode Switching

Once connected through the proxy, switch modes with AUTODB SQL:

```sql
-- All queries resolve against the shadow schema (autodb_shadow)
AUTODB MODE SYNTHETIC;

-- All queries resolve against the real schema (public)
AUTODB MODE REAL;

-- Switch only specific tables
AUTODB MODE SYNTHETIC FOR TABLE users, orders;
AUTODB MODE REAL FOR TABLE payments;
```

## How Routing Works

The proxy uses PostgreSQL's `search_path` to control query routing:

- **SYNTHETIC mode**: `SET search_path TO autodb_shadow, public` — unqualified table references resolve to `autodb_shadow` first, falling through to `public` if the table doesn't exist in the shadow schema
- **REAL mode**: `SET search_path TO public` — all queries resolve against the real data

This approach means:
- No SQL parsing needed — PostgreSQL handles the routing natively
- Per-table routing works naturally — only tables that exist in `autodb_shadow` get synthetic data
- Explicitly qualified queries (e.g., `SELECT * FROM public.users`) always hit real data

## Per-Table Routing

When you set mode for specific tables, only those tables change. All other tables keep their current routing:

```sql
-- Start in real mode (default)
-- Now switch only users to synthetic
AUTODB MODE SYNTHETIC FOR TABLE users;

-- Queries against "users" → autodb_shadow.users (synthetic)
-- Queries against everything else → public.* (real data)
```

## On-the-Fly Configuration

All [AUTODB SQL](autodb-sql.md) commands work through the proxy. You can reconfigure generators, regenerate data, and manage profiles without restarting anything:

```sql
-- Change a column's generator
AUTODB SET TABLE users COLUMN email GENERATOR 'internet.email';

-- Regenerate data with the new settings
AUTODB RESET ALL;

-- Check the generation plan
AUTODB SHOW GENERATION PLAN FOR TABLE users;
```

## How Standard SQL Passes Through

The proxy does **not** parse standard SQL. When a query does not start with `AUTODB` (case-insensitive), the raw bytes are forwarded to the database and the response is relayed back. This means:

- No SQL compatibility issues — any valid PostgreSQL query works
- No performance overhead from SQL parsing
- Transactions, prepared statements, and protocol features work as expected
