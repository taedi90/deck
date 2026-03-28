package stepspec

import "sync"

type FieldDoc struct {
	Description string
	Example     string
}

type ToolDocMetadata struct {
	Example   string
	FieldDocs map[string]FieldDoc
	Notes     []string
}

var (
	toolDocMu   sync.RWMutex
	toolDocMeta = map[string]ToolDocMetadata{}
)

func registerToolDoc(kind string, meta ToolDocMetadata) struct{} {
	toolDocMu.Lock()
	defer toolDocMu.Unlock()
	toolDocMeta[kind] = cloneToolDocMetadata(meta)
	return struct{}{}
}

func LookupToolDoc(kind string) (ToolDocMetadata, bool) {
	toolDocMu.RLock()
	meta, ok := toolDocMeta[kind]
	toolDocMu.RUnlock()
	if !ok {
		return ToolDocMetadata{}, false
	}
	return cloneToolDocMetadata(meta), true
}

func cloneToolDocMetadata(meta ToolDocMetadata) ToolDocMetadata {
	cloned := ToolDocMetadata{Example: meta.Example, Notes: append([]string(nil), meta.Notes...)}
	if meta.FieldDocs != nil {
		cloned.FieldDocs = make(map[string]FieldDoc, len(meta.FieldDocs))
		for key, value := range meta.FieldDocs {
			cloned.FieldDocs[key] = value
		}
	}
	return cloned
}
