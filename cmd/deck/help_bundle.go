package main

import "fmt"

func renderBundleHelp(args []string) (string, error) {
	if len(args) == 0 {
		return bundleHelpText(), nil
	}
	switch args[0] {
	case "verify":
		return bundleVerifyHelpText(), nil
	case "inspect":
		return bundleInspectHelpText(), nil
	case "import":
		return bundleImportHelpText(), nil
	case "collect":
		return bundleCollectHelpText(), nil
	case "merge":
		return bundleMergeHelpText(), nil
	default:
		return "", fmt.Errorf("unknown help topic %q", "bundle "+args[0])
	}
}

func renderCacheHelp(args []string) (string, error) {
	if len(args) == 0 {
		return cacheHelpText(), nil
	}
	switch args[0] {
	case "list":
		return cacheListHelpText(), nil
	case "clean":
		return cacheCleanHelpText(), nil
	default:
		return "", fmt.Errorf("unknown help topic %q", "cache "+args[0])
	}
}

func bundleHelpText() string {
	return formatHelp(
		"deck bundle <verify|inspect|import|collect|merge> [flags]",
		"Inspect or move deck bundles between directories and tar archives.",
		helpSection{Title: "Commands", Lines: []string{
			"verify       Verify bundle manifest integrity",
			"inspect      List manifest entries in a bundle",
			"import       Extract a bundle archive into a directory",
			"collect      Create a bundle archive from a directory",
			"merge        Merge a bundle archive into a destination directory",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck bundle verify ./bundle.tar",
			"deck bundle inspect ./bundle --output json",
			"deck bundle merge ./bundle.tar --to ./dest --dry-run",
		}},
	)
}

func cacheHelpText() string {
	return formatHelp(
		"deck cache <list|clean> [flags]",
		"Inspect or delete cached deck artifacts under the local deck cache root.",
		helpSection{Title: "Commands", Lines: []string{
			"list        Show cached files",
			"clean       Delete cached entries, optionally by age",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck cache list",
			"deck cache clean --older-than 30d --dry-run",
		}},
	)
}

func cacheListHelpText() string {
	return formatHelp(
		"deck cache list [--output text|json]",
		"List cached files under the default deck cache root.",
		helpSection{Title: "Flags", Lines: []string{
			"--output, -o  Output format: text or json",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck cache list",
			"deck cache list --output json",
		}},
	)
}

func cacheCleanHelpText() string {
	return formatHelp(
		"deck cache clean [--older-than <duration>] [--dry-run]",
		"Delete cached entries, optionally filtering by last modification age.",
		helpSection{Title: "Flags", Lines: []string{
			"--older-than  Delete entries older than a duration such as 30d or 24h",
			"--dry-run     Print the deletion plan without removing files",
		}},
		helpSection{Title: "Examples", Lines: []string{
			"deck cache clean --dry-run",
			"deck cache clean --older-than 30d",
		}},
	)
}

func bundleVerifyHelpText() string {
	return formatHelp(
		"deck bundle verify <path>",
		"Verify manifest integrity for a bundle directory or bundle tar archive.",
		helpSection{Title: "Flags", Lines: []string{"--file        Bundle path as an alternative to the positional argument"}},
		helpSection{Title: "Examples", Lines: []string{"deck bundle verify ./bundle.tar", "deck bundle verify --file ./bundle"}},
	)
}

func bundleInspectHelpText() string {
	return formatHelp(
		"deck bundle inspect <path> [--output text|json]",
		"List manifest entries for a bundle directory or bundle archive.",
		helpSection{Title: "Flags", Lines: []string{
			"--file        Bundle path as an alternative to the positional argument",
			"--output, -o  Output format: text or json",
		}},
		helpSection{Title: "Examples", Lines: []string{"deck bundle inspect ./bundle", "deck bundle inspect ./bundle.tar --output json"}},
	)
}

func bundleImportHelpText() string {
	return formatHelp(
		"deck bundle import --file <bundle.tar> --dest <dir>",
		"Extract a bundle tar archive into a destination directory.",
		helpSection{Title: "Flags", Lines: []string{
			"--file        Bundle archive path",
			"--dest        Destination directory",
		}},
		helpSection{Title: "Examples", Lines: []string{"deck bundle import --file ./bundle.tar --dest ./bundle"}},
	)
}

func bundleCollectHelpText() string {
	return formatHelp(
		"deck bundle collect --root <dir> --out <bundle.tar>",
		"Create a bundle tar archive from an unpacked bundle directory.",
		helpSection{Title: "Flags", Lines: []string{
			"--root        Bundle directory",
			"--out         Output archive path",
		}},
		helpSection{Title: "Examples", Lines: []string{"deck bundle collect --root ./bundle --out ./bundle.tar"}},
	)
}

func bundleMergeHelpText() string {
	return formatHelp(
		"deck bundle merge <bundle.tar> --to <dir> [--dry-run]",
		"Merge the contents of a bundle archive into a destination directory.",
		helpSection{Title: "Flags", Lines: []string{
			"--to          Merge destination directory",
			"--dry-run     Print the merge plan without writing files",
		}},
		helpSection{Title: "Examples", Lines: []string{"deck bundle merge ./bundle.tar --to ./dest", "deck bundle merge ./bundle.tar --to ./dest --dry-run"}},
	)
}
