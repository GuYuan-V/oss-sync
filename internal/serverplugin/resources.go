package serverplugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const resourceMarkerName = ".oss-plugin-resource.json"

type resourceMarker struct {
	PluginID   string `json:"plugin_id"`
	ResourceID string `json:"resource_id"`
	Name       string `json:"name"`
}

func (m *Manager) syncThemeResources(pluginID string, packageData Package) error {
	if err := m.removeOwnedThemeResources(pluginID); err != nil {
		return err
	}
	for _, resource := range packageData.Manifest.BlogThemes {
		if err := m.materializeThemeResource(pluginID, resource, packageData.Files, false); err != nil {
			_ = m.removeOwnedThemeResources(pluginID)
			return err
		}
	}
	for _, resource := range packageData.Manifest.ConsoleThemes {
		if err := m.materializeThemeResource(pluginID, resource, packageData.Files, true); err != nil {
			_ = m.removeOwnedThemeResources(pluginID)
			return err
		}
	}
	return nil
}

func (m *Manager) materializeThemeResource(pluginID string, resource ThemeResource, files map[string][]byte, console bool) error {
	root := filepath.Join(filepath.Dir(m.root), "themes")
	if console {
		root = filepath.Join(filepath.Dir(m.root), "console-themes")
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return fmt.Errorf("create plugin theme root: %w", err)
	}
	target := filepath.Join(root, resource.Key(pluginID))
	if info, err := os.Lstat(target); err == nil {
		existing, markerErr := readResourceMarker(target)
		if !info.IsDir() || markerErr != nil || existing.PluginID != pluginID || existing.ResourceID != resource.ID {
			return fmt.Errorf("theme resource %q conflicts with an existing resource", resource.Key(pluginID))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect existing theme resource: %w", err)
	}
	tmp, err := os.MkdirTemp(root, ".plugin-resource-")
	if err != nil {
		return fmt.Errorf("create theme resource staging directory: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = os.RemoveAll(tmp)
		}
	}()
	prefix := strings.TrimSuffix(resource.Path, "/") + "/"
	copied := 0
	for name, content := range files {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		relative := strings.TrimPrefix(name, prefix)
		if relative == "" || strings.Contains(relative, "../") || strings.HasPrefix(relative, "../") {
			return fmt.Errorf("theme resource %q contains an unsafe file path", resource.ID)
		}
		path := filepath.Join(tmp, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return fmt.Errorf("create theme resource directory: %w", err)
		}
		if err := os.WriteFile(path, content, 0o640); err != nil {
			return fmt.Errorf("write theme resource file: %w", err)
		}
		copied++
	}
	if copied == 0 {
		return fmt.Errorf("theme resource %q contains no files", resource.ID)
	}
	marker, err := json.Marshal(resourceMarker{PluginID: pluginID, ResourceID: resource.ID, Name: resource.Name})
	if err != nil {
		return fmt.Errorf("encode theme resource marker: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, resourceMarkerName), marker, 0o640); err != nil {
		return fmt.Errorf("write theme resource marker: %w", err)
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("replace theme resource: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("install theme resource: %w", err)
	}
	complete = true
	return nil
}

func (m *Manager) removeOwnedThemeResources(pluginID string) error {
	var joined error
	for _, rootName := range []string{"themes", "console-themes"} {
		root := filepath.Join(filepath.Dir(m.root), rootName)
		entries, err := os.ReadDir(root)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			joined = errors.Join(joined, err)
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			marker, err := readResourceMarker(filepath.Join(root, entry.Name()))
			if errors.Is(err, os.ErrNotExist) || marker.PluginID != pluginID {
				continue
			}
			if err != nil {
				joined = errors.Join(joined, err)
				continue
			}
			if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
				joined = errors.Join(joined, err)
			}
		}
	}
	return joined
}

func readResourceMarker(dir string) (resourceMarker, error) {
	raw, err := os.ReadFile(filepath.Join(dir, resourceMarkerName))
	if err != nil {
		return resourceMarker{}, err
	}
	var marker resourceMarker
	if err := json.Unmarshal(raw, &marker); err != nil {
		return resourceMarker{}, fmt.Errorf("decode theme resource marker: %w", err)
	}
	if marker.PluginID == "" || marker.ResourceID == "" || marker.Name == "" {
		return resourceMarker{}, errors.New("theme resource marker is incomplete")
	}
	return marker, nil
}
