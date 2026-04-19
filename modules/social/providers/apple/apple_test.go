package apple

import (
	"testing"

	"github.com/aegion/aegion/modules/social/store"
)

func TestPreset(t *testing.T) {
	p := Preset()
	if p.Slug != "apple" || p.Preset != "apple" || p.Protocol != store.ProtocolOIDC {
		t.Fatalf("Preset() unexpected identity fields: %#v", p)
	}
	if p.ClaimSource != store.ClaimSourceIDToken || p.PKCEMethod != store.PKCES256 || p.AuthStyle != store.AuthStyleClientSecretPost {
		t.Fatalf("Preset() unexpected auth settings: %#v", p)
	}
	if p.ExtraAuthParams["response_mode"] != "form_post" {
		t.Fatalf("Preset() expected response_mode=form_post, got %#v", p.ExtraAuthParams)
	}
}
