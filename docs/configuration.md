# Configuration

AutoDB is configured via `autodb.yaml`, created by `autodb init`. All settings can also be changed at runtime using [AUTODB SQL](autodb-sql.md) commands.

## Precedence

Settings are applied in this order (highest priority first):

1. **AUTODB SQL commands** — runtime overrides via the proxy
2. **`autodb.yaml`** — file-based configuration
3. **LLM recommendations** — the generation plan produced by schema analysis
4. **Type-based defaults** — fallback generators based on column data type

## Full Reference

```yaml
connection:
  real:
    url: postgres://user:pass@localhost:5432/mydb  # (1)

  shadow:
    schema: autodb_shadow   # (2) Schema name for synthetic data

llm:
  provider: ollama        # ollama | openai | custom (3)
  model: llama3.2         # Model name
  base_url: http://localhost:11434/v1  # LLM API endpoint
  api_key: ""             # API key (required for openai/custom)

generation:
  default_rows: 100       # Rows per table unless overridden (4)
  seed: 42                # Random seed for reproducibility

  tables:                 # Per-table overrides (5)
    users:
      rows: 500
      columns:
        bio:
          generator: llm
          prompt: "Write a short professional bio"
        role:
          generator: one_of
          values: ["admin", "user", "moderator"]
        email:
          generator: internet.email
          template: "{first_name}.{last_name}@example.com"
    orders:
      rows: 2000

  column_rules:           # Pattern-based rules (6)
    - pattern: "*_sku"
      generator: regex
      format: "[A-Z]{3}-[0-9]{6}"
    - pattern: "*_email"
      generator: internet.email
```

### Notes

1. **`connection.real.url`** — PostgreSQL connection string for your real database. Set by `autodb init --url`.

2. **`connection.shadow.schema`** — Name of the schema created inside your real database for synthetic data. Defaults to `autodb_shadow`. The shadow schema lives in the same database as your real data; the proxy uses `SET search_path` to route queries.

3. **`llm.provider`** — LLM provider for schema analysis:
    - `ollama` (default) — local Ollama instance
    - `openai` — OpenAI API
    - `custom` — any OpenAI-compatible endpoint

4. **`generation.default_rows`** — Default number of rows to generate per table. Can be overridden per-table in the `tables` section.

5. **`generation.tables`** — Per-table configuration. Each table can specify a custom row count and per-column generator overrides. See [Data Generators](generators.md) for available generators.

6. **`generation.column_rules`** — Pattern-based rules applied across all tables. Patterns use glob syntax (`*` for prefix/suffix matching). Rules are matched against column names.

## Column Configuration

Each column override supports these fields:

| Field | Description |
|-------|-------------|
| `generator` | Generator name (e.g., `internet.email`, `one_of`, `llm`) |
| `prompt` | Prompt text for the `llm` generator |
| `values` | List of allowed values for the `one_of` generator |
| `format` | Regex pattern for the `regex` generator |
| `template` | Template string with `{column_name}` placeholders |

## Column Rules

Column rules apply generators based on column name patterns across all tables:

```yaml
column_rules:
  - pattern: "*_email"      # Matches: user_email, admin_email, etc.
    generator: internet.email
  - pattern: "phone*"       # Matches: phone, phone_number, etc.
    generator: phone.national
  - pattern: "*_sku"
    generator: regex
    format: "[A-Z]{3}-[0-9]{6}"
```

Patterns support `*` as a wildcard at the start, end, or both:

- `*_email` — matches columns ending with `_email`
- `phone*` — matches columns starting with `phone`
- `*name*` — matches columns containing `name`
- `status` — exact match only
