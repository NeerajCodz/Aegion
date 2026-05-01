package google

import "github.com/aegion/aegion/modules/social/store"

func Preset() store.Provider {
	return store.Provider{
		Slug:         "google",
		DisplayName:  "Google",
		Preset:       "google",
		Protocol:     store.ProtocolOIDC,
		DiscoveryURL: "https://accounts.google.com/.well-known/openid-configuration",
		Scopes:       []string{"openid", "email", "profile"},
		ClaimMapping: store.ClaimMapping{
			Subject:       "sub",
			Email:         "email",
			EmailVerified: "email_verified",
			Name:          "name",
			Picture:       "picture",
		},
		PKCEMethod:         store.PKCES256,
		AuthStyle:          store.AuthStyleClientSecretPost,
		ClaimSource:        store.ClaimSourceUserInfo,
		Enabled:            false,
		TrustEmailVerified: true,
	}
}
