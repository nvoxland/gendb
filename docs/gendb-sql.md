# GENDB SQL Reference

GENDB SQL commands use standard PostgreSQL `CALL` procedure syntax with named arguments. Any client that can send SQL — `psql`, application drivers, database GUIs — can send GENDB commands. The proxy intercepts `CALL gendb.*()` statements before they reach PostgreSQL, so no actual procedures need to exist in the database.

Commands are case-insensitive. Trailing semicolons are optional.

---

## Generate

### `CALL gendb.generate_data(...)`

Generate synthetic rows for a specific table, or all tables if no arguments are given. Data is inserted into the shadow schema.

```sql
CALL gendb.generate_data(table_name => 'users', rows => 500);
CALL gendb.generate_data(table_name => 'orders', rows => 1000, seed => 42);

-- Generate rows for all tables
CALL gendb.generate_data();
```

!!! note
    `CALL gendb.regenerate_data(...)` is accepted as an alias for `CALL gendb.generate_data(...)`.

---

## Return Generated

### `CALL gendb.return_generated(table_name => '...')`

Route queries for a table to the generated (shadow) data. Creates a temporary view that shadows the real table, so subsequent queries against that table name return generated data.

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
