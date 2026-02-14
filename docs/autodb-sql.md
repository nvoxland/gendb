# AUTODB SQL Reference

AUTODB SQL commands use standard PostgreSQL `CALL` procedure syntax with named arguments. Any client that can send SQL — `psql`, application drivers, database GUIs — can send AUTODB commands. The proxy intercepts `CALL autodb.*()` statements before they reach PostgreSQL, so no actual procedures need to exist in the database.

Commands are case-insensitive. Trailing semicolons are optional.

---

## Mode

### `CALL autodb.mode(mode => '...')`

Switch the proxy to route queries to the synthetic (shadow) or real database.

```sql
-- Route all queries to the shadow database
CALL autodb.mode(mode => 'synthetic');

-- Route all queries to the real database
CALL autodb.mode(mode => 'real');
```

### `CALL autodb.mode(mode => '...', tables => '...')`

Switch mode for specific tables only. Unlisted tables keep their current mode.

```sql
CALL autodb.mode(mode => 'synthetic', tables => 'users,orders');
CALL autodb.mode(mode => 'real', tables => 'payments');
```

---

## Generate

### `CALL autodb.generate_data(...)`

Generate (append) synthetic rows for a specific table, or all tables if no arguments are given.

```sql
CALL autodb.generate_data(table_name => 'users', rows => 500);
CALL autodb.generate_data(table_name => 'orders', rows => 1000, seed => 42);

-- Generate rows for all tables
CALL autodb.generate_data();
```

!!! note
    `CALL autodb.regenerate_data(...)` is accepted as an alias for `CALL autodb.generate_data(...)`.

---

## Reset

### `CALL autodb.reset(...)`

Truncate and regenerate data. Pass a `table_name` to reset a single table, or call with no arguments to reset all tables.

```sql
-- Reset a single table
CALL autodb.reset(table_name => 'users');

-- Reset all tables
CALL autodb.reset();
```

---

## Set

### `CALL autodb.set_model(name => '...', key => '...')`

Configure the LLM model. Provide an API key for cloud providers.

```sql
CALL autodb.set_model(name => 'gpt-4o-mini', key => 'sk-...');
```

### `CALL autodb.set_model(name => 'local')`

Switch to the local Ollama model.

```sql
CALL autodb.set_model(name => 'local');
```

### `CALL autodb.set_seed(value => ...)`

Set the random seed for reproducible generation.

```sql
CALL autodb.set_seed(value => 42);
```

### `CALL autodb.set_default_rows(value => ...)`

Set the default number of rows generated per table.

```sql
CALL autodb.set_default_rows(value => 500);
```

### `CALL autodb.set_column(...)`

Override the generator for a specific column.

```sql
-- Use a specific generator
CALL autodb.set_column(table_name => 'users', column_name => 'email', generator => 'internet.email');

-- Use LLM with a prompt
CALL autodb.set_column(table_name => 'users', column_name => 'bio', generator => 'llm', prompt => 'Write a short professional bio');

-- Use a fixed set of values
CALL autodb.set_column(table_name => 'users', column_name => 'role', generator => 'one_of', values => 'admin,user,moderator');
```

---

## Show

### `CALL autodb.show_status()`

Display current mode, connection info, and shadow DB state.

```sql
CALL autodb.show_status();
```

### `CALL autodb.show_tables()`

List all tables known to AutoDB.

```sql
CALL autodb.show_tables();
```

### `CALL autodb.show_config()`

Display the current configuration.

```sql
CALL autodb.show_config();
```

### `CALL autodb.show_table(table_name => '...')`

Show details for a specific table (columns, types, constraints).

```sql
CALL autodb.show_table(table_name => 'users');
```

### `CALL autodb.show_generation_plan(...)`

Display the generation plan — which generator is assigned to each column.

```sql
-- Show plan for all tables
CALL autodb.show_generation_plan();

-- Show plan for a specific table
CALL autodb.show_generation_plan(table_name => 'users');
```

### `CALL autodb.show_profiles()`

List all saved profiles.

```sql
CALL autodb.show_profiles();
```

---

## Sync

### `CALL autodb.sync_schema()`

Re-introspect the real database and apply schema changes to the shadow database.

```sql
CALL autodb.sync_schema();
```

---

## Profiles

Profiles let you save named configurations for how many rows to generate per table.

### `CALL autodb.create_profile(name => '...', tables => '...')`

Create a named profile. Tables are specified as `table:rows` pairs separated by commas.

```sql
CALL autodb.create_profile(name => 'small', tables => 'users:10,orders:50');
CALL autodb.create_profile(name => 'large', tables => 'users:10000,orders:50000');
```

### `CALL autodb.use_profile(name => '...')`

Activate a profile.

```sql
CALL autodb.use_profile(name => 'small');
```

### `CALL autodb.drop_profile(name => '...')`

Delete a profile.

```sql
CALL autodb.drop_profile(name => 'small');
```
