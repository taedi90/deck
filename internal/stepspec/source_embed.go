package stepspec

import (
	"embed"

	"github.com/Airgap-Castaways/deck/internal/stepmeta"
)

//go:embed *.go
var sourceFiles embed.FS

var _ = stepmeta.RegisterSourceFS(sourceFiles)
