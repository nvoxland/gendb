# GENDB SQL Reference

GENDB SQL commands use standard PostgreSQL `CALL` procedure syntax with named arguments. Any client that can send SQL — `psql`, application drivers, database GUIs — can send GENDB commands. The proxy intercepts `CALL gendb.*()` statements before they reach PostgreSQL, so no actual procedures need to exist in the database.

Commands are case-insensitive. Trailing semicolons are optional.

---

## Generate

### `CALL gendb.generate_data(...)`

Generate synthetic rows for one or more tables, or all tables if no arguments are given. Data is inserted into the synthetic schema.

Use `include` to limit generation to matching tables and `exclude` to skip matching tables. Both support glob-like matching (`*` and `?` as wildcards) — for example, `user*` matches `users` and `user_roles`. Either, both, or neither can be specified.

```sql
CALL gendb.generate_data(include => 'users', rows => 500);
CALL gendb.generate_data(include => 'order*', rows => 1000, seed => 42);

-- Generate for all tables except audit logs
CALL gendb.generate_data(exclude => '*_log');

-- Combine: include user tables, but exclude user_logs
CALL gendb.generate_data(include => 'user*', exclude => 'user_logs');

-- Generate rows for all tables
CALL gendb.generate_data();
```

---

## Return Generated

### `CALL gendb.return_generated(...)`

Route queries for one or more tables to the generated (synthetic) data. Creates temporary views that overlay the real tables, so subsequent queries against those table names return generated data.

Use `include` and `exclude` the same way as `generate_data` — both support glob patterns. With no arguments, all tables with synthetic data are routed.

```sql
CALL gendb.return_generated(include => 'users');

-- Route all tables with synthetic data
CALL gendb.return_generated();

-- Route user-related tables except user_logs
CALL gendb.return_generated(include => 'user*', exclude => 'user_logs');

-- Now: SELECT * FROM users  →  returns generated data
```

The temporary views only affect the current session. Other connections are unaffected.

---

## Return Actual

### `CALL gendb.return_actual(...)`

Restore one or more tables to return real data. Drops the temporary views created by `return_generated`, so queries resolve against the real tables again.

Uses the same `include`/`exclude` pattern matching. With no arguments, all tables with synthetic data are restored.

```sql
CALL gendb.return_actual(include => 'users');

-- Restore all tables
CALL gendb.return_actual();

-- Restore user-related tables except user_logs
CALL gendb.return_actual(include => 'user*', exclude => 'user_logs');

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
