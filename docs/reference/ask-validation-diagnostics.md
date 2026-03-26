# Ask validation diagnostics

Structured diagnostics classify repair work by code, file, path, expected value, actual value, and source reference.

## Common categories
- `schema_invalid`: schema or YAML shape mismatch
- `component_fragment_shape`: component fragment was not a `steps:` object
- `import_shape`: phase import item was not an object with `path`
- `constrained_literal_template`: constrained field used a vars template instead of a literal
- `role_support`: step kind used in an unsupported role

## Repair model
- Repair prompts consume structured diagnostic JSON
- Diagnostics point back to workflow schema, component fragment schema, or step schema metadata
- Repair remains narrow and diagnostic-driven instead of re-sending broad prompt prose
