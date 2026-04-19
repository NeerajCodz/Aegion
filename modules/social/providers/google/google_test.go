package google

import (
	"testing"

	"github.com/aegion/aegion/modules/social/store"
)

func TestPreset(t *testing.T) {
	p := Preset()
	if p.Slug != "google" || p.Preset != "google" || p.Protocol != store.ProtocolOIDC {
		t.Fatalf("Preset() unexpected identity fields: %#v", p)
	}
	if p.ClaimSource != store.ClaimSourceUserInfo || p.PKCEMethod != store.PKCES256 || p.AuthStyle != store.AuthStyleClientSecretPost {
		t.Fatalf("Preset() unexpected auth settings: %#v", p)
	}
	if p.DiscoveryURL == "" {
		t.Fatalf("Preset() expected discovery url, got %#v", p)
	}
}
