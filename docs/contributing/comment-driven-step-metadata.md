# Comment-Driven Step Metadata

Public typed workflow steps in `deck` use a comment-driven source-of-truth model.

## Purpose

- Keep step shape, schema docs, generated schema annotations, and ask step context aligned.
- Avoid separate large metadata maps that drift from the actual step spec structs.
- Make documentation quality enforceable in CI instead of optional.

## What belongs where

- Put step shape in `internal/stepspec/*.go` structs.
- Put step summary, field descriptions, examples, and notes in comments next to those structs.
- Put machine-facing identity metadata in `stepmeta.MustRegister[...]` next to the same types.

Top-level schema pages such as workflow, tool-definition, and component fragment docs remain centrally declared in `internal/schemadoc/metadata.go` because they document authoring document formats rather than individual typed step kinds.

## Required comment contract for public steps

Every public step must include:

- type summary comment
- `@deck.when`
- at least one `@deck.example`
- field descriptions for public `spec.*` fields

Additionally:

- required fields need examples
- path, timeout, template, mode, url, bundle, source, and fetch-related fields need examples
- placeholder values such as `example` or `spec: {}` are not allowed

## Supported directives

- `@deck.when <text>`
- `@deck.note <text>`
- `@deck.example <inline>`
- `@deck.example` followed by a multiline block
- `@deck.required`
- `@deck.hidden`

## Review checklist for public step changes

When a PR adds or edits a public step:

- confirm the step uses `stepmeta.MustRegister[...]`
- confirm summary / when / example are present on the type
- confirm user-facing `spec` fields have descriptions and examples
- confirm generated schema docs still contain rich examples and notes
- run `make generate && git diff --exit-code`
- run `make test && make lint`

## Failure mode

Missing required comment metadata is treated as a contract failure.

- `make generate` or tests should fail
- do not add fallback placeholders to silence the failure
- fix the source comment or registration data instead
