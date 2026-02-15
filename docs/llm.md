# LLM Setup

GenDB uses an LLM to analyze your database schema and produce a generation plan — a mapping of each column to an appropriate data generator. This page covers how to configure different LLM providers.

## How GenDB Uses LLMs

The LLM generates all data values directly. GenDB sends your schema (table names, column names, data types, constraints, foreign key relationships) to the LLM, which returns realistic, semantically coherent data as JSON. Data is generated in batches per table (up to 50 rows per LLM call), with larger tables chunked automatically.

## Ollama (Local)

Ollama is the default provider and runs entirely on your machine.

### Setup

1. Install Ollama: [ollama.com](https://ollama.com)

2. Pull a model:

    ```bash
    ollama pull llama3.2
    ```

3. GenDB uses Ollama by default — no configuration changes needed:

    ```yaml
    llm:
      provider: ollama
      model: llama3.2
      base_url: http://localhost:11434/v1
    ```

## OpenAI

### Setup

1. Get an API key from [platform.openai.com](https://platform.openai.com)

2. Configure in `gendb.yaml`:

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

## Column Instructions

You can provide per-column instructions in `gendb.yaml` to guide the LLM:

```yaml
generation:
  tables:
    users:
      columns:
        bio:
          prompt: "Write a short professional bio"
        status:
          generator: one_of
          values: [active, inactive, pending]
```

These instructions are included in the LLM prompt alongside the schema context.

## Cost Profile

| Operation | LLM calls | Typical cost (OpenAI) |
|-----------|-----------|----------------------|
| Data generation | 1 per 50 rows per table | ~$0.01-0.05 per table |

With Ollama, all operations are free and local.
