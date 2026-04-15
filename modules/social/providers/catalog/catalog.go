package catalog

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/aegion/aegion/modules/social/store"
)

//go:embed presets/*.json
var presetFS embed.FS

var (
	loadOnce     sync.Once
	loadErr      error
	presetBySlug map[string]store.Provider
	presetOrder  []string
)

func Lookup(name string) (store.Provider, error) {
	if err := ensureLoaded(); err != nil {
		return store.Provider{}, err
	}
	slug := strings.ToLower(strings.TrimSpace(name))
	provider, ok := presetBySlug[slug]
	if !ok {
		return store.Provider{}, fmt.Errorf("unknown provider preset %q", name)
	}
	return provider, nil
}

func All() []store.Provider {
	if err := ensureLoaded(); err != nil {
		return nil
	}
	out := make([]store.Provider, 0, len(presetOrder))
	for _, slug := range presetOrder {
		out = append(out, presetBySlug[slug])
	}
	return out
}

func Names() []string {
	if err := ensureLoaded(); err != nil {
		return nil
	}
	return append([]string(nil), presetOrder...)
}

func ensureLoaded() error {
	loadOnce.Do(func() {
		presetBySlug = make(map[string]store.Provider)
		entries, err := fs.ReadDir(presetFS, "presets")
		if err != nil {
			loadErr = err
			return
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.EqualFold(path.Ext(entry.Name()), ".json") {
				continue
			}
			raw, err := presetFS.ReadFile(path.Join("presets", entry.Name()))
			if err != nil {
				loadErr = err
				return
			}
			var provider store.Provider
			if err := json.Unmarshal(raw, &provider); err != nil {
				loadErr = fmt.Errorf("decode %s: %w", entry.Name(), err)
				return
			}
			provider.Slug = strings.ToLower(strings.TrimSpace(provider.Slug))
			provider.Preset = strings.ToLower(strings.TrimSpace(provider.Preset))
			if provider.Slug == "" {
				loadErr = fmt.Errorf("preset %s is missing slug", entry.Name())
				return
			}
			if provider.Preset == "" {
				provider.Preset = provider.Slug
			}
			presetBySlug[provider.Slug] = provider
			presetOrder = append(presetOrder, provider.Slug)
		}
		slices.Sort(presetOrder)
	})
	return loadErr
}
