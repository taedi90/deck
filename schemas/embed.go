package schemas

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed deck-workflow.schema.json deck-tooldefinition.schema.json tools/*.schema.json
var files embed.FS

func WorkflowSchema() ([]byte, error) {
	return files.ReadFile("deck-workflow.schema.json")
}

func ToolDefinitionSchema() ([]byte, error) {
	return files.ReadFile("deck-tooldefinition.schema.json")
}

func ToolSchema(name string) ([]byte, error) {
	clean := path.Clean(strings.TrimSpace(name))
	if clean == "." || clean == "" || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return nil, fmt.Errorf("invalid tool schema path %q", name)
	}
	return files.ReadFile(path.Join("tools", clean))
}

func ToolSchemaNames() ([]string, error) {
	entries, err := fs.ReadDir(files, "tools")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".schema.json") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}
