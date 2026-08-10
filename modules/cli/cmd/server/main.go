package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	platformconfig "github.com/aegion/aegion/internal/platform/config"
	"github.com/google/uuid"
)

const (
	artifactName = "aegion-cli"

	defaultTimeout = 15 * time.Second
	maxTimeout     = 60 * time.Second

	maxConfigBytes     int64 = 1 << 20
	maxCredentialBytes int64 = 4 << 10
	maxTLSFileBytes    int64 = 1 << 20
	maxResponseBytes   int64 = 1 << 20
	maxOutputBytes           = 1 << 20
	maxPageSize              = 100
)

var version = "0.2.0"

const (
	exitSuccess       = 0
	exitUsage         = 2
	exitAuthorization = 3
	exitAPI           = 4
)

type options struct {
	configPath     string
	apiURL         string
	caCertFile     string
	clientCertFile string
	clientKeyFile  string
	apiKeyFile     string
	timeout        time.Duration
	showVersion    bool
}

type clientConfig struct {
	baseURL     *url.URL
	apiKey      string
	rootCAs     *x509.CertPool
	clientCerts []tls.Certificate
	timeout     time.Duration
}

type adminClient struct {
	baseURL *url.URL
	apiKey  string
	http    *http.Client
}

type commandRequest struct {
	method      string
	endpoint    string
	query       url.Values
	expectEmpty bool
}

type apiError struct {
	status int
	code   string
}

func (e *apiError) Error() string {
	if e.code != "" {
		return fmt.Sprintf("admin API request failed: HTTP %d (%s)", e.status, e.code)
	}
	return fmt.Sprintf("admin API request failed: HTTP %d", e.status)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	opts, commandArgs, err := parseOptions(args)
	if err != nil {
		writeCLIError(stderr, err)
		writeUsage(stderr)
		return exitUsage
	}
	if opts.showVersion {
		if len(commandArgs) != 0 {
			writeCLIError(stderr, errors.New("--version cannot be combined with a command"))
			return exitUsage
		}
		_, _ = fmt.Fprintf(stdout, "%s %s\n", artifactName, version)
		return exitSuccess
	}
	if len(commandArgs) == 0 || commandArgs[0] == "help" {
		writeUsage(stdout)
		return exitSuccess
	}

	request, err := parseCommand(commandArgs)
	if err != nil {
		writeCLIError(stderr, err)
		writeUsage(stderr)
		return exitUsage
	}

	cfg, err := loadClientConfig(opts)
	if err != nil {
		writeCLIError(stderr, err)
		return exitUsage
	}
	client := newAdminClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()
	response, err := client.do(ctx, request)
	if err != nil {
		writeCLIError(stderr, err)
		var remoteErr *apiError
		if errors.As(err, &remoteErr) && (remoteErr.status == http.StatusUnauthorized || remoteErr.status == http.StatusForbidden) {
			return exitAuthorization
		}
		return exitAPI
	}
	if response == nil {
		return exitSuccess
	}
	if err := writeJSON(stdout, response); err != nil {
		writeCLIError(stderr, err)
		return exitAPI
	}
	return exitSuccess
}

func parseOptions(args []string) (options, []string, error) {
	opts := options{
		configPath:     strings.TrimSpace(os.Getenv("AEGION_CONFIG")),
		apiURL:         strings.TrimSpace(os.Getenv("AEGION_CLI_API_URL")),
		caCertFile:     strings.TrimSpace(os.Getenv("AEGION_CLI_CA_CERT_FILE")),
		clientCertFile: strings.TrimSpace(os.Getenv("AEGION_CLI_CLIENT_CERT_FILE")),
		clientKeyFile:  strings.TrimSpace(os.Getenv("AEGION_CLI_CLIENT_KEY_FILE")),
		apiKeyFile:     strings.TrimSpace(os.Getenv("AEGION_CLI_API_KEY_FILE")),
		timeout:        defaultTimeout,
	}
	fs := flag.NewFlagSet(artifactName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.configPath, "config", opts.configPath, "Optional Aegion configuration file used to resolve modules.admin.public_url")
	fs.StringVar(&opts.apiURL, "api-url", opts.apiURL, "HTTPS admin API base URL")
	fs.StringVar(&opts.caCertFile, "ca-cert-file", opts.caCertFile, "CA bundle for the admin API")
	fs.StringVar(&opts.clientCertFile, "client-cert-file", opts.clientCertFile, "Optional mTLS client certificate")
	fs.StringVar(&opts.clientKeyFile, "client-key-file", opts.clientKeyFile, "Optional mTLS client private key")
	fs.StringVar(&opts.apiKeyFile, "api-key-file", opts.apiKeyFile, "File containing one admin API key")
	fs.DurationVar(&opts.timeout, "timeout", defaultTimeout, "Per-command API deadline (1s to 60s)")
	fs.BoolVar(&opts.showVersion, "version", false, "Print the CLI version")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return opts, []string{"help"}, nil
		}
		return options{}, nil, err
	}
	if opts.timeout < time.Second || opts.timeout > maxTimeout {
		return options{}, nil, fmt.Errorf("--timeout must be between %s and %s", time.Second, maxTimeout)
	}
	return opts, fs.Args(), nil
}

func parseCommand(args []string) (commandRequest, error) {
	if len(args) == 0 {
		return commandRequest{}, errors.New("a command is required")
	}
	switch args[0] {
	case "status":
		if len(args) != 1 {
			return commandRequest{}, errors.New("usage: status")
		}
		return commandRequest{method: http.MethodGet, endpoint: "/api/admin/setup/status"}, nil
	case "identities":
		return parseIdentityCommand(args[1:])
	case "sessions":
		return parseSessionCommand(args[1:])
	case "audit":
		return parseAuditCommand(args[1:])
	default:
		return commandRequest{}, fmt.Errorf("unsupported command %q", args[0])
	}
}

func parseIdentityCommand(args []string) (commandRequest, error) {
	if len(args) == 0 {
		return commandRequest{}, errors.New("identity subcommand is required")
	}
	switch args[0] {
	case "list":
		query, err := parsePagination(args[1:])
		if err != nil {
			return commandRequest{}, err
		}
		return commandRequest{method: http.MethodGet, endpoint: "/api/admin/identities/", query: query}, nil
	case "get":
		id, err := parseUUIDArgument("usage: identities get <identity-id>", args[1:])
		if err != nil {
			return commandRequest{}, err
		}
		return commandRequest{method: http.MethodGet, endpoint: "/api/admin/identities/" + id}, nil
	case "suspend":
		id, err := parseConfirmedUUIDArgument("usage: identities suspend --yes <identity-id>", args[1:])
		if err != nil {
			return commandRequest{}, err
		}
		return commandRequest{method: http.MethodPost, endpoint: "/api/admin/identities/" + id + "/suspend"}, nil
	case "activate":
		id, err := parseConfirmedUUIDArgument("usage: identities activate --yes <identity-id>", args[1:])
		if err != nil {
			return commandRequest{}, err
		}
		return commandRequest{method: http.MethodPost, endpoint: "/api/admin/identities/" + id + "/activate"}, nil
	case "reset-mfa":
		id, err := parseConfirmedUUIDArgument("usage: identities reset-mfa --yes <identity-id>", args[1:])
		if err != nil {
			return commandRequest{}, err
		}
		return commandRequest{method: http.MethodPost, endpoint: "/api/admin/identities/" + id + "/reset-mfa"}, nil
	default:
		return commandRequest{}, fmt.Errorf("unsupported identities command %q", args[0])
	}
}

func parseSessionCommand(args []string) (commandRequest, error) {
	if len(args) == 0 {
		return commandRequest{}, errors.New("session subcommand is required")
	}
	switch args[0] {
	case "list":
		query, err := parsePagination(args[1:])
		if err != nil {
			return commandRequest{}, err
		}
		return commandRequest{method: http.MethodGet, endpoint: "/api/admin/sessions/", query: query}, nil
	case "revoke":
		id, err := parseConfirmedUUIDArgument("usage: sessions revoke --yes <session-id>", args[1:])
		if err != nil {
			return commandRequest{}, err
		}
		return commandRequest{method: http.MethodDelete, endpoint: "/api/admin/sessions/" + id, expectEmpty: true}, nil
	default:
		return commandRequest{}, fmt.Errorf("unsupported sessions command %q", args[0])
	}
}

func parseAuditCommand(args []string) (commandRequest, error) {
	if len(args) == 0 || args[0] != "list" {
		return commandRequest{}, errors.New("usage: audit list [--page N] [--per-page N]")
	}
	query, err := parsePagination(args[1:])
	if err != nil {
		return commandRequest{}, err
	}
	return commandRequest{method: http.MethodGet, endpoint: "/api/admin/audit/", query: query}, nil
}

func parsePagination(args []string) (url.Values, error) {
	page := 1
	perPage := 20
	fs := flag.NewFlagSet("pagination", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.IntVar(&page, "page", page, "page number")
	fs.IntVar(&perPage, "per-page", perPage, "results per page")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if len(fs.Args()) != 0 {
		return nil, errors.New("unexpected positional argument")
	}
	if page < 1 || page > 100000 {
		return nil, errors.New("--page must be between 1 and 100000")
	}
	if perPage < 1 || perPage > maxPageSize {
		return nil, fmt.Errorf("--per-page must be between 1 and %d", maxPageSize)
	}
	return url.Values{"page": {fmt.Sprintf("%d", page)}, "per_page": {fmt.Sprintf("%d", perPage)}}, nil
}

func parseUUIDArgument(usage string, args []string) (string, error) {
	if len(args) != 1 {
		return "", errors.New(usage)
	}
	id, err := uuid.Parse(strings.TrimSpace(args[0]))
	if err != nil {
		return "", errors.New(usage)
	}
	return id.String(), nil
}

func parseConfirmedUUIDArgument(usage string, args []string) (string, error) {
	confirmed := false
	fs := flag.NewFlagSet("confirmation", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&confirmed, "yes", false, "confirm the requested state change")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if !confirmed {
		return "", errors.New(usage)
	}
	return parseUUIDArgument(usage, fs.Args())
}

func loadClientConfig(opts options) (clientConfig, error) {
	apiURL := strings.TrimSpace(opts.apiURL)
	apiKeyPrefix := "aegion_"
	apiKeyLookupPrefixLen := 12

	if configPath := strings.TrimSpace(opts.configPath); configPath != "" {
		if err := checkBoundedRegularFile(configPath, maxConfigBytes); err != nil {
			return clientConfig{}, fmt.Errorf("validate configuration file: %w", err)
		}
		cfg, err := platformconfig.Load(configPath)
		if err != nil {
			return clientConfig{}, fmt.Errorf("load configuration: %w", err)
		}
		apiKeyPrefix = cfg.Admin.APIKeyPrefix
		apiKeyLookupPrefixLen = cfg.Admin.APIKeyLookupPrefixLen
		if apiURL == "" {
			admin, ok := cfg.Modules["admin"]
			if !ok || strings.TrimSpace(admin.PublicURL) == "" {
				return clientConfig{}, errors.New("provide --api-url or configure modules.admin.public_url")
			}
			apiURL = admin.PublicURL
		}
	}
	if apiURL == "" {
		return clientConfig{}, errors.New("provide --api-url or --config with modules.admin.public_url")
	}
	baseURL, err := parseAdminURL(apiURL)
	if err != nil {
		return clientConfig{}, err
	}
	if strings.TrimSpace(opts.caCertFile) == "" {
		return clientConfig{}, errors.New("provide --ca-cert-file")
	}
	if strings.TrimSpace(opts.apiKeyFile) == "" {
		return clientConfig{}, errors.New("provide --api-key-file")
	}
	apiKey, err := readAPIKey(opts.apiKeyFile, apiKeyPrefix, apiKeyLookupPrefixLen)
	if err != nil {
		return clientConfig{}, err
	}
	rootCAs, clientCerts, err := loadTLSMaterial(opts.caCertFile, opts.clientCertFile, opts.clientKeyFile)
	if err != nil {
		return clientConfig{}, err
	}
	return clientConfig{
		baseURL:     baseURL,
		apiKey:      apiKey,
		rootCAs:     rootCAs,
		clientCerts: clientCerts,
		timeout:     opts.timeout,
	}, nil
}

func parseAdminURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, errors.New("admin API URL is invalid")
	}
	if u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("admin API URL must be an absolute HTTPS URL without credentials, query, or fragment")
	}
	if strings.ContainsAny(u.Host, "\r\n") {
		return nil, errors.New("admin API URL contains an invalid host")
	}
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u, nil
}

func readAPIKey(path, prefix string, lookupPrefixLen int) (string, error) {
	data, err := readBoundedSecretFile(path, maxCredentialBytes)
	if err != nil {
		return "", fmt.Errorf("read CLI credential file: %w", err)
	}
	key := strings.TrimSuffix(string(data), "\n")
	key = strings.TrimSuffix(key, "\r")
	if key == "" || strings.ContainsAny(key, " \t\r\n") {
		return "", errors.New("CLI credential file must contain exactly one API key")
	}
	if prefix == "" || lookupPrefixLen <= 0 || !strings.HasPrefix(key, prefix) || len(key) < len(prefix)+lookupPrefixLen {
		return "", errors.New("CLI credential file does not contain an API key accepted by the configured admin API")
	}
	for _, r := range key {
		if r < 0x21 || r > 0x7e {
			return "", errors.New("CLI credential file contains an invalid API key")
		}
	}
	return key, nil
}

func loadTLSMaterial(caPath, certPath, keyPath string) (*x509.CertPool, []tls.Certificate, error) {
	caPEM, err := readBoundedRegularFile(caPath, maxTLSFileBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("read CLI CA certificate: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, nil, errors.New("CLI CA certificate file does not contain a certificate")
	}
	certPath = strings.TrimSpace(certPath)
	keyPath = strings.TrimSpace(keyPath)
	if certPath == "" && keyPath == "" {
		return roots, nil, nil
	}
	if certPath == "" || keyPath == "" {
		return nil, nil, errors.New("--client-cert-file and --client-key-file must be provided together")
	}
	certPEM, err := readBoundedRegularFile(certPath, maxTLSFileBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("read CLI client certificate: %w", err)
	}
	keyPEM, err := readBoundedSecretFile(keyPath, maxTLSFileBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("read CLI client key: %w", err)
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("load CLI client certificate: %w", err)
	}
	return roots, []tls.Certificate{cert}, nil
}

func checkBoundedRegularFile(path string, maxBytes int64) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("path is not a regular file")
	}
	if info.Size() <= 0 {
		return errors.New("file is empty")
	}
	if info.Size() > maxBytes {
		return fmt.Errorf("file exceeds %d-byte limit", maxBytes)
	}
	return nil
}

func readBoundedRegularFile(path string, maxBytes int64) ([]byte, error) {
	if err := checkBoundedRegularFile(path, maxBytes); err != nil {
		return nil, err
	}
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := readBounded(file, maxBytes)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New("file is empty")
	}
	return data, nil
}

func readBoundedSecretFile(path string, maxBytes int64) ([]byte, error) {
	if err := checkBoundedRegularFile(path, maxBytes); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("secret file must not be readable by group or other users")
	}
	return readBoundedRegularFile(path, maxBytes)
}

func newAdminClient(cfg clientConfig) *adminClient {
	transport := &http.Transport{
		Proxy:                 nil,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: cfg.rootCAs, Certificates: cfg.clientCerts},
		MaxConnsPerHost:       2,
		MaxIdleConns:          2,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   cfg.timeout,
		ResponseHeaderTimeout: cfg.timeout,
		ExpectContinueTimeout: time.Second,
	}
	return &adminClient{
		baseURL: cfg.baseURL,
		apiKey:  cfg.apiKey,
		http: &http.Client{
			Transport: transport,
			Timeout:   cfg.timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("redirects are not permitted for admin API requests")
			},
		},
	}
}

func (c *adminClient) do(ctx context.Context, command commandRequest) (any, error) {
	endpoint, err := c.endpointURL(command.endpoint, command.query)
	if err != nil {
		return nil, err
	}
	// #nosec G704 -- endpointURL derives a fixed admin path from the operator-validated HTTPS base URL.
	req, err := http.NewRequestWithContext(ctx, command.method, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create admin API request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-store")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	// #nosec G704 -- endpointURL derives a fixed admin path from the operator-validated HTTPS base URL.
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call admin API: %w", err)
	}
	defer resp.Body.Close()
	body, err := readBounded(resp.Body, maxResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("read admin API response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, parseAPIError(resp.StatusCode, body)
	}
	if resp.StatusCode == http.StatusNoContent && command.expectEmpty {
		return nil, nil
	}
	if len(body) == 0 {
		return nil, errors.New("admin API returned an empty success response")
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return nil, errors.New("admin API returned a non-JSON success response")
	}
	var value any
	if err := jsonUnmarshal(body, &value); err != nil {
		return nil, errors.New("admin API returned an invalid JSON response")
	}
	return redactValue(value), nil
}

func (c *adminClient) endpointURL(endpoint string, query url.Values) (string, error) {
	if !strings.HasPrefix(endpoint, "/api/admin/") {
		return "", errors.New("internal command endpoint is not an admin API path")
	}
	u := *c.baseURL
	basePath := strings.TrimRight(u.Path, "/")
	if strings.HasSuffix(basePath, "/api/admin") {
		u.Path = basePath + strings.TrimPrefix(endpoint, "/api/admin")
	} else {
		u.Path = basePath + endpoint
	}
	u.RawPath = ""
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func parseAPIError(status int, body []byte) error {
	var response struct {
		Error struct {
			Status string `json:"status"`
		} `json:"error"`
	}
	if err := jsonUnmarshal(body, &response); err == nil && safeErrorCode(response.Error.Status) {
		return &apiError{status: status, code: response.Error.Status}
	}
	return &apiError{status: status}
}

var errorCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func safeErrorCode(code string) bool {
	return errorCodePattern.MatchString(code)
}

func readBounded(r io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("response exceeds %d-byte limit", maxBytes)
	}
	return data, nil
}

func writeJSON(w io.Writer, value any) error {
	data, err := jsonMarshal(value)
	if err != nil {
		return errors.New("encode command result")
	}
	if len(data)+1 > maxOutputBytes {
		return errors.New("command result exceeds output limit")
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write command result: %w", err)
	}
	return nil
}

func redactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if isSensitiveField(key) {
				out[key] = "[REDACTED]"
				continue
			}
			out[key] = redactValue(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = redactValue(item)
		}
		return out
	case string:
		if looksLikeSecret(typed) {
			return "[REDACTED]"
		}
		return typed
	default:
		return value
	}
}

func isSensitiveField(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	switch key {
	case "secret", "client_secret", "client-secret", "password", "token", "access_token", "access-token", "refresh_token", "refresh-token", "id_token", "id-token", "api_key", "api-key", "credential", "authorization", "private_key", "private-key", "key", "details":
		return true
	default:
		return strings.Contains(key, "secret") ||
			strings.Contains(key, "password") ||
			strings.Contains(key, "credential") ||
			strings.Contains(key, "api_key") ||
			strings.Contains(key, "api-key") ||
			strings.Contains(key, "private_key") ||
			strings.Contains(key, "private-key") ||
			strings.Contains(key, "token")
	}
}

func looksLikeSecret(value string) bool {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "aegion_") || strings.HasPrefix(strings.ToLower(value), "bearer ") {
		return true
	}
	parts := strings.Split(value, ".")
	return len(parts) == 3 && len(parts[0]) > 8 && len(parts[1]) > 8 && len(parts[2]) > 8
}

func writeCLIError(w io.Writer, err error) {
	_, _ = fmt.Fprintf(w, "%s: %s\n", artifactName, err)
}

func writeUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, `Usage:
  aegion-cli [flags] <command>

Connection flags (or their environment-variable equivalents):
  --api-url URL                HTTPS admin API base URL (AEGION_CLI_API_URL)
  --config PATH                Optional fallback for modules.admin.public_url (AEGION_CONFIG)
  --ca-cert-file PATH          Required CA bundle (AEGION_CLI_CA_CERT_FILE)
  --client-cert-file PATH      Optional mTLS client certificate (AEGION_CLI_CLIENT_CERT_FILE)
  --client-key-file PATH       Optional mTLS private key (AEGION_CLI_CLIENT_KEY_FILE)
  --api-key-file PATH          Required file containing one API key (AEGION_CLI_API_KEY_FILE)
  --timeout DURATION           Per-command deadline, from 1s to 60s

Commands:
  status
  identities list [--page N] [--per-page N]
  identities get <identity-id>
  identities suspend --yes <identity-id>
  identities activate --yes <identity-id>
  identities reset-mfa --yes <identity-id>
  sessions list [--page N] [--per-page N]
  sessions revoke --yes <session-id>
  audit list [--page N] [--per-page N]

The API key is read only from a file and is never accepted as a command-line value.
Exit status: 0 success; 2 invalid invocation, configuration, or credentials;
3 rejected authentication or authorization; 4 network or remote API failure.
`)
}

var (
	jsonMarshal   = func(value any) ([]byte, error) { return json.Marshal(value) }
	jsonUnmarshal = func(data []byte, value any) error { return json.Unmarshal(data, value) }
)
