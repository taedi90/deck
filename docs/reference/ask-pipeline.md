# Ask pipeline

- Request intake normalizes the prompt and workspace root
- Classification chooses interaction mode only
- Retrieval gathers workspace context, typed evidence, and deck knowledge slices
- Requirements derivation decides offline assumptions, required files, and acceptance level
- Scaffold selection chooses a validated starter shape
- Generation fills scaffold slots rather than inventing file topology from scratch
- Validation emits structured diagnostics
- Repair consumes diagnostic JSON plus source references
