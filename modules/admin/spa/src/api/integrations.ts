import apiClient from './client';
import type {
  IntegrationOverview,
  ProxyRouteSummary,
  ProxyUpstreamSummary,
  SetupStatus,
  SocialPresetSummary,
  SocialProviderSummary,
  SSOConnectionSummary,
} from '../types';

type Envelope<T> = {
  count: number;
  [key: string]: T[] | number;
};

function readItems<T>(payload: Envelope<T>, key: string): T[] {
  const value = payload[key];
  return Array.isArray(value) ? (value as T[]) : [];
}

export const integrationsApi = {
  overview: async (): Promise<IntegrationOverview> => {
    const response = await apiClient.get<IntegrationOverview>('/admin/integrations/overview');
    return response.data;
  },

  setupStatus: async (): Promise<SetupStatus> => {
    const response = await apiClient.get<SetupStatus>('/admin/setup/status');
    return response.data;
  },

  listSocialProviders: async (): Promise<SocialProviderSummary[]> => {
    const response = await apiClient.get<Envelope<SocialProviderSummary>>('/admin/integrations/social/providers');
    return readItems(response.data, 'providers');
  },

  listSocialPresets: async (): Promise<SocialPresetSummary[]> => {
    const response = await apiClient.get<Envelope<SocialPresetSummary>>('/admin/integrations/social/presets');
    return readItems(response.data, 'presets');
  },

  upsertSocialProvider: async (payload: {
    slug: string;
    display_name: string;
    preset?: string;
    enabled: boolean;
    redirect_uri: string;
    client_id: string;
    client_secret?: string;
  }): Promise<void> => {
    await apiClient.post('/admin/integrations/social/providers', payload);
  },

  deleteSocialProvider: async (slug: string): Promise<void> => {
    await apiClient.delete(`/admin/integrations/social/providers/${encodeURIComponent(slug)}`);
  },

  listSSOConnections: async (): Promise<SSOConnectionSummary[]> => {
    const response = await apiClient.get<Envelope<SSOConnectionSummary>>('/admin/integrations/sso/connections');
    return readItems(response.data, 'connections');
  },

  upsertSSOConnection: async (payload: {
    slug: string;
    display_name: string;
    entity_id: string;
    sso_url: string;
    metadata_url?: string;
    certificate_pem?: string;
    domains?: string[];
    attribute_mapping?: Record<string, string>;
    jit_provisioning?: boolean;
    default_redirect_to?: string;
    extra_authn_context?: Record<string, string>;
    enabled: boolean;
  }): Promise<void> => {
    await apiClient.post('/admin/integrations/sso/connections', payload);
  },

  deleteSSOConnection: async (slug: string): Promise<void> => {
    await apiClient.delete(`/admin/integrations/sso/connections/${encodeURIComponent(slug)}`);
  },

  listProxyUpstreams: async (): Promise<ProxyUpstreamSummary[]> => {
    const response = await apiClient.get<Envelope<ProxyUpstreamSummary>>('/admin/integrations/proxy/upstreams');
    return readItems(response.data, 'upstreams');
  },

  upsertProxyUpstream: async (payload: {
    name: string;
    url: string;
    health_check?: string;
    health_check_expected_body?: string;
    timeout?: string;
    max_connections?: number;
    headers?: Record<string, string>;
    circuit_breaker?: Record<string, unknown>;
    enabled: boolean;
  }): Promise<void> => {
    await apiClient.post('/admin/integrations/proxy/upstreams', payload);
  },

  deleteProxyUpstream: async (name: string): Promise<void> => {
    await apiClient.delete(`/admin/integrations/proxy/upstreams/${encodeURIComponent(name)}`);
  },

  listProxyRoutes: async (): Promise<ProxyRouteSummary[]> => {
    const response = await apiClient.get<Envelope<ProxyRouteSummary>>('/admin/integrations/proxy/routes');
    return readItems(response.data, 'routes');
  },

  upsertProxyRoute: async (payload: {
    id?: string;
    path: string;
    methods?: string[];
    require_auth: boolean;
    required_aal?: string;
    capabilities?: string[];
    rate_limit?: Record<string, unknown>;
    target: string;
    priority?: number;
    headers?: Record<string, string>;
    rewrite?: Record<string, unknown>;
    enabled: boolean;
    description?: string;
  }): Promise<void> => {
    await apiClient.post('/admin/integrations/proxy/routes', payload);
  },

  deleteProxyRoute: async (id: string): Promise<void> => {
    await apiClient.delete(`/admin/integrations/proxy/routes/${encodeURIComponent(id)}`);
  },

  simulateProxyRoute: async (payload: {
    path: string;
    method: string;
    authenticated: boolean;
    aal?: string;
    capabilities?: string[];
  }): Promise<{
    matched: boolean;
    allowed: boolean;
    denial_reason?: string;
    identity_needed?: boolean;
    rewritten_path?: string;
    rule?: Record<string, unknown>;
    upstream?: Record<string, unknown>;
    evaluation?: Record<string, unknown>;
  }> => {
    const response = await apiClient.post('/admin/integrations/proxy/simulate', payload);
    return response.data;
  },
};
