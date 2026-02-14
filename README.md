# AutoDB

**Synthetic database for development & testing** — AutoDB creates a shadow schema inside your PostgreSQL database populated with LLM-analyzed synthetic data, so developers can work against realistic data without production PII.

## Key Features

- **LLM-driven generation** — An LLM analyzes your schema (column names, types, constraints) and picks the best data generator for each column
- **Wire protocol proxy** — A PostgreSQL proxy intercepts connections and routes queries to your real or shadow database via temporary views
- **AUTODB SQL** — Control commands using standard `CALL autodb.*()` syntax, sent through any PostgreSQL connection to generate data and toggle between real and generated data per table
- **Full CRUD against synthetic data** — The shadow schema contains real PostgreSQL tables; reads and writes work normally
- **Reproducible** — Set a seed for deterministic data generation across runs
- **Zero application changes** — Point your app at the proxy and switch between real and synthetic data per table

## Quick Start

### Prerequisites

- **Go 1.21+**
- **PostgreSQL**
- **Ollama** (recommended for local LLM) or an OpenAI API key

### Install

```bash
go install github.com/nvoxland/autodb/cmd/autodb@latest
```

### Run

```bash
# Start the proxy
autodb serve --db-database mydb --port 5433
```

Then connect through the proxy and use AUTODB SQL commands:

```bash
psql -h localhost -p 5433 -U user mydb
```

```sql
-- Generate synthetic data for the users table
CALL autodb.generate_data(table_name => 'users', rows => 500);

-- Route queries for "users" to the generated data
CALL autodb.return_generated(table_name => 'users');

-- Switch back to real data
CALL autodb.return_actual(table_name => 'users');
```

## Building from Source

```bash
git clone https://github.com/nvoxland/autodb.git
cd autodb
make build      # binary is at bin/autodb
```

Other targets:

```bash
make test       # run tests
make lint       # run golangci-lint
make clean      # remove build artifacts
```

## Documentation

Full docs are available at [nvoxland.github.io/autodb](https://nvoxland.github.io/autodb/).

To build/serve docs locally (requires [MkDocs](https://www.mkdocs.org/) and the Material theme):

```bash
## Serves at http://localhost:8000
docker run --rm -it -p 8000:8000 -v ${PWD}:/docs $(docker build -q .)
```

## License

See [LICENSE](LICENSE) for details.
