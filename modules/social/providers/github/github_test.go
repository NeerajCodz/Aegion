package github

import (
	"testing"

	"github.com/aegion/aegion/modules/social/store"
)

func TestPreset(t *testing.T) {
	p := Preset()
	if p.Slug != "github" || p.Preset != "github" || p.Protocol != store.ProtocolOAuth {
		t.Fatalf("Preset() unexpected identity fields: %#v", p)
	}
	if p.ClaimSource != store.ClaimSourceGitHubUser || p.PKCEMethod != store.PKCES256 || p.AuthStyle != store.AuthStyleClientSecretPost {
		t.Fatalf("Preset() unexpected auth settings: %#v", p)
	}
	if p.UserInfoEndpoint == "" || p.TokenEndpoint == "" || p.AuthorizeEndpoint == "" {
		t.Fatalf("Preset() expected oauth endpoints, got %#v", p)
	}
}
