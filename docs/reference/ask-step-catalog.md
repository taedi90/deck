# Ask step catalog

Generated from workflow contracts and schema docs.

## Common authoring guidance
- Prefer typed steps over `Command` when a typed step clearly matches the request.
- Keep typed arrays and objects as native YAML values.
- Keep constrained enum and pattern fields literal.

## Key step families
- `DownloadPackage`, `DownloadImage`, `DownloadFile`: prepare-time collection into bundle storage
- `InstallPackage`, `LoadImage`, `WriteFile`, `ConfigureRepository`, `ManageService`: apply-time local node changes
- `CheckHost`, `CheckCluster`, `WaitFor*`: validation and convergence checks
- `InitKubeadm`, `JoinKubeadm`, `ResetKubeadm`, `UpgradeKubeadm`: kubeadm-specific lifecycle steps

## Generated reference source
- Runtime step catalog comes from `internal/workflowcontract/steps.go`
- Field docs and examples come from `internal/schemadoc/metadata.go`
- Role support comes from built-in step definitions
