# Configuration

GenDB is configured via `gendb.yaml`. Pass a custom path with `--config`.

## Precedence

Settings are applied in this order (highest priority first):

1. **`gendb.yaml`** — file-based column instructions
2. **LLM generation** — the LLM generates data based on schema context and instructions

## Full Reference

```yaml
llm:
  provider: ollama        # ollama | openai | custom
  model: llama3.2         # Model name
  base_url: http://localhost:11434/v1  # LLM API endpoint
  api_key: ""             # API key (required for openai/custom)

generation:
  default_rows: 100       # Rows per table unless overridden

  tables:                 # Per-table overrides
    users:
      rows: 500
      columns:
        bio:
          prompt: "Write a short professional bio"
        role:
          generator: one_of
          values: ["admin", "user", "moderator"]
    orders:
      rows: 2000

  column_rules:           # Pattern-based rules
    - pattern: "*_sku"
      generator: regex
      format: "[A-Z]{3}-[0-9]{6}"
```

### Notes

1. **`llm.provider`** — LLM provider for data generation:
    - `ollama` (default) — local Ollama instance
    - `openai` — OpenAI API
    - `custom` — any OpenAI-compatible endpoint

2. **`generation.default_rows`** — Default number of rows to generate per table. Can be overridden per-table in the `tables` section.

3. **`generation.tables`** — Per-table configuration. Each table can specify a custom row count and per-column instructions for the LLM.

4. **`generation.column_rules`** — Pattern-based rules applied across all tables. Patterns use glob syntax (`*` for prefix/suffix matching). Rules are matched against column names.

## Column Configuration

Each column override supports these fields:

| Field | Description |
|-------|-------------|
| `generator` | Override type: `one_of`, `regex`, or `skip` |
| `prompt` | Direct instruction to the LLM for this column |
| `values` | List of allowed values for `one_of` |
| `format` | Regex pattern for `regex` |

## Column Rules

Column rules apply instructions based on column name patterns across all tables:

```yaml
column_rules:
  - pattern: "*_sku"
    generator: regex
    format: "[A-Z]{3}-[0-9]{6}"
  - pattern: "*_status"
    generator: skip
```

Patterns support `*` as a wildcard at the start, end, or both:

- `*_email` — matches columns ending with `_email`
- `phone*` — matches columns starting with `phone`
- `*name*` — matches columns containing `name`
- `status` — exact match only
