# GENDB SQL Reference

GENDB SQL commands use standard PostgreSQL `CALL` procedure syntax with named arguments. Any client that can send SQL — `psql`, application drivers, database GUIs — can send GENDB commands. The proxy intercepts `CALL gendb.*()` statements before they reach PostgreSQL, so no actual procedures need to exist in the database.

Commands are case-insensitive. Trailing semicolons are optional.

---

## Generate

### `CALL gendb.generate_data(...)`

Generate synthetic rows for one or more tables, or all tables if no arguments are given. Data is inserted into the synthetic schema.

Use `include_tables` to limit generation to matching tables and `exclude_tables` to skip matching tables. Both support glob-like matching (`*` and `?` as wildcards) — for example, `user*` matches `users` and `user_roles`. Either, both, or neither can be specified.

```sql
CALL gendb.generate_data(include_tables => 'users', rows => 500);
CALL gendb.generate_data(include_tables => 'order*', rows => 1000, seed => 42);

-- Generate for all tables except audit logs
CALL gendb.generate_data(exclude_tables => '*_log');

-- Combine: include user tables, but exclude user_logs
CALL gendb.generate_data(include_tables => 'user*', exclude_tables => 'user_logs');

-- Generate rows for all tables
CALL gendb.generate_data();
```

---

## Return Generated

### `CALL gendb.return_generated(table_name => '...')`

Route queries for a table to the generated (synthetic) data. Creates a temporary view that overlays the real table, so subsequent queries against that table name return generated data.

```sql
CALL gendb.return_generated(table_name => 'users');

-- Now: SELECT * FROM users  →  returns generated data
```

The temporary view only affects the current session. Other connections are unaffected.

---

## Return Actual

### `CALL gendb.return_actual(table_name => '...')`

Restore a table to return real data. Drops the temporary view created by `return_generated`, so queries resolve against the real table again.

```sql
CALL gendb.return_actual(table_name => 'users');

-- Now: SELECT * FROM users  →  returns real data
```

---

## Sync

### `CALL gendb.sync(...)`

Re-synchronize synthetic tables with the current real schema. For each synthetic table, GenDB inspects the real table, drops the old synthetic table, and recreates it with the updated DDL. Existing generated data is removed — run `generate_data` again after syncing.

```sql
-- Sync all synthetic tables
CALL gendb.sync();

-- Sync a specific table
CALL gendb.sync(table_name => 'users');

-- Sync a specific table in a specific scenario
CALL gendb.sync(table_name => 'users', scenario => 'edge');
```

---

## Drop Scenario

### `CALL gendb.drop_scenario(scenario => '...')`

Drop all synthetic tables for a given scenario. Optionally filter by source schema.

```sql
-- Drop all synthetic tables for the 'edge' scenario
CALL gendb.drop_scenario(scenario => 'edge');

-- Drop only synthetic tables from 'public' schema for 'edge' scenario
CALL gendb.drop_scenario(scenario => 'edge', schema => 'public');
```
