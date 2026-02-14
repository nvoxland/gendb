# LLM Setup

AutoDB uses an LLM to analyze your database schema and produce a generation plan — a mapping of each column to an appropriate data generator. This page covers how to configure different LLM providers.

## How AutoDB Uses LLMs

Generation happens in two phases:

1. **Schema analysis** — AutoDB sends your schema (table names, column names, data types, constraints) to the LLM. The LLM returns a JSON generation plan mapping each column to a [generator](generators.md) based on semantic understanding of column names and types.

2. **Data generation** — AutoDB executes the plan using fast, local generators (powered by [gofakeit](https://github.com/brianvoe/gofakeit)). The LLM is **not** called per-row — it only chooses the generators.

The exception is columns configured with `generator: llm`, which call the LLM to generate each value individually. See [Direct LLM Generation](#direct-llm-generation) below.

## Ollama (Local)

Ollama is the default provider and runs entirely on your machine.

### Setup

1. Install Ollama: [ollama.com](https://ollama.com)

2. Pull a model:

    ```bash
    ollama pull llama3.2
    ```

3. AutoDB uses Ollama by default — no configuration changes needed:

    ```yaml
    llm:
      provider: ollama
      model: llama3.2
      base_url: http://localhost:11434/v1
    ```

## OpenAI

### Setup

1. Get an API key from [platform.openai.com](https://platform.openai.com)

2. Configure in `autodb.yaml`:

    ```yaml
    llm:
      provider: openai
      model: gpt-4o-mini
      api_key: sk-...
    ```

!!! tip
    `gpt-4o-mini` is recommended for a good balance of quality and cost. Schema analysis is a single API call per `generate_data`, so costs are minimal.

## Custom / Self-Hosted

Any OpenAI-compatible API endpoint works — including vLLM, text-generation-inference, DeepSeek, and other providers.

```yaml
llm:
  provider: custom
  model: deepseek-coder
  base_url: http://localhost:8000/v1
  api_key: ""  # if required by your endpoint
```

## Direct LLM Generation

For text columns that need rich, contextual content, you can use the `llm` generator to call the LLM for each row:

```yaml
generation:
  tables:
    users:
      columns:
        bio:
          generator: llm
          prompt: "Write a short professional bio"
        review:
          generator: llm
          prompt: "Write a product review for a {category} item"
```

The prompt can reference other columns in the same row using `{column_name}` placeholders. These are substituted with already-generated values before sending to the LLM.

!!! warning
    Direct LLM generation calls the LLM once per row per column. For large tables this can be slow and costly. Use it selectively for columns that truly need natural language content.

## Cost Profile

| Operation | LLM calls | Typical cost (OpenAI) |
|-----------|-----------|----------------------|
| Schema analysis | 1 per `generate_data` | ~$0.01 |
| Data generation (default) | 0 | Free |
| `generator: llm` columns | 1 per row per column | Varies |

With Ollama, all operations are free and local.
