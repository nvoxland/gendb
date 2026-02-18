# Data Generation

GenDB uses an LLM to generate all synthetic data values directly. The LLM receives your schema (table names, column names, data types, constraints) and produces realistic, semantically coherent data in JSON batches.

## How It Works

1. Tables are processed in **topological order** (referenced tables first)
2. For each table, the LLM receives the full schema context, column types, constraints, and any config instructions
3. Data is generated in batches of up to 50 rows per LLM call
4. Generated values are type-coerced and validated before insertion

## Config Overrides

You can provide per-column instructions in `gendb.yaml` to guide the LLM:

```yaml
generation:
  tables:
    users:
      columns:
        status:
          generator: one_of
          values: [active, inactive, pending]
        bio:
          prompt: "Write a short professional bio for a tech company employee"
        sku:
          generator: regex
          format: "[A-Z]{3}-[0-9]{6}"
```

### Override Types

| Type | Config | LLM instruction |
|------|--------|-----------------|
| `one_of` | `generator: one_of` + `values` | "must be one of: [v1, v2, v3]" |
| `regex` | `generator: regex` + `format` | "must match the regex pattern: ..." |
| `skip` | `generator: skip` | Column excluded from generation |
| `prompt` | `prompt: "..."` | Direct instruction to the LLM |

## Column Rules

Column rules apply instructions across all tables based on column name patterns:

```yaml
generation:
  column_rules:
    - pattern: "*_sku"
      generator: regex
      format: "[A-Z]{3}-[0-9]{6}"
    - pattern: "*_status"
      generator: skip
```

Patterns use glob-style matching:

| Pattern | Matches |
|---------|---------|
| `*_email` | `user_email`, `admin_email` |
| `phone*` | `phone`, `phone_number` |
| `*name*` | `first_name`, `company_name` |
| `status` | `status` (exact match) |

## Foreign Key Resolution

GenDB automatically resolves foreign key relationships:

- Tables are generated in **topological order** — referenced tables first, then dependent tables
- FK values from parent tables are included in the LLM prompt so it can pick valid references
- Post-processing validates FK values and replaces any invalid ones

## UNIQUE Constraint Handling

Unique constraints are communicated to the LLM in the prompt. The LLM is instructed to generate unique values for columns with UNIQUE indexes. A post-processing step validates uniqueness using a tracker.
