package microsoft

import (
	"testing"

	"github.com/aegion/aegion/modules/social/store"
)

func TestPreset(t *testing.T) {
	p := Preset()
	if p.Slug != "microsoft" || p.Preset != "microsoft" || p.Protocol != store.ProtocolOIDC {
		t.Fatalf("Preset() unexpected identity fields: %#v", p)
	}
	if p.ClaimSource != store.ClaimSourceUserInfo || p.PKCEMethod != store.PKCES256 || p.AuthStyle != store.AuthStyleClientSecretPost {
		t.Fatalf("Preset() unexpected auth settings: %#v", p)
	}
	if p.DiscoveryURL == "" {
		t.Fatalf("Preset() expected discovery url, got %#v", p)
	}
}
