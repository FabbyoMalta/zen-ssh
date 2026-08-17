package sshcfg

import (
	"errors"
	"os"
	"path/filepath"

	"zenssh/internal/config"
)

type fileSnapshot struct {
	path   string
	data   []byte
	mode   os.FileMode
	exists bool
}

func SaveAll(store *config.Store, hosts []config.Host) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	paths := []string{store.HostsFile(), store.ManagedSSHConfigPath(), filepath.Join(home, ".ssh", "config")}
	snapshots := make([]fileSnapshot, 0, len(paths))
	for _, path := range paths {
		snapshot, err := takeSnapshot(path)
		if err != nil {
			return err
		}
		snapshots = append(snapshots, snapshot)
	}
	if err := store.SaveHosts(hosts); err != nil {
		return err
	}
	if err := WriteManagedConfig(store.ManagedSSHConfigPath(), hosts); err != nil {
		return errors.Join(err, restoreSnapshots(snapshots))
	}
	return nil
}

func takeSnapshot(path string) (fileSnapshot, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return fileSnapshot{path: path}, nil
	}
	if err != nil {
		return fileSnapshot{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fileSnapshot{}, err
	}
	return fileSnapshot{path: path, data: data, mode: info.Mode().Perm(), exists: true}, nil
}

func restoreSnapshots(snapshots []fileSnapshot) error {
	var result error
	for _, snapshot := range snapshots {
		if snapshot.exists {
			if err := os.MkdirAll(filepath.Dir(snapshot.path), 0o700); err != nil {
				result = errors.Join(result, err)
				continue
			}
			result = errors.Join(result, atomicWriteFile(snapshot.path, snapshot.data, snapshot.mode))
		} else if err := os.Remove(snapshot.path); err != nil && !os.IsNotExist(err) {
			result = errors.Join(result, err)
		}
	}
	return result
}

func RestoreMainConfigBackup() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	main := filepath.Join(home, ".ssh", "config")
	backup := main + ".zenssh.bak"
	data, err := os.ReadFile(backup)
	if err != nil {
		return err
	}
	return atomicWriteFile(main, data, 0o600)
}
