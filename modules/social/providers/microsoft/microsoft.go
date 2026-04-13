package microsoft

import "github.com/aegion/aegion/modules/social/store"

func Preset() store.Provider {
	return store.Provider{
		Slug:         "microsoft",
		DisplayName:  "Microsoft",
		Preset:       "microsoft",
		Protocol:     store.ProtocolOIDC,
		DiscoveryURL: "https://login.microsoftonline.com/common/v2.0/.well-known/openid-configuration",
		Scopes:       []string{"openid", "email", "profile"},
		ClaimMapping: store.ClaimMapping{
			Subject:       "sub",
			Email:         "email|preferred_username",
			EmailVerified: "email_verified",
			Name:          "name",
		},
		PKCEMethod:         store.PKCES256,
		AuthStyle:          store.AuthStyleClientSecretPost,
		ClaimSource:        store.ClaimSourceUserInfo,
		Enabled:            false,
		TrustEmailVerified: true,
	}
}
