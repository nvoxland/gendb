# Data Generators

GenDB uses generators to produce synthetic values for each column. The LLM analyzes your schema and assigns generators automatically, but you can override any assignment via [configuration](configuration.md) or [GENDB SQL](gendb-sql.md).

## Built-in Generators

### Person

| Generator | Output | Example |
|-----------|--------|---------|
| `person.first_name` | First name | `Emma` |
| `person.last_name` | Last name | `Johnson` |
| `person.full_name` | Full name | `Emma Johnson` |

### Internet

| Generator | Output | Example |
|-----------|--------|---------|
| `internet.email` | Email address | `emma.johnson@example.com` |
| `internet.url` | URL | `https://www.example.com/path` |
| `internet.image_url` | Image URL | `https://picsum.photos/seed/abcdefgh/200/200` |
| `internet.domain` | Domain name | `example.com` |

`internet.email` supports a `template` field for custom formats:

```yaml
columns:
  email:
    generator: internet.email
    template: "{first_name}.{last_name}@company.com"
```

### Phone

| Generator | Output | Example |
|-----------|--------|---------|
| `phone.national` | National phone number | `(555) 123-4567` |
| `phone.international` | Formatted international number | `+1 (555) 123-4567` |

### Address

| Generator | Output | Example |
|-----------|--------|---------|
| `address.street` | Street address | `123 Main St` |
| `address.city` | City | `Portland` |
| `address.state` | State | `Oregon` |
| `address.zip` | ZIP code | `97201` |
| `address.country` | Country | `United States` |

### Company

| Generator | Output | Example |
|-----------|--------|---------|
| `company.name` | Company name | `Acme Corp` |
| `company.bs` | Business buzzword phrase | `synergize scalable solutions` |
| `company.suffix` | Company suffix | `LLC` |

### Text

| Generator | Output | Params |
|-----------|--------|--------|
| `lorem.sentence` | Single sentence | — |
| `lorem.paragraph` | Paragraph of text | `sentences` (default: 3) |

### Time

| Generator | Output | Params |
|-----------|--------|--------|
| `time.recent` | Recent timestamp | `days` (default: 365) |
| `time.past` | Timestamp in the past 5 years | — |
| `time.future` | Timestamp in the next year | — |

### Number

| Generator | Output | Params |
|-----------|--------|--------|
| `number.int` | Integer | `min` (default: 0), `max` (default: 1000000) |
| `number.float` | Float | `min` (default: 0.0), `max` (default: 1000000.0) |
| `number.price` | Price value (1.00–999.99) | — |

### Utility

| Generator | Output | Notes |
|-----------|--------|-------|
| `uuid` | UUID v4 | `f47ac10b-58cc-4372-a567-0e02b2c3d479` |
| `boolean` | `true` / `false` | — |
| `one_of` | Random pick from a list | Requires `values` |
| `regex` | String matching a pattern | Requires `format` |
| `sequence` | Auto-incrementing integer | For serial/identity columns |
| `skip` | Skip this column | For columns with defaults or generated values |
| `llm` | LLM-generated text | Requires `prompt` or `template`; see [LLM Setup](llm.md#direct-llm-generation) |

## Column Rules

Column rules apply generators across all tables based on column name patterns:

```yaml
generation:
  column_rules:
    - pattern: "*_email"
      generator: internet.email
    - pattern: "*_sku"
      generator: regex
      format: "[A-Z]{3}-[0-9]{6}"
    - pattern: "phone*"
      generator: phone.national
```

Patterns use glob-style matching:

| Pattern | Matches |
|---------|---------|
| `*_email` | `user_email`, `admin_email` |
| `phone*` | `phone`, `phone_number` |
| `*name*` | `first_name`, `company_name` |
| `status` | `status` (exact match) |

## Type-Based Fallback

When no generator is assigned (by the LLM, config, or column rules), GenDB falls back to generating values based on the PostgreSQL column type:

| Column type | Generated value |
|-------------|----------------|
| `int`, `integer`, `bigint` | Random integer (0–100,000) |
| `serial`, `bigserial` | Skipped (database-generated) |
| `boolean` | Random `true`/`false` |
| `text`, `varchar`, `char` | Random sentence |
| `numeric`, `decimal`, `money` | Random price (1.00–999.99) |
| `float`, `double`, `real` | Random float (0–1,000) |
| `timestamp`, `date` | Random date in the past 2 years |
| `uuid` | Random UUID v4 |
| `json`, `jsonb` | `{}` |
| Other | Random word |

## Foreign Key Resolution

GenDB automatically resolves foreign key relationships:

- Tables are generated in **topological order** — referenced tables first, then dependent tables
- FK columns are populated by randomly selecting from the referenced table's already-generated primary key values
- No configuration needed; this happens automatically based on schema introspection

## UNIQUE Constraint Handling

For columns with UNIQUE constraints, GenDB retries value generation up to **100 times** per row to find a unique value. If a unique value cannot be generated after 100 attempts, generation fails with an error.

!!! tip
    If you hit uniqueness errors, try using generators with a larger value space (e.g., `uuid` instead of `number.int` with a small range), or reduce the row count for that table.
