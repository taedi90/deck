package askcontext

import (
	"testing"

	"github.com/Airgap-Castaways/deck/internal/askcommandspec"
)

func TestAskCommandMetaMatchesSharedSpec(t *testing.T) {
	meta := AskCommandMeta()
	spec := askcommandspec.Current()
	if meta.Short != spec.Root.Short {
		t.Fatalf("root short mismatch: %q != %q", meta.Short, spec.Root.Short)
	}
	if meta.Plan.Short != spec.Plan.Short || meta.Plan.Long != spec.Plan.Long {
		t.Fatalf("plan metadata mismatch")
	}
	if meta.Config.Short != spec.Config.Short {
		t.Fatalf("config short mismatch: %q != %q", meta.Config.Short, spec.Config.Short)
	}
	if len(meta.Flags) != len(spec.Root.Flags) {
		t.Fatalf("root flag count mismatch: %d != %d", len(meta.Flags), len(spec.Root.Flags))
	}
}
