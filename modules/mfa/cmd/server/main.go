package main

import (
	"flag"

	"github.com/aegion/aegion/internal/platform/moduleserver"
	"github.com/aegion/aegion/internal/xlog"
)

const (
	listenAddrEnv = "AEGION_MFA_HTTP_LISTEN_ADDR"
	defaultListen = "0.0.0.0:9003"
	moduleVersion = "0.1.0"
)

var runModuleServer = moduleserver.Run
var logFatal = func(v ...any) {
	if len(v) == 0 {
		xlog.Default().Fatal("mfa server failed")
		return
	}
	if err, ok := v[0].(error); ok {
		xlog.Default().Fatal(err.Error(), "error", err)
		return
	}
	xlog.Default().Fatal("mfa server failed", v...)
}

func defaultListenAddr() string {
	return moduleserver.EnvOrDefault(listenAddrEnv, defaultListen)
}

func moduleConfig(listenAddr string) moduleserver.Config {
	return moduleserver.Config{
		Module:       "mfa",
		Version:      moduleVersion,
		ListenAddr:   listenAddr,
		Capabilities: []string{"totp", "webauthn", "sms", "backup_codes"},
		Routes:       []string{"/self-service/mfa/*", "/api/v1/mfa/*"},
		EventSubscriptions: []string{
			"session.created",
			"identity.updated",
			"identity.deleted",
		},
	}
}

func main() {
	listenAddr := flag.String("listen", defaultListenAddr(), "HTTP listen address")
	flag.Parse()

	err := runModuleServer(moduleConfig(*listenAddr))
	if err != nil {
		logFatal(err)
	}
}
