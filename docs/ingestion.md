# S3 relationship ingestion

Run commands from the repository root. Configure your AWS profile before running them.

The ingestion command reads a Markdown or text object and its Bedrock metadata file directly from S3.
It checks the document with Guardrails and extracts claims through Converse.
It validates JSON, entity types, predicates, names, and exact supporting quotes.
It checks generated output with Guardrails before writing to Neptune.
A matching quote does not prove correct interpretation.

## Required metadata

The adjacent object must use the document key plus `.metadata.json`.
This example uses the simplified Bedrock format:

```json
{
  "metadataAttributes": {
    "document_id": "product-sso-001",
    "version": "1",
    "status": "approved",
    "owner": "sales",
    "unrestricted": false,
    "allowed_groups": ["sales"]
  }
}
```

Use the simplified metadata format shown above. Typed attribute objects are not supported.
Set `unrestricted` to `true` only when `allowed_groups` is explicitly empty.
Missing or conflicting access fields stop ingestion.
Upload content and metadata before starting ingestion.

## Extract without graph writes

This operation still calls S3, Bedrock, and Guardrails. AWS charges can apply.

```bash
AWS_REGION=eu-west-2 go run ./cmd/ingest \
  -bucket YOUR_BUCKET \
  -key sources/sales/sso.md \
  -model YOUR_MODEL_OR_INFERENCE_PROFILE_ID \
  -guardrail YOUR_GUARDRAIL_ID \
  -guardrail-version 1 \
  -dry-run
```

## Extract and write

```bash
AWS_REGION=eu-west-2 go run ./cmd/ingest \
  -bucket YOUR_BUCKET \
  -key sources/sales/sso.md \
  -model YOUR_MODEL_OR_INFERENCE_PROFILE_ID \
  -guardrail YOUR_GUARDRAIL_ID \
  -guardrail-version 1 \
  -endpoint https://YOUR_INSTANCE_ENDPOINT:8182
```

The output reports the source URI, snapshot ID, and validated claim count.
The ingestion role needs source `s3:GetObject`, model `bedrock:InvokeModel`, and policy `bedrock:ApplyGuardrail` permissions.
It also needs `neptune-db:WriteDataViaQuery` for the target cluster resource.
Scope permissions to the selected resources.
Inference profiles can require permissions for destination model resources.

## Inspect active claims

The graph command is an administrator tool. It does not apply end-user document permissions.
Do not expose it through a user API or MCP tool.

```bash
AWS_REGION=eu-west-2 go run ./cmd/graph \
  -endpoint https://YOUR_INSTANCE_ENDPOINT:8182 \
  -source s3://YOUR_BUCKET/sources/sales/sso.md
```

## Snapshot behaviour

One parameterised query stores a document snapshot and its claims.
The query sets the document's active snapshot within the same transaction.
A failed query does not activate a partial snapshot.
Stable inputs and extraction results produce stable identifiers.
Model output can vary between runs and produce different snapshots.
An empty extraction activates a snapshot without claims.
Old snapshots remain for inspection.
Future graph retrieval must select only the active snapshot and enforce its permissions.
Entity names provide basic matching, rather than full identity resolution.

## Current limits

Ingestion processes one S3 document per command.
It supports UTF-8 Markdown and text, up to 1 MiB.
Metadata is limited to 10 KiB. Extraction is limited to 50 claims.
Model responses must contain one JSON object. A single enclosing JSON or plain Markdown code fence is accepted.
The command rejects truncated generation and invalid claims before graph writes.

S3 content and metadata use separate reads, rather than an atomic source snapshot.
Do not change either object during ingestion.
Concurrent ingestion of one source is unsupported; the last successful write becomes active.
Source deletion does not yet deactivate its graph snapshot automatically.
Graph evidence is included when `cmd/retrieve` receives `-graph-endpoint`.
The local commands use the developer's AWS identity.
Automated access revocation, multi-document batches, and graph retrieval remain future work.

## Local checks

```bash
go test ./...
go vet ./...
```

Tests cover strict extraction validation, explicit source permissions, size limits, snapshot identifiers, and blocked model operations.
Local tests do not establish live AWS connectivity or openCypher compatibility.

## Existing commands

`cmd/ingest` replaces the earlier local document-loading exercise.
`cmd/upload` and `cmd/retrieve` retain their existing purposes.

## Answer with graph evidence

Add `-graph-endpoint https://YOUR_INSTANCE_ENDPOINT:8182` to the existing retrieval command.
Bedrock first selects permitted document chunks.
The application looks up claims for those source URIs and matching metadata versions.
Neptune lookup selects only active snapshots.
The application checks snapshot permissions before sending claims to the model.
Missing or malformed permission fields deny graph access.
The answer output includes `document_chunks` and `graph_evidence` for inspection.
Graph lookup failures stop answer generation when graph retrieval is enabled.
Omit `-graph-endpoint` to retain document-only retrieval.

This implementation enriches retrieved sources; it does not discover unrelated sources through multi-hop traversal.
Only claims from returned source documents enter the answer context.
Source permissions remain synced copies and can become outdated.
The group flag simulates identity and must not be exposed as authentication.
