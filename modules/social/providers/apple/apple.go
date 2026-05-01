package apple

import "github.com/aegion/aegion/modules/social/store"

func Preset() store.Provider {
	return store.Provider{
		Slug:         "apple",
		DisplayName:  "Apple",
		Preset:       "apple",
		Protocol:     store.ProtocolOIDC,
		Issuer:       "https://appleid.apple.com",
		DiscoveryURL: "https://appleid.apple.com/.well-known/openid-configuration",
		Scopes:       []string{"openid", "email", "name"},
		ClaimMapping: store.ClaimMapping{
			Subject:       "sub",
			Email:         "email",
			EmailVerified: "email_verified",
			Name:          "name",
		},
		PKCEMethod:         store.PKCES256,
		AuthStyle:          store.AuthStyleClientSecretPost,
		ClaimSource:        store.ClaimSourceIDToken,
		Enabled:            false,
		TrustEmailVerified: true,
		ExtraAuthParams: map[string]string{
			"response_mode": "form_post",
		},
	}
}
