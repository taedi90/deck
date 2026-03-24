package install

import (
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Airgap-Castaways/deck/internal/filemode"
	"github.com/Airgap-Castaways/deck/internal/hostfs"
)

func writeFileIfChanged(path string, content []byte, mode os.FileMode) error {
	hostPath, err := hostfs.NewHostPath(path)
	if err != nil {
		return err
	}
	return hostfs.WriteFileIfChanged(hostPath, content, mode)
}

func editFileBackupEnabledValue(backup *bool) bool {
	if backup == nil {
		return true
	}
	return *backup
}

func createEditFileBackup(path string, content []byte) (string, error) {
	base := path + ".bak-" + time.Now().UTC().Format("20060102T150405Z")
	backupPath := base
	for i := 0; i < 5; i++ {
		hostPath, err := hostfs.NewHostPath(backupPath)
		if err != nil {
			return backupPath, err
		}
		if _, err := hostPath.Stat(); os.IsNotExist(err) {
			if err := filemode.WriteArtifactFile(backupPath, content); err != nil {
				return backupPath, err
			}
			return backupPath, nil
		}
		suffix, err := editFileBackupRandSuffix()
		if err != nil {
			return backupPath, err
		}
		backupPath = base + "-" + suffix
	}
	return backupPath, fmt.Errorf("unable to allocate unique backup name")
}

func trimEditFileBackups(path string, keep int) error {
	if keep < 1 {
		return nil
	}
	dir := filepath.Dir(path)
	prefix := filepath.Base(path) + ".bak-"

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	type backupFile struct {
		path    string
		modTime time.Time
	}
	backups := make([]backupFile, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		backups = append(backups, backupFile{path: filepath.Join(dir, entry.Name()), modTime: info.ModTime()})
	}
	if len(backups) <= keep {
		return nil
	}

	sort.Slice(backups, func(i, j int) bool {
		if backups[i].modTime.Equal(backups[j].modTime) {
			return backups[i].path < backups[j].path
		}
		return backups[i].modTime.Before(backups[j].modTime)
	})

	for _, backup := range backups[:len(backups)-keep] {
		if err := os.Remove(backup.path); err != nil {
			return fmt.Errorf("remove backup %s: %w", backup.path, err)
		}
	}
	return nil
}

func editFileBackupRandSuffix() (string, error) {
	b := make([]byte, 4)
	if _, err := crand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
