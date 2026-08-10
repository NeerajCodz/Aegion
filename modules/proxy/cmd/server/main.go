package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	coreproxy "github.com/aegion/aegion/core/proxy"
	coresession "github.com/aegion/aegion/core/session"
	"github.com/aegion/aegion/internal/platform/egress"
	"github.com/aegion/aegion/internal/platform/moduleserver"
	"github.com/aegion/aegion/internal/xlog"
	"github.com/aegion/aegion/modules/proxy/handler"
	"github.com/aegion/aegion/modules/proxy/service"
	"github.com/aegion/aegion/modules/proxy/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	listenAddrEnv                    = "AEGION_PROXY_HTTP_LISTEN_ADDR"
	defaultListen                    = "0.0.0.0:9009"
	moduleVersion                    = "0.2.0"
	dbURLEnv                         = "AEGION_PROXY_DATABASE_URL"
	managementTokenEnv               = "AEGION_PROXY_MANAGEMENT_TOKEN"
	dataPlaneListenAddrEnv           = "AEGION_PROXY_DATA_PLANE_LISTEN_ADDR"
	egressAllowedHostsEnv            = "AEGION_PROXY_EGRESS_ALLOWED_HOSTS"
	egressAllowedCIDRsEnv            = "AEGION_PROXY_EGRESS_ALLOWED_CIDRS"
	egressTrustedCIDRsEnv            = "AEGION_PROXY_EGRESS_TRUSTED_CIDRS"
	sessionContextSecretEnv          = "AEGION_PROXY_SESSION_CONTEXT_SECRET"
	upstreamIdentitySigningSecretEnv = "AEGION_PROXY_UPSTREAM_IDENTITY_SIGNING_SECRET"
	configReloadIntervalEnv          = "AEGION_PROXY_CONFIG_RELOAD_INTERVAL"

	minimumSecretLength   = 32
	defaultReloadInterval = 5 * time.Second
)

var (
	runModuleServer       = moduleserver.Run
	buildRuntimeHook      = buildRuntime
	newPoolWithConfigHook = pgxpool.NewWithConfig
	poolPingHook          = func(ctx context.Context, pool *pgxpool.Pool) error { return pool.Ping(ctx) }
	poolCloseHook         = func(pool *pgxpool.Pool) { pool.Close() }
	newPostgresRepoHook   = store.NewPostgres
	logFatal              = func(v ...any) {
		if len(v) == 0 {
			xlog.Default().Fatal("proxy server failed")
			return
		}
		if err, ok := v[0].(error); ok {
			xlog.Default().Fatal(err.Error(), "error", err)
			return
		}
		xlog.Default().Fatal(v[0].(string), v[1:]...)
	}
)

var protectedDataPlanePaths = []string{
	"/internal",
	"/internal/registry",
	"/admin",
	"/api/v1/admin",
	"/auth",
	"/api/v1/auth",
	"/oauth2",
	"/.well-known",
	"/proxy",
	"/api/v1/proxy",
}

type moduleRuntime struct {
	registerHTTPRoutes func(mux *http.ServeMux)
	capabilities       []string
	readiness          func(context.Context) error
	start              func() error
	cleanup            func()
}

type validatingRepository struct {
	store.Repository
	egressClient *egress.Client
}

type dataPlaneSnapshot struct {
	proxy *coreproxy.Proxy
}

type dataPlane struct {
	service                       *service.Service
	egressClient                  *egress.Client
	sessionContextSecret          []byte
	upstreamIdentitySigningSecret string
	listenAddr                    string
	reloadInterval                time.Duration

	snapshot atomic.Pointer[dataPlaneSnapshot]
	mu       sync.RWMutex
	lastErr  error
	started  bool
	server   *http.Server
	watchCtx context.Context
	cancel   context.CancelFunc
	stopOnce sync.Once
}

func defaultListenAddr() string {
	return moduleserver.EnvOrDefault(listenAddrEnv, defaultListen)
}

// moduleConfig declares only the proxy's core-discoverable surface. Management
// endpoints remain on the module's authenticated internal listener and the
// optional data plane is never claimed as a core public route.
func moduleConfig(listenAddr string, registerHTTPRoutes func(mux *http.ServeMux)) moduleserver.Config {
	return moduleserver.Config{
		Module:             "proxy",
		Version:            moduleVersion,
		ListenAddr:         listenAddr,
		Capabilities:       []string{"proxy_rule_registry"},
		RegisterHTTPRoutes: registerHTTPRoutes,
	}
}

func main() {
	listenAddr := flag.String("listen", defaultListenAddr(), "internal management HTTP listen address")
	flag.Parse()

	runtime, err := buildRuntimeHook(context.Background())
	if err != nil {
		logFatal(err)
	}
	if runtime.cleanup != nil {
		defer runtime.cleanup()
	}
	if runtime.start != nil {
		if err := runtime.start(); err != nil {
			logFatal(err)
		}
	}

	cfg := moduleConfig(*listenAddr, runtime.registerHTTPRoutes)
	cfg.Capabilities = append([]string(nil), runtime.capabilities...)
	cfg.Readiness = runtime.readiness
	if err := runModuleServer(cfg); err != nil {
		logFatal(err)
	}
}

func buildRuntime(ctx context.Context) (*moduleRuntime, error) {
	managementToken, err := requiredSecret(managementTokenEnv)
	if err != nil {
		return nil, err
	}
	egressClient, err := buildEgressClient()
	if err != nil {
		return nil, err
	}

	repo, repositoryReadiness, cleanup, err := buildRepositoryWithReadiness(ctx)
	if err != nil {
		return nil, err
	}
	cleanupOnError := func(err error) (*moduleRuntime, error) {
		cleanup()
		return nil, err
	}
	if err := repositoryReadiness(ctx); err != nil {
		return cleanupOnError(err)
	}

	proxySvc := service.New(&validatingRepository{Repository: repo, egressClient: egressClient})
	if err := validateStoredConfiguration(ctx, proxySvc, egressClient, ""); err != nil {
		return cleanupOnError(err)
	}

	runtime := &moduleRuntime{
		registerHTTPRoutes: handler.New(proxySvc, handler.Config{ManagementToken: managementToken}).RegisterRoutes,
		capabilities:       []string{"proxy_rule_registry"},
		readiness: func(ctx context.Context) error {
			if err := repositoryReadiness(ctx); err != nil {
				return err
			}
			return validateStoredConfiguration(ctx, proxySvc, egressClient, "")
		},
		cleanup: cleanup,
	}

	dataPlaneAddr := strings.TrimSpace(os.Getenv(dataPlaneListenAddrEnv))
	if dataPlaneAddr == "" {
		return runtime, nil
	}

	sessionContextSecret, err := requiredSecret(sessionContextSecretEnv)
	if err != nil {
		return cleanupOnError(err)
	}
	upstreamIdentitySigningSecret, err := requiredSecret(upstreamIdentitySigningSecretEnv)
	if err != nil {
		return cleanupOnError(err)
	}
	reloadInterval, err := configuredReloadInterval()
	if err != nil {
		return cleanupOnError(err)
	}

	plane, err := newDataPlane(ctx, proxySvc, egressClient, dataPlaneAddr, []byte(sessionContextSecret), upstreamIdentitySigningSecret, reloadInterval)
	if err != nil {
		return cleanupOnError(err)
	}
	previousCleanup := runtime.cleanup
	runtime.start = plane.Start
	runtime.capabilities = append(runtime.capabilities, "authz_proxy")
	runtime.readiness = func(ctx context.Context) error {
		if err := repositoryReadiness(ctx); err != nil {
			return err
		}
		return plane.Readiness(ctx)
	}
	runtime.cleanup = func() {
		plane.Close()
		previousCleanup()
	}
	return runtime, nil
}

func buildRepository(ctx context.Context) (store.Repository, func(), error) {
	repo, _, cleanup, err := buildRepositoryWithReadiness(ctx)
	return repo, cleanup, err
}

func buildRepositoryWithReadiness(ctx context.Context) (store.Repository, func(context.Context) error, func(), error) {
	dbURL := strings.TrimSpace(os.Getenv(dbURLEnv))
	if dbURL == "" {
		return nil, nil, nil, fmt.Errorf("%s is required", dbURLEnv)
	}

	poolCfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse proxy database url: %w", err)
	}
	poolCfg.MaxConns = 10
	poolCfg.MinConns = 1

	pool, err := newPoolWithConfigHook(ctx, poolCfg)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("connect proxy database: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := poolPingHook(pingCtx, pool); err != nil {
		poolCloseHook(pool)
		return nil, nil, nil, fmt.Errorf("ping proxy database: %w", err)
	}

	repo, err := newPostgresRepoHook(pool)
	if err != nil {
		poolCloseHook(pool)
		return nil, nil, nil, fmt.Errorf("create proxy repository: %w", err)
	}
	return repo, func(readinessCtx context.Context) error {
		return checkProxySchema(readinessCtx, pool)
	}, func() { poolCloseHook(pool) }, nil
}

func checkProxySchema(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("proxy database pool is unavailable")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := poolPingHook(checkCtx, pool); err != nil {
		return fmt.Errorf("ping proxy database: %w", err)
	}

	const query = `
		SELECT
			to_regclass('proxy_upstreams') IS NOT NULL
			AND to_regclass('proxy_routes') IS NOT NULL
			AND EXISTS (
				SELECT 1
				FROM information_schema.columns
				WHERE table_schema = current_schema()
					AND table_name = 'proxy_upstreams'
					AND column_name = 'health_check_expected_body'
			)`
	var ready bool
	if err := pool.QueryRow(checkCtx, query).Scan(&ready); err != nil {
		return fmt.Errorf("check proxy schema: %w", err)
	}
	if !ready {
		return errors.New("proxy schema is not at the required migration level")
	}
	return nil
}

func buildEgressClient() (*egress.Client, error) {
	client, err := egress.NewClient(egress.Policy{
		AllowedHosts: splitCSVEnv(egressAllowedHostsEnv),
		AllowedCIDRs: splitCSVEnv(egressAllowedCIDRsEnv),
		TrustedCIDRs: splitCSVEnv(egressTrustedCIDRsEnv),
		Timeout:      10 * time.Second,
		MaxBodyBytes: 1 << 20,
	})
	if err != nil {
		return nil, fmt.Errorf("configure proxy egress policy: %w", err)
	}
	return client, nil
}

func splitCSVEnv(name string) []string {
	raw := strings.Split(os.Getenv(name), ",")
	values := make([]string, 0, len(raw))
	for _, value := range raw {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func requiredSecret(name string) (string, error) {
	secret := strings.TrimSpace(os.Getenv(name))
	if len(secret) < minimumSecretLength {
		return "", fmt.Errorf("%s must be at least %d bytes", name, minimumSecretLength)
	}
	return secret, nil
}

func configuredReloadInterval() (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(configReloadIntervalEnv))
	if raw == "" {
		return defaultReloadInterval, nil
	}
	interval, err := time.ParseDuration(raw)
	if err != nil || interval < time.Second {
		return 0, fmt.Errorf("%s must be a duration of at least one second", configReloadIntervalEnv)
	}
	return interval, nil
}

func (r *validatingRepository) UpsertUpstream(ctx context.Context, upstream store.Upstream) (*store.Upstream, error) {
	if r == nil || r.egressClient == nil {
		return nil, errors.New("proxy egress policy is unavailable")
	}
	if _, err := r.egressClient.ValidateURL(ctx, upstream.URL); err != nil {
		return nil, fmt.Errorf("upstream destination is not allowed: %w", err)
	}
	return r.Repository.UpsertUpstream(ctx, upstream)
}

func (r *validatingRepository) UpsertRoute(ctx context.Context, route store.Route) (*store.Route, error) {
	if route.Enabled && isProtectedDataPlanePath(route.Path) {
		return nil, service.ErrInvalidProxyConfig
	}
	return r.Repository.UpsertRoute(ctx, route)
}

func newDataPlane(ctx context.Context, proxySvc *service.Service, egressClient *egress.Client, listenAddr string, sessionContextSecret []byte, upstreamIdentitySigningSecret string, reloadInterval time.Duration) (*dataPlane, error) {
	if strings.TrimSpace(listenAddr) == "" {
		return nil, errors.New("proxy data-plane listen address is required")
	}
	if len(sessionContextSecret) < minimumSecretLength {
		return nil, errors.New("proxy session context secret is too short")
	}
	if len(strings.TrimSpace(upstreamIdentitySigningSecret)) < minimumSecretLength {
		return nil, errors.New("proxy upstream identity signing secret is too short")
	}
	if reloadInterval < time.Second {
		return nil, errors.New("proxy configuration reload interval must be at least one second")
	}

	watchCtx, cancel := context.WithCancel(context.Background())
	plane := &dataPlane{
		service:                       proxySvc,
		egressClient:                  egressClient,
		sessionContextSecret:          append([]byte(nil), sessionContextSecret...),
		upstreamIdentitySigningSecret: strings.TrimSpace(upstreamIdentitySigningSecret),
		listenAddr:                    listenAddr,
		reloadInterval:                reloadInterval,
		watchCtx:                      watchCtx,
		cancel:                        cancel,
	}
	if err := plane.reload(ctx); err != nil {
		cancel()
		return nil, err
	}
	plane.server = &http.Server{
		Addr:              listenAddr,
		Handler:           plane,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	return plane, nil
}

func (d *dataPlane) Start() error {
	if d == nil || d.server == nil {
		return errors.New("proxy data plane is unavailable")
	}
	d.mu.Lock()
	if d.started {
		d.mu.Unlock()
		return errors.New("proxy data plane has already started")
	}
	d.started = true
	d.mu.Unlock()

	listener, err := net.Listen("tcp", d.listenAddr)
	if err != nil {
		d.mu.Lock()
		d.started = false
		d.mu.Unlock()
		return fmt.Errorf("listen on proxy data plane: %w", err)
	}
	go func() {
		if err := d.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			d.setLastError(fmt.Errorf("serve proxy data plane: %w", err))
		}
	}()
	go d.watchConfiguration()
	return nil
}

func (d *dataPlane) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if d == nil {
		http.Error(w, "proxy unavailable", http.StatusServiceUnavailable)
		return
	}
	snapshot := d.snapshot.Load()
	if snapshot == nil || snapshot.proxy == nil {
		http.Error(w, "proxy unavailable", http.StatusServiceUnavailable)
		return
	}

	trustedRequest, err := d.withTrustedSession(r)
	if err != nil {
		http.Error(w, "invalid session context", http.StatusUnauthorized)
		return
	}
	snapshot.proxy.ServeHTTP(w, trustedRequest)
}

func (d *dataPlane) withTrustedSession(r *http.Request) (*http.Request, error) {
	if r == nil {
		return nil, errors.New("request is required")
	}
	if !hasSignedSessionContextHeaders(r) {
		return r, nil
	}
	contextValue, err := coresession.VerifyHeaders(r, d.sessionContextSecret)
	if err != nil || contextValue == nil || contextValue.SessionID == uuid.Nil || contextValue.IdentityID == uuid.Nil {
		return nil, errors.New("invalid signed session context")
	}
	if !validAAL(contextValue.AAL) {
		return nil, errors.New("invalid session assurance level")
	}

	cleanRequest := r.Clone(r.Context())
	for _, suffix := range []string{"Session-ID", "Identity-ID", "AAL", "Signature"} {
		cleanRequest.Header.Del(coresession.HeaderPrefix + suffix)
	}
	now := time.Now().UTC()
	trustedSession := &coresession.Session{
		ID:              contextValue.SessionID,
		IdentityID:      contextValue.IdentityID,
		AAL:             contextValue.AAL,
		AuthenticatedAt: now,
		ExpiresAt:       now.Add(5 * time.Minute),
		Active:          true,
	}
	return cleanRequest.WithContext(coresession.WithSession(cleanRequest.Context(), trustedSession)), nil
}

func hasSignedSessionContextHeaders(r *http.Request) bool {
	if r == nil {
		return false
	}
	for _, suffix := range []string{"Session-ID", "Identity-ID", "AAL", "Signature"} {
		if strings.TrimSpace(r.Header.Get(coresession.HeaderPrefix+suffix)) != "" {
			return true
		}
	}
	return false
}

func validAAL(aal coresession.AAL) bool {
	switch aal {
	case coresession.AAL0, coresession.AAL1, coresession.AAL2:
		return true
	default:
		return false
	}
}

func (d *dataPlane) watchConfiguration() {
	ticker := time.NewTicker(d.reloadInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = d.reload(context.Background())
		case <-d.watchCtx.Done():
			return
		}
	}
}

func (d *dataPlane) reload(ctx context.Context) error {
	if d == nil || d.service == nil || d.egressClient == nil {
		return errors.New("proxy runtime dependencies are unavailable")
	}
	effective, err := d.service.EffectiveConfig(ctx)
	if err != nil {
		d.setLastError(fmt.Errorf("load proxy configuration: %w", err))
		return d.currentError()
	}
	config, rules, err := compileProxyConfig(ctx, effective, d.egressClient, d.upstreamIdentitySigningSecret)
	if err != nil {
		d.setLastError(err)
		return err
	}
	candidate := coreproxy.NewProxy(config, rules, xlog.Default())
	previous := d.snapshot.Swap(&dataPlaneSnapshot{proxy: candidate})
	if previous != nil && previous.proxy != nil {
		previous.proxy.Close()
	}
	d.setLastError(nil)
	return nil
}

func (d *dataPlane) Readiness(ctx context.Context) error {
	if d == nil || d.snapshot.Load() == nil {
		return errors.New("proxy data plane is not configured")
	}
	if err := d.currentError(); err != nil {
		return err
	}
	return nil
}

func (d *dataPlane) Close() {
	if d == nil {
		return
	}
	d.stopOnce.Do(func() {
		if d.cancel != nil {
			d.cancel()
		}
		if d.server != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = d.server.Shutdown(shutdownCtx)
		}
		if snapshot := d.snapshot.Load(); snapshot != nil && snapshot.proxy != nil {
			snapshot.proxy.Close()
		}
	})
}

func (d *dataPlane) setLastError(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastErr = err
}

func (d *dataPlane) currentError() error {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.lastErr
}

func validateStoredConfiguration(ctx context.Context, proxySvc *service.Service, egressClient *egress.Client, upstreamIdentitySigningSecret string) error {
	effective, err := proxySvc.EffectiveConfig(ctx)
	if err != nil {
		return fmt.Errorf("load proxy configuration: %w", err)
	}
	_, _, err = compileProxyConfig(ctx, effective, egressClient, upstreamIdentitySigningSecret)
	return err
}

func compileProxyConfig(ctx context.Context, effective *service.EffectiveConfig, egressClient *egress.Client, upstreamIdentitySigningSecret string) (coreproxy.Config, *coreproxy.RuleEngine, error) {
	if effective == nil || egressClient == nil {
		return coreproxy.Config{}, nil, errors.New("proxy configuration dependencies are unavailable")
	}

	config := coreproxy.DefaultConfig()
	config.IdentitySigningSecret = strings.TrimSpace(upstreamIdentitySigningSecret)
	config.Upstreams = make(map[string]coreproxy.Upstream, len(effective.Upstreams))
	config.EgressTransport = egressClient.RoundTripper()
	enabledUpstreams := make(map[string]bool, len(effective.Upstreams))
	for _, upstream := range effective.Upstreams {
		name := strings.ToLower(strings.TrimSpace(upstream.Name))
		if name == "" {
			return coreproxy.Config{}, nil, errors.New("stored proxy upstream is missing a name")
		}
		if _, err := egressClient.ValidateURL(ctx, upstream.URL); err != nil {
			return coreproxy.Config{}, nil, fmt.Errorf("stored proxy upstream %q violates the egress policy: %w", name, err)
		}
		if !upstream.Enabled {
			continue
		}
		coreUpstream, err := toCoreUpstream(upstream)
		if err != nil {
			return coreproxy.Config{}, nil, fmt.Errorf("compile proxy upstream %q: %w", name, err)
		}
		config.Upstreams[name] = coreUpstream
		enabledUpstreams[name] = true
	}

	rules := make([]coreproxy.Rule, 0, len(effective.Routes))
	for _, route := range effective.Routes {
		if route.Enabled && isProtectedDataPlanePath(route.Path) {
			return coreproxy.Config{}, nil, fmt.Errorf("stored proxy route %q targets a protected core path", route.ID)
		}
		coreRule, err := toCoreRule(route)
		if err != nil {
			return coreproxy.Config{}, nil, fmt.Errorf("compile proxy route %q: %w", route.ID, err)
		}
		if route.Enabled && !enabledUpstreams[coreRule.Target] {
			return coreproxy.Config{}, nil, fmt.Errorf("enabled proxy route %q targets a disabled or missing upstream", route.ID)
		}
		rules = append(rules, coreRule)
	}
	return config, coreproxy.NewRuleEngine(rules), nil
}

func toCoreUpstream(upstream store.Upstream) (coreproxy.Upstream, error) {
	coreUpstream := coreproxy.Upstream{
		URL:                     strings.TrimRight(strings.TrimSpace(upstream.URL), "/"),
		HealthCheck:             strings.TrimSpace(upstream.HealthCheck),
		HealthCheckExpectedBody: strings.TrimSpace(upstream.HealthCheckExpectedBody),
		MaxConnections:          upstream.MaxConnections,
		Headers:                 copyStringMap(upstream.Headers),
	}
	if coreUpstream.HealthCheck == "" {
		coreUpstream.HealthCheck = "/health"
	}
	if !strings.HasPrefix(coreUpstream.HealthCheck, "/") {
		return coreproxy.Upstream{}, errors.New("health check path must begin with a slash")
	}
	if rawTimeout := strings.TrimSpace(upstream.Timeout); rawTimeout != "" {
		timeout, err := time.ParseDuration(rawTimeout)
		if err != nil || timeout <= 0 {
			return coreproxy.Upstream{}, errors.New("invalid upstream timeout")
		}
		coreUpstream.Timeout = timeout
	}
	if upstream.CircuitBreaker != nil {
		breaker := coreproxy.DefaultCircuitBreakerConfig()
		if upstream.CircuitBreaker.FailureThreshold > 0 {
			breaker.FailureThreshold = upstream.CircuitBreaker.FailureThreshold
		}
		if upstream.CircuitBreaker.SuccessThreshold > 0 {
			breaker.SuccessThreshold = upstream.CircuitBreaker.SuccessThreshold
		}
		if rawTimeout := strings.TrimSpace(upstream.CircuitBreaker.Timeout); rawTimeout != "" {
			timeout, err := time.ParseDuration(rawTimeout)
			if err != nil || timeout <= 0 {
				return coreproxy.Upstream{}, errors.New("invalid circuit breaker timeout")
			}
			breaker.Timeout = timeout
		}
		coreUpstream.CircuitBreaker = breaker
	}
	return coreUpstream, nil
}

func toCoreRule(route store.Route) (coreproxy.Rule, error) {
	coreRule := coreproxy.Rule{
		ID:           strings.TrimSpace(route.ID),
		Path:         strings.TrimSpace(route.Path),
		Methods:      append([]string(nil), route.Methods...),
		RequireAuth:  route.RequireAuth,
		RequiredAAL:  coresession.AAL(strings.ToLower(strings.TrimSpace(route.RequiredAAL))),
		Capabilities: append([]string(nil), route.Capabilities...),
		Target:       strings.ToLower(strings.TrimSpace(route.Target)),
		Priority:     route.Priority,
		Headers:      copyStringMap(route.Headers),
		Enabled:      route.Enabled,
		Description:  strings.TrimSpace(route.Description),
	}
	if route.RateLimit != nil {
		if route.RateLimit.RequestsPerSecond <= 0 || route.RateLimit.BurstSize <= 0 {
			return coreproxy.Rule{}, errors.New("invalid route rate limit")
		}
		coreRule.RateLimit = &coreproxy.RateLimitConfig{
			RequestsPerSecond: route.RateLimit.RequestsPerSecond,
			BurstSize:         route.RateLimit.BurstSize,
			ByIP:              route.RateLimit.ByIP,
			ByUser:            route.RateLimit.ByUser,
			ByPath:            route.RateLimit.ByPath,
		}
	}
	if route.Rewrite != nil {
		coreRule.Rewrite = &coreproxy.RewriteConfig{
			StripPrefix: strings.TrimSpace(route.Rewrite.StripPrefix),
			AddPrefix:   strings.TrimSpace(route.Rewrite.AddPrefix),
		}
	}
	if err := coreRule.Validate(); err != nil {
		return coreproxy.Rule{}, err
	}
	return coreRule, nil
}

func isProtectedDataPlanePath(pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	for _, protected := range protectedDataPlanePaths {
		matched, err := path.Match(pattern, protected)
		if err == nil && matched {
			return true
		}
		matched, err = path.Match(pattern, protected+"/probe")
		if err == nil && matched {
			return true
		}
	}
	return false
}

func copyStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return map[string]string{}
	}
	copy := make(map[string]string, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}
