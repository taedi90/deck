package install

import "github.com/Airgap-Castaways/deck/internal/hostcheck"

var detectHostFacts = func() map[string]any {
	return hostcheck.DetectHostFacts(hostcheck.DefaultRuntime())
}

func CurrentHostFacts() map[string]any {
	return detectHostFacts()
}
