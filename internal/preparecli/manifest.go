package preparecli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/Airgap-Castaways/deck/internal/fsutil"
)

func buildPreparedManifest(bundleRoot fsutil.PreparedRoot) (preparedManifest, error) {
	entries := make([]preparedManifestEntry, 0)
	workspaceRoot := filepath.Dir(bundleRoot.Abs())
	for _, root := range []string{"packages", "images", "files", "bin"} {
		if _, _, err := bundleRoot.Stat(root); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return preparedManifest{}, err
		}
		if err := bundleRoot.WalkFiles(func(path string, d os.DirEntry) error {
			if d.IsDir() {
				return nil
			}
			raw, err := fsutil.ReadFile(path)
			if err != nil {
				return err
			}
			info, err := os.Stat(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(workspaceRoot, path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(raw)
			entries = append(entries, preparedManifestEntry{Path: filepath.ToSlash(rel), SHA256: hex.EncodeToString(sum[:]), Size: info.Size()})
			return nil
		}, root); err != nil {
			return preparedManifest{}, err
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return preparedManifest{Entries: entries}, nil
}

func writePreparedManifest(path string, manifest preparedManifest) error {
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return writeBytes(path, raw, 0o644)
}
