# CLI Reference

## `autodb`

AutoDB — synthetic database for development & testing.

Creates a shadow schema with LLM-analyzed synthetic data inside your real PostgreSQL database. Developers can work against realistic data without production PII.

---

## `autodb init`

Initialize AutoDB by introspecting the real database.

Connects to the real database, introspects the schema, and creates `autodb.yaml`.

```bash
autodb init --url <connection-string>
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--url` | string | *(required)* | PostgreSQL connection string |

### Example

```bash
autodb init --url postgres://user:pass@localhost:5432/mydb
```

---

## `autodb up`

Create shadow schema, apply tables, and generate synthetic data.

Creates an `autodb_shadow` schema inside your real database, clones the table structure from the `public` schema, and generates synthetic data using the configured LLM.

```bash
autodb up [--rows N] [--seed N]
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--rows` | int | `0` (use config) | Override default row count |
| `--seed` | int64 | `0` (use config) | Override random seed |

### Example

```bash
# Use defaults from autodb.yaml
autodb up

# Generate 500 rows per table with a specific seed
autodb up --rows 500 --seed 12345
```

---

## `autodb down`

No-op — the shadow schema persists inside your database automatically.

The shadow schema lives in the same database as your real data and doesn't need to be "stopped". To remove it, use `autodb destroy`.

```bash
autodb down
```

---

## `autodb destroy`

Drop the shadow schema and all its data.

Drops the `autodb_shadow` schema and all tables within it using `CASCADE`.

```bash
autodb destroy
```

---

## `autodb generate`

Generate synthetic data (without truncating existing data).

Appends new synthetic rows to the shadow schema tables. Requires the shadow schema to exist (`autodb up`).

```bash
autodb generate [--rows N] [--seed N]
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--rows` | int | `0` (use config) | Override default row count |
| `--seed` | int64 | `0` (use config) | Override random seed |

### Example

```bash
# Add another batch of rows
autodb generate

# Add 200 more rows
autodb generate --rows 200
```

---

## `autodb reset`

Truncate and regenerate all synthetic data.

Truncates all tables in the shadow schema, then regenerates fresh data. Requires the shadow schema to exist.

```bash
autodb reset [--rows N] [--seed N]
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--rows` | int | `0` (use config) | Override default row count |
| `--seed` | int64 | `0` (use config) | Override random seed |

### Example

```bash
# Regenerate with defaults
autodb reset

# Regenerate with a specific seed for reproducibility
autodb reset --seed 42
```

---

## `autodb sync`

Re-introspect real DB and apply schema changes to shadow.

Drops the shadow schema, re-creates it, fetches the latest schema from the real database, and applies the reconstructed DDL. Data is not regenerated — run `autodb reset` after syncing.

```bash
autodb sync
```

---

## `autodb status`

Show connection status and shadow schema state.

Displays the real database URL, shadow schema name, LLM provider/model, and whether the shadow schema exists.

```bash
autodb status
```

### Example output

```
Real DB:       postgres://user:pass@localhost:5432/mydb
Shadow schema: autodb_shadow
LLM:           ollama/llama3.2
Shadow DB:     active (schema autodb_shadow exists)
```

---

## `autodb proxy`

Start the PostgreSQL wire protocol proxy.

Listens for PostgreSQL connections and routes them to the real database. Uses `SET search_path` to switch between real and synthetic data based on the current mode. AUTODB SQL commands are intercepted and executed by the proxy.

```bash
autodb proxy [--port N]
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--port` | int | `5433` | Proxy listen port |

### Example

```bash
# Start on default port
autodb proxy

# Start on a custom port
autodb proxy --port 6000
```
