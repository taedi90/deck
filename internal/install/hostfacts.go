package install

import (
	"os"
	"runtime"

	"github.com/Airgap-Castaways/deck/internal/workflowexec"
)

func detectHostFacts() map[string]any {
	return workflowexec.DetectHostFacts(runtime.GOOS, runtime.GOARCH, os.ReadFile)
}
