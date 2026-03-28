package stepmeta_test

import (
	"strings"
	"testing"

	"github.com/Airgap-Castaways/deck/internal/stepmeta"
	_ "github.com/Airgap-Castaways/deck/internal/workflowschema"
)

func TestLookupErrorIncludesSourceLocation(t *testing.T) {
	_, ok, err := stepmeta.Lookup("WriteFile")
	if err != nil {
		t.Fatalf("Lookup(WriteFile): %v", err)
	}
	if !ok {
		t.Fatal("expected WriteFile registration")
	}

	_, _, err = stepmeta.Lookup("missing-kind")
	if err != nil {
		t.Fatalf("unexpected error for missing kind: %v", err)
	}

	// Smoke check that validation messages include source locations.
	entry, ok, err := stepmeta.Lookup("Command")
	if err != nil {
		t.Fatalf("Lookup(Command): %v", err)
	}
	if !ok {
		t.Fatal("expected Command registration")
	}
	if entry.Docs.Source.File == "" || entry.Docs.Source.Line <= 0 {
		t.Fatalf("expected docs source location, got %+v", entry.Docs.Source)
	}
	if len(entry.Docs.Fields) == 0 || entry.Docs.Fields[0].Source.File == "" {
		t.Fatalf("expected field source location, got %+v", entry.Docs.Fields)
	}
	if !strings.Contains(entry.Docs.Source.File, "internal/stepspec/") {
		t.Fatalf("unexpected source file %q", entry.Docs.Source.File)
	}
}
