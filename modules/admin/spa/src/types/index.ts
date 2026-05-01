export interface Identity {
  id: string;
  email: string;
  display_name: string;
  avatar_url?: string;
  status: 'active' | 'suspended' | 'pending';
  mfa_enabled: boolean;
  created_at: string;
  updated_at: string;
  last_login_at?: string;
  metadata?: Record<string, unknown>;
}

export interface IdentitySession {
  id: string;
  identity_id: string;
  user_agent: string;
  ip_address: string;
  created_at: string;
  expires_at: string;
  last_active_at: string;
  is_current?: boolean;
}

export interface Operator {
  id: string;
  email: string;
  name: string;
  role: string;
  status: 'active' | 'inactive';
  permissions?: Record<string, boolean>;
  effective_permissions?: string[];
  created_at: string;
  updated_at?: string;
  last_login_at?: string;
}

export interface Role {
  id: string;
  name: string;
  description: string;
  permissions: string[];
  is_system: boolean;
  created_at: string;
  updated_at: string;
}

export interface SystemSettings {
  session_lifetime_hours: number;
  mfa_required: boolean;
  password_min_length: number;
  max_login_attempts: number;
  lockout_duration_minutes: number;
  allowed_domains?: string[];
}

export interface PaginatedResponse<T> {
  data: T[];
  total: number;
  page: number;
  per_page: number;
  total_pages: number;
}

export interface ApiError {
  message: string;
  code: string;
  details?: Record<string, unknown>;
}

export interface DashboardStats {
  total_identities: number;
  active_sessions: number;
  identities_last_24h: number;
  mfa_adoption_rate: number;
}

export type HealthState = 'healthy' | 'degraded' | 'offline';

export interface ModuleHealthStatus {
  key: string;
  label: string;
  endpoint: string;
  status: HealthState;
  status_code: number;
  response_time_ms: number;
  message: string;
  checked_at: string;
}

export interface DashboardConfig {
  base_path: string;
}

export interface ObservabilityProbeStatus {
  key: string;
  label: string;
  url: string;
  status: HealthState;
  status_code: number;
  response_time_ms: number;
  message: string;
  checked_at: string;
}

export interface ObservabilityTelemetrySummary {
  service_name: string;
  service_version: string;
  environment: string;
  instance_id: string;
  traces_enabled: boolean;
  metrics_enabled: boolean;
  logs_enabled: boolean;
  traces_endpoint: string;
  metrics_endpoint: string;
  logs_endpoint: string;
  trace_sampling_ratio: number;
  metric_export_interval: string;
  trace_export_timeout: string;
  insecure_exporter: boolean;
  traces_endpoint_present: boolean;
  metrics_endpoint_present: boolean;
  logs_endpoint_present: boolean;
}

export interface ObservabilityGuardrailsSummary {
  admin_auth_required: boolean;
  observability_rbac: boolean;
  admin_rate_limiting: boolean;
  admin_csrf_protection: boolean;
  strict_transport_security: boolean;
  trusted_proxy_headers: boolean;
  scim_bearer_auth: boolean;
  scim_unknown_field_rejection: boolean;
  scim_body_limit_bytes: number;
  telemetry_secrets_redacted: boolean;
  warnings: string[];
}

export interface ObservabilitySCIMSummary {
  enabled: boolean;
  base_path: string;
  mapping_count: number;
  token_count: number;
  active_token_count: number;
  expired_token_count: number;
  expiring_token_count: number;
  wildcard_token_count: number;
  write_token_count: number;
  last_token_used_at?: string;
  token_prefix: string;
  warnings: string[];
}

export interface ObservabilitySummary {
  enabled: boolean;
  generated_at: string;
  telemetry: ObservabilityTelemetrySummary;
  guardrails: ObservabilityGuardrailsSummary;
  scim?: ObservabilitySCIMSummary;
  stack: ObservabilityProbeStatus[];
}

export interface AuthState {
  operator: Operator | null;
  token: string | null;
  isAuthenticated: boolean;
}

export interface LoginCredentials {
  email: string;
  password: string;
}

export interface SCIMToken {
  id: string;
  name: string;
  description: string;
  prefix: string;
  permissions: string[];
  created_by: string;
  created_at: string;
  expires_at?: string;
  last_used_at?: string;
  active: boolean;
}

export interface SCIMMapping {
  id: string;
  name: string;
  description: string;
  username_source: string;
  username_custom?: string;
  email_source: string;
  name_mapping: Record<string, string>;
  attribute_mapping: Record<string, string>;
  group_mapping: Record<string, string>;
  created_at: string;
  updated_at: string;
}

export interface IntegrationOverview {
  social_providers: number;
  social_links: number;
  sso_connections: number;
  proxy_upstreams: number;
  proxy_routes: number;
  scim_tokens: number;
  oauth2_clients: number;
  oauth2_tokens: number;
}

export interface SetupStatus {
  operators: number;
  roles: number;
  api_keys: number;
  social_providers: number;
  social_enabled: number;
  social_links: number;
  sso_connections: number;
  sso_enabled: number;
  proxy_upstreams: number;
  proxy_routes: number;
  proxy_enabled: number;
  scim_tokens: number;
  oauth2_clients: number;
  oauth2_tokens: number;
  ip_bans: number;
  audit_events_24h: number;
  admin_operators: number;
  has_admin_operator: boolean;
  has_social_provider: boolean;
  has_sso_connection: boolean;
  has_proxy_route: boolean;
  has_scim_token: boolean;
  has_oauth2_client: boolean;
  has_ip_ban: boolean;
}

export interface SocialProviderSummary {
  slug: string;
  display_name: string;
  preset: string;
  protocol: string;
  enabled: boolean;
  redirect_uri?: string;
  created_at: string;
  updated_at: string;
}

export interface SocialPresetSummary {
  slug: string;
  display_name: string;
  preset: string;
  protocol: string;
}

export interface SSOConnectionSummary {
  slug: string;
  display_name: string;
  entity_id: string;
  metadata_url?: string;
  enabled: boolean;
  jit_provisioning: boolean;
  created_at: string;
  updated_at: string;
}

export interface ProxyUpstreamSummary {
  name: string;
  url: string;
  health_check?: string;
  health_check_expected_body?: string;
  timeout?: string;
  max_connections: number;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface ProxyRouteSummary {
  id: string;
  path: string;
  target: string;
  require_auth: boolean;
  required_aal?: string;
  priority: number;
  enabled: boolean;
  description?: string;
  created_at: string;
  updated_at: string;
}

export interface PolicyABACRule {
  id: string;
  name: string;
  description?: string;
  expression: string;
  priority: number;
  effect: 'allow' | 'deny';
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface PolicyReBACTuple {
  id: string;
  namespace: string;
  object_id: string;
  relation: string;
  subject_id: string;
  created_at: string;
}

export interface PolicyReBACNamespace {
  id: string;
  name: string;
  config: Record<string, unknown>;
  version: number;
  active: boolean;
  created_at: string;
  updated_at: string;
}

export interface OAuth2ClientSummary {
  id: string;
  name: string;
  description?: string;
  redirect_uris: string[];
  grant_types: string[];
  response_types: string[];
  scopes: string[];
  token_endpoint_auth_method: string;
  require_pkce: boolean;
  require_consent: boolean;
  allow_offline_access: boolean;
  created_at: string;
  updated_at: string;
}

export interface OAuth2TokenSummary {
  token_type: string;
  id: string;
  client_id: string;
  identity_id: string;
  session_id: string;
  scopes: string[];
  audience: string[];
  status: string;
  expires_at: string;
  created_at: string;
  metadata?: Record<string, unknown>;
}

export interface IPBanRecord {
  id: string;
  cidr: string;
  reason: string;
  created_by?: string;
  created_at: string;
  updated_at: string;
  expires_at?: string;
  active: boolean;
}
