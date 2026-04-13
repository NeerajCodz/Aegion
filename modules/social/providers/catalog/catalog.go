package catalog

import (
	"fmt"
	"strings"

	"github.com/aegion/aegion/modules/social/providers/apple"
	"github.com/aegion/aegion/modules/social/providers/github"
	"github.com/aegion/aegion/modules/social/providers/gitlab"
	"github.com/aegion/aegion/modules/social/providers/google"
	"github.com/aegion/aegion/modules/social/providers/microsoft"
	"github.com/aegion/aegion/modules/social/providers/roblox"
	"github.com/aegion/aegion/modules/social/store"
)

func Lookup(name string) (store.Provider, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "google":
		return google.Preset(), nil
	case "github":
		return github.Preset(), nil
	case "apple":
		return apple.Preset(), nil
	case "microsoft":
		return microsoft.Preset(), nil
	case "gitlab":
		return gitlab.Preset(), nil
	case "roblox":
		return roblox.Preset(), nil
	default:
		return store.Provider{}, fmt.Errorf("unknown provider preset %q", name)
	}
}

func All() []store.Provider {
	return []store.Provider{
		google.Preset(),
		github.Preset(),
		apple.Preset(),
		microsoft.Preset(),
		gitlab.Preset(),
		roblox.Preset(),
	}
}
