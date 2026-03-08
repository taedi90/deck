# deck JSON Schemas

This directory contains the raw JSON Schema files used to validate `deck` workflows.

## Files

- `deck-workflow.schema.json`: top-level workflow schema
- `deck-tooldefinition.schema.json`: tool definition manifest schema
- `tools/*.schema.json`: per-step-kind schemas

## Validation model

1. Validate the workflow file structure.
2. Validate each step against the schema for its `kind`.

Use this directory when you want the raw schema files. For a guided explanation, read `../reference/schema-reference.md`.
