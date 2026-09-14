package project

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (p *Store) Export() (string, error) {
	if err := p.CheckUnchanged(); err != nil {
		return "", err
	}
	path := filepath.Join(p.Dir, "exports", "manuscript.md")
	if err := Atomic(path, []byte(p.Manuscript())); err != nil {
		return "", err
	}
	manifest := map[string]any{"exported_at": Now(), "passages": p.State.Passages, "source_fingerprint": p.Hashes}
	return path, WriteJSON(filepath.Join(p.Dir, "exports", "manifest.json"), manifest)
}

type SourceVersion struct {
	ID, Label string
	At        time.Time
}

func (p *Store) SourceHistory(id string) ([]SourceVersion, error) {
	if !ValidID(id) {
		return nil, fmt.Errorf("invalid source identity")
	}
	base := filepath.Join(p.Dir, ".twriter", "history")
	dirs, err := os.ReadDir(base)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var versions []SourceVersion
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(base, dir.Name()))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if !strings.HasPrefix(f.Name(), id+"-") || !f.Type().IsRegular() {
				continue
			}
			info, err := f.Info()
			if err != nil {
				return nil, err
			}
			versions = append(versions, SourceVersion{ID: dir.Name() + "/" + f.Name(), Label: f.Name(), At: info.ModTime()})
		}
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].At.After(versions[j].At) })
	return versions, nil
}
func (p *Store) ReadSourceVersion(id string) (string, error) {
	parts := strings.Split(id, "/")
	if len(parts) != 2 || !ValidID(parts[0]) || strings.ContainsAny(parts[1], `/\`) || parts[1] == "." || parts[1] == ".." || parts[1] == "" {
		return "", fmt.Errorf("invalid history identity")
	}
	path := filepath.Join(p.Dir, ".twriter", "history", parts[0], parts[1])
	for _, component := range []string{filepath.Dir(path), path} {
		info, err := os.Lstat(component)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("history cannot be a symlink")
		}
	}
	b, err := os.ReadFile(path)
	return string(b), err
}

func (p *Store) PrepareExternalEdit(id, field string) (string, error) {
	if err := p.CheckUnchanged(); err != nil {
		return "", err
	}
	path, err := p.Path(id, field)
	if err != nil {
		return "", err
	}
	if _, err = os.Stat(path); os.IsNotExist(err) {
		err = Atomic(path, nil)
	}
	return path, err
}
