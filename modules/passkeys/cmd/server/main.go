package main

import (
	"flag"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aegion/aegion/internal/platform/moduleserver"
	"github.com/aegion/aegion/internal/xlog"
	"github.com/aegion/aegion/modules/passkeys/handler"
	"github.com/aegion/aegion/modules/passkeys/service"
	"github.com/aegion/aegion/modules/passkeys/store"
)

const (
	listenAddrEnv = "AEGION_PASSKEYS_HTTP_LISTEN_ADDR"
	defaultListen = "0.0.0.0:9004"
	moduleVersion = "0.1.0"
)

var runModuleServer = moduleserver.Run

func defaultListenAddr() string {
	return moduleserver.EnvOrDefault(listenAddrEnv, defaultListen)
}

func passkeyConfig() service.Config {
	return service.Config{
		RPID:         strings.TrimSpace(os.Getenv("AEGION_PASSKEYS_RP_ID")),
		RPOrigin:     strings.TrimSpace(os.Getenv("AEGION_PASSKEYS_RP_ORIGIN")),
		ChallengeTTL: 5 * time.Minute,
	}
}

func moduleConfig(listenAddr string, registerHTTPRoutes func(mux *http.ServeMux)) moduleserver.Config {
	return moduleserver.Config{
		Module:       "passkeys",
		Version:      moduleVersion,
		ListenAddr:   listenAddr,
		Capabilities: []string{"webauthn_passwordless"},
		Routes:       []string{"/self-service/passkeys/*", "/api/v1/passkeys/*"},
		GRPCServices: []string{"passkeys.PasskeyEngine"},
		EventSubscriptions: []string{
			"session.created",
			"identity.deleted",
		},
		RegisterHTTPRoutes: registerHTTPRoutes,
	}
}

func main() {
	listenAddr := flag.String("listen", defaultListenAddr(), "HTTP listen address")
	flag.Parse()

	passkeySvc := service.New(store.New(), passkeyConfig())
	h := handler.New(passkeySvc)
	err := runModuleServer(moduleConfig(*listenAddr, h.RegisterRoutes))
	if err != nil {
		xlog.Default().Fatal("passkeys server failed", "error", err)
	}
}
