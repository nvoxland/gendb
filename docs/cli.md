# CLI Reference

## `gendb`

GenDB — synthetic database for development & testing.

Creates a shadow schema with LLM-analyzed synthetic data inside your real PostgreSQL database. Developers can work against realistic data without production PII.

---

## `gendb init`

Initialize GenDB by introspecting the real database.

Connects to the real database, introspects the schema, and creates `gendb.yaml`.

```bash
gendb init --url <connection-string>
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--url` | string | *(required)* | PostgreSQL connection string |

### Example

```bash
gendb init --url postgres://user:pass@localhost:5432/mydb
```

---

## `gendb up`

Create shadow schema, apply tables, and generate synthetic data.

Creates a `gendb_shadow` schema inside your real database, clones the table structure from the `public` schema, and generates synthetic data using the configured LLM.

```bash
gendb up [--rows N] [--seed N]
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--rows` | int | `0` (use config) | Override default row count |
| `--seed` | int64 | `0` (use config) | Override random seed |

### Example

```bash
# Use defaults from gendb.yaml
gendb up

# Generate 500 rows per table with a specific seed
gendb up --rows 500 --seed 12345
```

---

## `gendb down`

No-op — the shadow schema persists inside your database automatically.

The shadow schema lives in the same database as your real data and doesn't need to be "stopped". To remove it, use `gendb destroy`.

```bash
gendb down
```

---

## `gendb destroy`

Drop the shadow schema and all its data.

Drops the `gendb_shadow` schema and all tables within it using `CASCADE`.

```bash
gendb destroy
```

---

## `gendb generate`

Generate synthetic data (without truncating existing data).

Appends new synthetic rows to the shadow schema tables. Requires the shadow schema to exist (`gendb up`).

```bash
gendb generate [--rows N] [--seed N]
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--rows` | int | `0` (use config) | Override default row count |
| `--seed` | int64 | `0` (use config) | Override random seed |

### Example

```bash
# Add another batch of rows
gendb generate

# Add 200 more rows
gendb generate --rows 200
```

---

## `gendb reset`

Truncate and regenerate all synthetic data.

Truncates all tables in the shadow schema, then regenerates fresh data. Requires the shadow schema to exist.

```bash
gendb reset [--rows N] [--seed N]
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--rows` | int | `0` (use config) | Override default row count |
| `--seed` | int64 | `0` (use config) | Override random seed |

### Example

```bash
# Regenerate with defaults
gendb reset

# Regenerate with a specific seed for reproducibility
gendb reset --seed 42
```

---

## `gendb sync`

Re-introspect real DB and apply schema changes to shadow.

Drops the shadow schema, re-creates it, fetches the latest schema from the real database, and applies the reconstructed DDL. Data is not regenerated — run `gendb reset` after syncing.

```bash
gendb sync
```

---

## `gendb status`

Show connection status and shadow schema state.

Displays the real database URL, shadow schema name, LLM provider/model, and whether the shadow schema exists.

```bash
gendb status
```

### Example output

```
Real DB:       postgres://user:pass@localhost:5432/mydb
Shadow schema: gendb_shadow
LLM:           ollama/llama3.2
Shadow DB:     active (schema gendb_shadow exists)
```

---

## `gendb proxy`

Start the PostgreSQL wire protocol proxy.

Listens for PostgreSQL connections and routes them to the real database. GENDB SQL commands are intercepted and executed by the proxy.

```bash
gendb proxy [--port N]
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--port` | int | `5433` | Proxy listen port |

### Example

```bash
# Start on default port
gendb proxy

# Start on a custom port
gendb proxy --port 6000
```
