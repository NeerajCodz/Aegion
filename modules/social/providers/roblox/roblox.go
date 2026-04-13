package roblox

import "github.com/aegion/aegion/modules/social/store"

func Preset() store.Provider {
	return store.Provider{
		Slug:         "roblox",
		DisplayName:  "Roblox",
		Preset:       "roblox",
		Protocol:     store.ProtocolOIDC,
		DiscoveryURL: "https://apis.roblox.com/oauth/.well-known/openid-configuration",
		Scopes:       []string{"openid", "profile", "email"},
		ClaimMapping: store.ClaimMapping{
			Subject:       "sub",
			Email:         "email",
			EmailVerified: "email_verified|verified",
			Name:          "name|preferred_username",
			Picture:       "picture",
		},
		PKCEMethod:         store.PKCES256,
		AuthStyle:          store.AuthStyleClientSecretPost,
		ClaimSource:        store.ClaimSourceUserInfo,
		Enabled:            false,
		TrustEmailVerified: true,
	}
}
