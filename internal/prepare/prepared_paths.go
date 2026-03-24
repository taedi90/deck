package prepare

import (
	"fmt"
	"strings"

	"github.com/Airgap-Castaways/deck/internal/workspacepaths"
)

func ensurePreparedPathUnderRoot(kind, field, relPath, root string) (string, error) {
	trimmed := strings.TrimSpace(relPath)
	if trimmed == "" {
		return "", nil
	}
	if workspacepaths.IsPreparedPathUnderRoot(trimmed, root) {
		return trimmed, nil
	}
	return "", fmt.Errorf("%s %s must stay under %s/", kind, field, root)
}
