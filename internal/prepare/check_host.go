package prepare

import "github.com/Airgap-Castaways/deck/internal/hostcheck"

type checksRuntime struct {
	readHostFile  func(string) ([]byte, error)
	currentGOOS   func() string
	currentGOARCH func() string
}

func resolveCheckHostRuntime(opts RunOptions) checksRuntime {
	resolved := hostcheck.ResolveRuntime(hostcheck.Runtime{
		ReadHostFile:  opts.checksRuntime.readHostFile,
		CurrentGOOS:   opts.checksRuntime.currentGOOS,
		CurrentGOARCH: opts.checksRuntime.currentGOARCH,
	})
	return checksRuntime{
		readHostFile:  resolved.ReadHostFile,
		currentGOOS:   resolved.CurrentGOOS,
		currentGOARCH: resolved.CurrentGOARCH,
	}
}

func detectHostFactsForRuntime(rt checksRuntime) map[string]any {
	return hostcheck.DetectHostFacts(hostcheck.Runtime{
		ReadHostFile:  rt.readHostFile,
		CurrentGOOS:   rt.currentGOOS,
		CurrentGOARCH: rt.currentGOARCH,
	})
}
