# AUTODB SQL Reference

AUTODB SQL is a control language sent through standard PostgreSQL connections. Any client that can send SQL — `psql`, application drivers, database GUIs — can send AUTODB commands.

Commands are case-insensitive. Trailing semicolons are optional.

---

## Mode

### `AUTODB MODE SYNTHETIC|REAL`

Switch the proxy to route queries to the synthetic (shadow) or real database.

```sql
-- Route all queries to the shadow database
AUTODB MODE SYNTHETIC;

-- Route all queries to the real database
AUTODB MODE REAL;
```

### `AUTODB MODE ... FOR TABLE`

Switch mode for specific tables only. Unlisted tables keep their current mode.

```sql
AUTODB MODE SYNTHETIC FOR TABLE users, orders;
AUTODB MODE REAL FOR TABLE payments;
```

---

## Generate

### `AUTODB GENERATE TABLE <name> [ROWS <n>] [SEED <n>]`

Generate (append) synthetic rows for a specific table.

```sql
AUTODB GENERATE TABLE users ROWS 500;
AUTODB GENERATE TABLE orders ROWS 1000 SEED 42;
```

### `AUTODB GENERATE ALL [ROWS <n>]`

Generate rows for all tables.

```sql
AUTODB GENERATE ALL ROWS 200;
```

!!! note
    `AUTODB REGENERATE` is accepted as an alias for `AUTODB GENERATE`.

---

## Reset

### `AUTODB RESET TABLE <name>`

Truncate and regenerate a single table.

```sql
AUTODB RESET TABLE users;
```

### `AUTODB RESET ALL`

Truncate and regenerate all tables.

```sql
AUTODB RESET ALL;
```

---

## Set

### `AUTODB SET MODEL '<name>' [KEY '<key>']`

Configure the LLM model. Provide an API key for cloud providers.

```sql
AUTODB SET MODEL 'gpt-4o-mini' KEY 'sk-...';
```

### `AUTODB SET MODEL LOCAL`

Switch to the local Ollama model.

```sql
AUTODB SET MODEL LOCAL;
```

### `AUTODB SET SEED <n>`

Set the random seed for reproducible generation.

```sql
AUTODB SET SEED 42;
```

### `AUTODB SET DEFAULT_ROWS <n>`

Set the default number of rows generated per table.

```sql
AUTODB SET DEFAULT_ROWS 500;
```

### `AUTODB SET TABLE <table> COLUMN <column> GENERATOR '<generator>' [PROMPT '<prompt>'] [VALUES (...)]`

Override the generator for a specific column.

```sql
-- Use a specific generator
AUTODB SET TABLE users COLUMN email GENERATOR 'internet.email';

-- Use LLM with a prompt
AUTODB SET TABLE users COLUMN bio GENERATOR 'llm' PROMPT 'Write a short professional bio';

-- Use a fixed set of values
AUTODB SET TABLE users COLUMN role GENERATOR 'one_of' VALUES ('admin', 'user', 'moderator');
```

---

## Show

### `AUTODB SHOW STATUS`

Display current mode, connection info, and shadow DB state.

```sql
AUTODB SHOW STATUS;
```

### `AUTODB SHOW TABLES`

List all tables known to AutoDB.

```sql
AUTODB SHOW TABLES;
```

### `AUTODB SHOW CONFIG`

Display the current configuration.

```sql
AUTODB SHOW CONFIG;
```

### `AUTODB SHOW TABLE <name>`

Show details for a specific table (columns, types, constraints).

```sql
AUTODB SHOW TABLE users;
```

### `AUTODB SHOW GENERATION PLAN [FOR TABLE <name>]`

Display the generation plan — which generator is assigned to each column.

```sql
-- Show plan for all tables
AUTODB SHOW GENERATION PLAN;

-- Show plan for a specific table
AUTODB SHOW GENERATION PLAN FOR TABLE users;
```

### `AUTODB SHOW PROFILES`

List all saved profiles.

```sql
AUTODB SHOW PROFILES;
```

---

## Sync

### `AUTODB SYNC SCHEMA`

Re-introspect the real database and apply schema changes to the shadow database.

```sql
AUTODB SYNC SCHEMA;
```

---

## Profiles

Profiles let you save named configurations for how many rows to generate per table.

### `AUTODB CREATE PROFILE '<name>' (TABLE <t> ROWS <n>, ...)`

Create a named profile.

```sql
AUTODB CREATE PROFILE 'small' (TABLE users ROWS 10, TABLE orders ROWS 50);
AUTODB CREATE PROFILE 'large' (TABLE users ROWS 10000, TABLE orders ROWS 50000);
```

### `AUTODB USE PROFILE '<name>'`

Activate a profile.

```sql
AUTODB USE PROFILE 'small';
```

### `AUTODB DROP PROFILE '<name>'`

Delete a profile.

```sql
AUTODB DROP PROFILE 'small';
```
