package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/aegion/aegion/internal/platform/moduleserver"
	"github.com/aegion/aegion/modules/social/handler"
	"github.com/aegion/aegion/modules/social/service"
	"github.com/aegion/aegion/modules/social/store"
)

const (
	listenAddrEnv = "AEGION_SOCIAL_HTTP_LISTEN_ADDR"
	defaultListen = "0.0.0.0:9006"
	moduleVersion = "0.1.0"
)

var runModuleServer = moduleserver.Run

func defaultListenAddr() string {
	return moduleserver.EnvOrDefault(listenAddrEnv, defaultListen)
}

func socialServiceConfig() service.Config {
	return service.Config{
		Google: service.ProviderConfig{
			ClientID:     strings.TrimSpace(os.Getenv("AEGION_SOCIAL_GOOGLE_CLIENT_ID")),
			ClientSecret: strings.TrimSpace(os.Getenv("AEGION_SOCIAL_GOOGLE_CLIENT_SECRET")),
			RedirectURI:  strings.TrimSpace(os.Getenv("AEGION_SOCIAL_GOOGLE_REDIRECT_URI")),
		},
		GitHub: service.ProviderConfig{
			ClientID:     strings.TrimSpace(os.Getenv("AEGION_SOCIAL_GITHUB_CLIENT_ID")),
			ClientSecret: strings.TrimSpace(os.Getenv("AEGION_SOCIAL_GITHUB_CLIENT_SECRET")),
			RedirectURI:  strings.TrimSpace(os.Getenv("AEGION_SOCIAL_GITHUB_REDIRECT_URI")),
		},
	}
}

func moduleConfig(listenAddr string, registerHTTPRoutes func(mux *http.ServeMux)) moduleserver.Config {
	return moduleserver.Config{
		Module:       "social",
		Version:      moduleVersion,
		ListenAddr:   listenAddr,
		Capabilities: []string{"oauth2_social_login"},
		Routes:       []string{"/self-service/social/*", "/api/v1/social/*"},
		GRPCServices: []string{"social.SocialEngine"},
		EventSubscriptions: []string{
			"identity.created",
			"identity.updated",
		},
		RegisterHTTPRoutes: registerHTTPRoutes,
	}
}

func main() {
	listenAddr := flag.String("listen", defaultListenAddr(), "HTTP listen address")
	flag.Parse()

	socialSvc := service.New(store.New(), socialServiceConfig())
	h := handler.New(socialSvc)
	err := runModuleServer(moduleConfig(*listenAddr, h.RegisterRoutes))
	if err != nil {
		log.Fatal(err)
	}
}
