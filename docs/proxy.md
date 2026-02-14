# Proxy

The AutoDB proxy is a PostgreSQL wire protocol relay that sits between your application and your database. It intercepts AUTODB SQL commands and uses temporary views to route queries to real or generated data per table.

## What the Proxy Does

- **Byte-level relay** — Standard SQL queries are forwarded as raw bytes with no parsing or modification
- **AUTODB command interception** — Queries starting with `CALL autodb.` are intercepted, parsed, and executed locally by the proxy
- **Per-table routing via temp views** — `return_generated` creates a temporary view that shadows the real table; `return_actual` drops it to restore real data access

## Starting the Proxy

```bash
autodb serve --port 5433
```

## Connecting Through the Proxy

Connect to the proxy using any PostgreSQL client, with the same credentials as your real database:

```bash
psql -h localhost -p 5433 -U myuser mydb
```

From your application, change only the host and port in the connection string to point at the proxy.

## Generating Data and Routing

Once connected through the proxy, use AUTODB SQL commands:

```sql
-- Generate synthetic data
CALL autodb.generate_data(table_name => 'users', rows => 500);

-- Route queries for "users" to generated data
CALL autodb.return_generated(table_name => 'users');

-- Switch back to real data
CALL autodb.return_actual(table_name => 'users');
```

## How Routing Works

The proxy uses PostgreSQL temporary views for per-table routing:

- **`return_generated`** creates a `CREATE OR REPLACE TEMP VIEW <table> AS SELECT * FROM <shadow_schema>.<table>`. Because temporary views take priority over base tables in PostgreSQL's name resolution, queries against that table name return generated data.
- **`return_actual`** drops the temporary view with `DROP VIEW IF EXISTS pg_temp.<table>`, restoring normal resolution to the real table.

This approach means:
- Routing is per-session — other connections are unaffected
- Per-table control — each table can be independently toggled
- No SQL parsing needed — PostgreSQL handles the routing natively

## How Standard SQL Passes Through

The proxy does **not** parse standard SQL. When a query does not start with `CALL autodb.` (case-insensitive), the raw bytes are forwarded to the database and the response is relayed back. This means:

- No SQL compatibility issues — any valid PostgreSQL query works
- No performance overhead from SQL parsing
- Transactions, prepared statements, and protocol features work as expected
