# Local setup

## Requirements

- Go 1.25.1 or later.
- AWS credentials available through the default SDK configuration.
- An S3 bucket and a Bedrock Knowledge Base with an S3 data source.
- A Neptune database that the server can reach.
- Access to a Bedrock extraction model and a published Guardrail version.

This repository does not create AWS resources.
API Gateway, Lambda hosting, authentication, source connectors, and scheduled ingestion are deployment targets.

## Configuration

Set these values for your AWS resources:

```bash
export AWS_REGION=eu-west-2
export BRAIN_BUCKET='YOUR_BUCKET'
export BRAIN_KB_ID='YOUR_KNOWLEDGE_BASE_ID'
export BRAIN_DATA_SOURCE_ID='YOUR_DATA_SOURCE_ID'
export BRAIN_GRAPH_ENDPOINT='https://YOUR_NEPTUNE_ENDPOINT:8182'
export BRAIN_GUARDRAIL_ID='YOUR_GUARDRAIL_ID'
export BRAIN_GUARDRAIL_VERSION='YOUR_PUBLISHED_VERSION'
export BRAIN_MODEL_ID='YOUR_BEDROCK_MODEL_OR_INFERENCE_PROFILE'
```

## Upload and synchronize documents

Start the admin server from the repository root:

```bash
go run ./cmd/admin \
  -bucket "$BRAIN_BUCKET" \
  -prefix sources/ \
  -kb "$BRAIN_KB_ID" \
  -data-source "$BRAIN_DATA_SOURCE_ID" \
  -model "$BRAIN_MODEL_ID" \
  -guardrail "$BRAIN_GUARDRAIL_ID" \
  -guardrail-version "$BRAIN_GUARDRAIL_VERSION" \
  -graph-endpoint "$BRAIN_GRAPH_ENDPOINT"
```

1. Open <http://127.0.0.1:8085/>.
2. Upload CSV, Markdown, or text documents.
3. Set the permitted access groups.
4. Select **Sync** to update document search and graph evidence.

Extraction uses the full source document.
It does not require a special evidence column in CSV files.
Entity types and relationship names depend on the source data.

## Start the MCP server

Run this command in another terminal:

```bash
go run ./cmd/mcp \
  -listen 127.0.0.1:8765 \
  -kb "$BRAIN_KB_ID" \
  -group YOUR_GROUP \
  -guardrail "$BRAIN_GUARDRAIL_ID" \
  -guardrail-version "$BRAIN_GUARDRAIL_VERSION" \
  -graph-endpoint "$BRAIN_GRAPH_ENDPOINT"
```

The MCP endpoint is <http://127.0.0.1:8765/mcp>.
It provides the read-only `search_company_knowledge` tool.
A remote client requires an HTTPS connection to this endpoint.

## Command reference

| Command | Function |
|---|---|
| `cmd/admin` | Upload, preview, and synchronize documents |
| `cmd/upload` | Upload source files and metadata to S3 |
| `cmd/ingest` | Extract relationships and store them in Neptune |
| `cmd/mcp` | Return evidence through MCP |
| `cmd/retrieve` | Generate answers through Bedrock |
| `cmd/graph` | Inspect graph evidence during development |

Run `go test ./...` to execute the tests.

[Return to the README](../readme.md).

## Verification

Run the focused access, Guardrails, MCP, and admin checks:

```bash
go test ./internal/ai ./internal/retrieval ./internal/mcpserver ./internal/admin
```

Run the graph extraction, identity, storage, and traversal checks:

```bash
go test ./internal/graph -run 'Test(Expand|EntityID|Extract|DecodeExtraction|StoreSnapshot)'
```

These checks do not establish production readiness or measure answer accuracy.
The Knowledge Base configuration selects the vector storage backend.
