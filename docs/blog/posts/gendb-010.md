---
date: 2026-02-18
categories:
  - Releases
---

# GenDB 0.1.0

I pushed out the first release of GenDB today.

```bash
# Install from the GitHub release
gh release download v0.1.0 --repo nvoxland/gendb
```

<!-- more -->

## What it does

GenDB sits between your application and PostgreSQL as a wire protocol proxy -- silently acting as a test data manager.

An LLM analyzes your schema -- column names, types, foreign keys -- and picks appropriate data generators for each column.
The result is a synthetic copy of your database that looks realistic without containing any production PII.

What's in this release:

- **LLM-driven data generation** -- point it at your database and it figures out what kind of data each column needs
- **PostgreSQL proxy** -- intercepts connections and routes queries to real or generated data per table
- **GENDB SQL** -- control everything through standard `CALL gendb.*()` syntax from any PostgreSQL client
- **Full CRUD** -- the synthetic schema contains real PostgreSQL tables, so reads and writes work normally
- **Per-table routing** -- switch individual tables between real and synthetic data without restarting anything

## Where things stand

This is not production-ready code. The core flow works -- start the proxy, generate data, toggle routing -- but there are
rough edges. The LLM analysis works best with descriptive column names and well-defined foreign keys.
Tables with vague names or missing constraints will get less realistic data, and I'm sure there are bugs I haven't found yet.

I'm planning to focus on better generator customization and multi-schema support next.

If you want to try it out, check the [Getting Started](../../getting-started/index.md) guide.
Issues and feedback on [GitHub](https://github.com/nvoxland/gendb).
