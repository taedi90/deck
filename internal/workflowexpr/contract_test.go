package workflowexpr

import (
	"reflect"
	"testing"
)

func TestPublicContract(t *testing.T) {
	contract := PublicContract()
	if contract.Language != "CEL" {
		t.Fatalf("expected CEL language, got %q", contract.Language)
	}
	wantNamespaces := []string{"runtime", "vars"}
	if !reflect.DeepEqual(contract.Namespaces, wantNamespaces) {
		t.Fatalf("unexpected namespaces: got %v want %v", contract.Namespaces, wantNamespaces)
	}
}
