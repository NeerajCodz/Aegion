package catalog

import "testing"

func TestCatalogLoadsPresetsDynamically(t *testing.T) {
	names := Names()
	if len(names) < 6 {
		t.Fatalf("expected embedded presets, got %v", names)
	}
	for _, slug := range []string{"apple", "github", "gitlab", "google", "microsoft", "roblox"} {
		if _, err := Lookup(slug); err != nil {
			t.Fatalf("lookup %s: %v", slug, err)
		}
	}
	all := All()
	if len(all) != len(names) {
		t.Fatalf("all providers count mismatch: %d vs %d", len(all), len(names))
	}
}
