package main

import (
	"github.com/Airgap-Castaways/deck/internal/bundlecli"
	"github.com/Airgap-Castaways/deck/internal/initcli"
)

func executeInit(output string) error {
	return initcli.Run(initcli.Options{
		Output:       output,
		DeckWorkDir:  deckWorkDirName,
		StdoutPrintf: stdoutPrintf,
	})
}

func executeBundleVerify(filePath string, positionalArgs []string, output string) error {
	resolvedOutput, err := resolveOutputFormat(output)
	if err != nil {
		return err
	}
	return bundlecli.Verify(bundlecli.VerifyOptions{
		FilePath:       filePath,
		PositionalArgs: positionalArgs,
		Output:         resolvedOutput,
		Verbosef:       verbosef,
		JSONEncoder: func(v any) error {
			enc := stdoutJSONEncoder()
			enc.SetIndent("", "  ")
			return enc.Encode(v)
		},
		StdoutPrintf: stdoutPrintf,
	})
}

func executeBundleBuild(root string, out string) error {
	return bundlecli.Build(bundlecli.BuildOptions{
		Root:         root,
		Out:          out,
		Verbosef:     verbosef,
		StdoutPrintf: stdoutPrintf,
	})
}
