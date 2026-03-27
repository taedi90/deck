package workflowissues

import "testing"

func TestSupportedCriticCodesAreRegistered(t *testing.T) {
	for _, code := range SupportedCriticCodes() {
		if !IsRegistered(code) {
			t.Fatalf("supported critic code %q is not registered", code)
		}
	}
}

func TestDuplicateStepIDSpecIsBlocking(t *testing.T) {
	spec := MustSpec(CodeDuplicateStepID)
	if spec.DefaultSeverity != SeverityBlocking {
		t.Fatalf("expected duplicate step id to be blocking, got %#v", spec)
	}
}

func TestAmbiguousJoinContractIsRecoverable(t *testing.T) {
	spec := MustSpec(CodeAmbiguousJoinContract)
	if !spec.DefaultRecoverable || spec.DefaultSeverity != SeverityAdvisory {
		t.Fatalf("expected ambiguous join contract to be recoverable advisory, got %#v", spec)
	}
}
