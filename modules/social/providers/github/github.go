package github

import "github.com/aegion/aegion/modules/social/store"

func Preset() store.Provider {
	return store.Provider{
		Slug:              "github",
		DisplayName:       "GitHub",
		Preset:            "github",
		Protocol:          store.ProtocolOAuth,
		AuthorizeEndpoint: "https://github.com/login/oauth/authorize",
		TokenEndpoint:     "https://github.com/login/oauth/access_token",
		UserInfoEndpoint:  "https://api.github.com/user",
		Scopes:            []string{"read:user", "user:email"},
		ClaimMapping: store.ClaimMapping{
			Subject: "id",
			Email:   "email",
			Name:    "name|login",
			Picture: "avatar_url",
		},
		PKCEMethod:         store.PKCES256,
		AuthStyle:          store.AuthStyleClientSecretPost,
		ClaimSource:        store.ClaimSourceGitHubUser,
		Enabled:            false,
		TrustEmailVerified: true,
	}
}
