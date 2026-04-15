import type { FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Activity, AlertCircle, Network, Route, ShieldCheck, Trash2, Users } from 'lucide-react';
import { integrationsApi } from '../api/integrations';
import { useAuth } from '../hooks/useAuth';
import { operatorHasPermission } from '../lib/permissions';

function StatusPill({ enabled }: { enabled: boolean }) {
  return (
    <span
      className={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium ${
        enabled ? 'bg-green-100 text-green-800' : 'bg-surface-100 text-surface-600'
      }`}
    >
      {enabled ? 'Enabled' : 'Disabled'}
    </span>
  );
}

export function Integrations() {
  const queryClient = useQueryClient();
  const { operator } = useAuth();
  const canUpdateConfig = operatorHasPermission(operator, 'config:update');

  const overviewQuery = useQuery({
    queryKey: ['integrations', 'overview'],
    queryFn: integrationsApi.overview,
  });
  const setupQuery = useQuery({
    queryKey: ['integrations', 'setup-status'],
    queryFn: integrationsApi.setupStatus,
  });
  const socialQuery = useQuery({
    queryKey: ['integrations', 'social'],
    queryFn: integrationsApi.listSocialProviders,
  });
  const socialPresetsQuery = useQuery({
    queryKey: ['integrations', 'social-presets'],
    queryFn: integrationsApi.listSocialPresets,
  });
  const ssoQuery = useQuery({
    queryKey: ['integrations', 'sso'],
    queryFn: integrationsApi.listSSOConnections,
  });
  const upstreamQuery = useQuery({
    queryKey: ['integrations', 'proxy-upstreams'],
    queryFn: integrationsApi.listProxyUpstreams,
  });
  const routeQuery = useQuery({
    queryKey: ['integrations', 'proxy-routes'],
    queryFn: integrationsApi.listProxyRoutes,
  });

  const anyError =
    overviewQuery.error ||
    setupQuery.error ||
    socialQuery.error ||
    socialPresetsQuery.error ||
    ssoQuery.error ||
    upstreamQuery.error ||
    routeQuery.error;

  const upsertSocialMutation = useMutation({
    mutationFn: integrationsApi.upsertSocialProvider,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['integrations', 'social'] });
      queryClient.invalidateQueries({ queryKey: ['integrations', 'overview'] });
      queryClient.invalidateQueries({ queryKey: ['integrations', 'setup-status'] });
    },
  });

  const deleteSocialMutation = useMutation({
    mutationFn: integrationsApi.deleteSocialProvider,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['integrations', 'social'] });
      queryClient.invalidateQueries({ queryKey: ['integrations', 'overview'] });
      queryClient.invalidateQueries({ queryKey: ['integrations', 'setup-status'] });
    },
  });

  const upsertSSOMutation = useMutation({
    mutationFn: integrationsApi.upsertSSOConnection,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['integrations', 'sso'] });
      queryClient.invalidateQueries({ queryKey: ['integrations', 'overview'] });
      queryClient.invalidateQueries({ queryKey: ['integrations', 'setup-status'] });
    },
  });

  const deleteSSOMutation = useMutation({
    mutationFn: integrationsApi.deleteSSOConnection,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['integrations', 'sso'] });
      queryClient.invalidateQueries({ queryKey: ['integrations', 'overview'] });
      queryClient.invalidateQueries({ queryKey: ['integrations', 'setup-status'] });
    },
  });

  const upsertUpstreamMutation = useMutation({
    mutationFn: integrationsApi.upsertProxyUpstream,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['integrations', 'proxy-upstreams'] });
      queryClient.invalidateQueries({ queryKey: ['integrations', 'overview'] });
    },
  });

  const deleteUpstreamMutation = useMutation({
    mutationFn: integrationsApi.deleteProxyUpstream,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['integrations', 'proxy-upstreams'] });
      queryClient.invalidateQueries({ queryKey: ['integrations', 'overview'] });
    },
  });

  const upsertRouteMutation = useMutation({
    mutationFn: integrationsApi.upsertProxyRoute,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['integrations', 'proxy-routes'] });
      queryClient.invalidateQueries({ queryKey: ['integrations', 'overview'] });
      queryClient.invalidateQueries({ queryKey: ['integrations', 'setup-status'] });
    },
  });

  const deleteRouteMutation = useMutation({
    mutationFn: integrationsApi.deleteProxyRoute,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['integrations', 'proxy-routes'] });
      queryClient.invalidateQueries({ queryKey: ['integrations', 'overview'] });
      queryClient.invalidateQueries({ queryKey: ['integrations', 'setup-status'] });
    },
  });

  const simulateRouteMutation = useMutation({
    mutationFn: integrationsApi.simulateProxyRoute,
  });

  if (anyError) {
    return (
      <div className="card p-6">
        <div className="flex items-center gap-3 text-red-600">
          <AlertCircle className="w-5 h-5" />
          <span>Failed to load integration data.</span>
        </div>
      </div>
    );
  }

  const overview = overviewQuery.data;
  const setup = setupQuery.data;
  const social = socialQuery.data ?? [];
  const socialPresets = socialPresetsQuery.data ?? [];
  const connections = ssoQuery.data ?? [];
  const upstreams = upstreamQuery.data ?? [];
  const routes = routeQuery.data ?? [];

  const handleSocialSubmit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!canUpdateConfig) {
      return;
    }
    const formData = new FormData(e.currentTarget);
    upsertSocialMutation.mutate({
      slug: String(formData.get('slug') ?? '').trim().toLowerCase(),
      display_name: String(formData.get('display_name') ?? '').trim(),
      preset: String(formData.get('preset') ?? '').trim().toLowerCase() || undefined,
      enabled: String(formData.get('enabled') ?? 'true') === 'true',
      redirect_uri: String(formData.get('redirect_uri') ?? '').trim(),
      client_id: String(formData.get('client_id') ?? '').trim(),
      client_secret: String(formData.get('client_secret') ?? '').trim() || undefined,
    });
    e.currentTarget.reset();
  };

  const handleSSOSubmit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!canUpdateConfig) {
      return;
    }
    const formData = new FormData(e.currentTarget);
    const domains = String(formData.get('domains') ?? '')
      .split(',')
      .map((value) => value.trim())
      .filter(Boolean);
    upsertSSOMutation.mutate({
      slug: String(formData.get('slug') ?? '').trim().toLowerCase(),
      display_name: String(formData.get('display_name') ?? '').trim(),
      entity_id: String(formData.get('entity_id') ?? '').trim(),
      sso_url: String(formData.get('sso_url') ?? '').trim(),
      metadata_url: String(formData.get('metadata_url') ?? '').trim() || undefined,
      certificate_pem: String(formData.get('certificate_pem') ?? '').trim() || undefined,
      domains,
      enabled: String(formData.get('enabled') ?? 'true') === 'true',
      jit_provisioning: String(formData.get('jit_provisioning') ?? 'false') === 'true',
      default_redirect_to: String(formData.get('default_redirect_to') ?? '').trim() || undefined,
      attribute_mapping: {
        subject: String(formData.get('subject_attr') ?? 'subject').trim() || 'subject',
        email: String(formData.get('email_attr') ?? 'email').trim() || 'email',
        display_name: String(formData.get('display_name_attr') ?? 'display_name').trim() || 'display_name',
      },
    });
    e.currentTarget.reset();
  };

  const handleUpstreamSubmit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!canUpdateConfig) {
      return;
    }
    const formData = new FormData(e.currentTarget);
    upsertUpstreamMutation.mutate({
      name: String(formData.get('name') ?? '').trim().toLowerCase(),
      url: String(formData.get('url') ?? '').trim(),
      health_check: String(formData.get('health_check') ?? '').trim() || undefined,
      timeout: String(formData.get('timeout') ?? '').trim() || undefined,
      max_connections: Number.parseInt(String(formData.get('max_connections') ?? '0'), 10) || 0,
      enabled: String(formData.get('enabled') ?? 'true') === 'true',
    });
    e.currentTarget.reset();
  };

  const handleRouteSubmit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!canUpdateConfig) {
      return;
    }
    const formData = new FormData(e.currentTarget);
    const methods = String(formData.get('methods') ?? '')
      .split(',')
      .map((method) => method.trim().toUpperCase())
      .filter(Boolean);
    upsertRouteMutation.mutate({
      id: String(formData.get('id') ?? '').trim() || undefined,
      path: String(formData.get('path') ?? '').trim(),
      target: String(formData.get('target') ?? '').trim().toLowerCase(),
      methods,
      require_auth: String(formData.get('require_auth') ?? 'false') === 'true',
      required_aal: String(formData.get('required_aal') ?? '').trim() || undefined,
      priority: Number.parseInt(String(formData.get('priority') ?? '0'), 10) || 0,
      description: String(formData.get('description') ?? '').trim() || undefined,
      enabled: String(formData.get('enabled') ?? 'true') === 'true',
    });
    e.currentTarget.reset();
  };

  const handleRouteSimulationSubmit = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const formData = new FormData(e.currentTarget);
    const capabilities = String(formData.get('capabilities') ?? '')
      .split(',')
      .map((value) => value.trim())
      .filter(Boolean);
    simulateRouteMutation.mutate({
      path: String(formData.get('path') ?? '').trim(),
      method: String(formData.get('method') ?? 'GET').trim().toUpperCase(),
      authenticated: String(formData.get('authenticated') ?? 'false') === 'true',
      aal: String(formData.get('aal') ?? '').trim() || undefined,
      capabilities,
    });
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-foreground">Integrations</h1>
        <p className="text-muted-foreground">Operational overview for social, SSO, proxy, SCIM, and OAuth2.</p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        <div className="card p-4">
          <p className="text-sm text-surface-500">Social Providers</p>
          <p className="text-2xl font-semibold text-surface-900">{overview?.social_providers ?? 0}</p>
          <p className="text-xs text-surface-500">Links: {overview?.social_links ?? 0}</p>
        </div>
        <div className="card p-4">
          <p className="text-sm text-surface-500">SSO Connections</p>
          <p className="text-2xl font-semibold text-surface-900">{overview?.sso_connections ?? 0}</p>
          <p className="text-xs text-surface-500">Enabled: {setup?.sso_enabled ?? 0}</p>
        </div>
        <div className="card p-4">
          <p className="text-sm text-surface-500">Proxy Routes</p>
          <p className="text-2xl font-semibold text-surface-900">{overview?.proxy_routes ?? 0}</p>
          <p className="text-xs text-surface-500">Upstreams: {overview?.proxy_upstreams ?? 0}</p>
        </div>
        <div className="card p-4">
          <p className="text-sm text-surface-500">SCIM / OAuth2</p>
          <p className="text-2xl font-semibold text-surface-900">
            {(overview?.scim_tokens ?? 0)} / {(overview?.oauth2_clients ?? 0)}
          </p>
          <p className="text-xs text-surface-500">OAuth2 tokens: {overview?.oauth2_tokens ?? 0}</p>
        </div>
      </div>

      {(upsertSocialMutation.error ||
        deleteSocialMutation.error ||
        upsertSSOMutation.error ||
        deleteSSOMutation.error ||
        upsertUpstreamMutation.error ||
        deleteUpstreamMutation.error ||
        upsertRouteMutation.error ||
        deleteRouteMutation.error ||
        simulateRouteMutation.error) && (
        <div className="card p-4 border border-red-200 bg-red-50 text-red-700 text-sm">
          Failed to save integration configuration changes.
        </div>
      )}

      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        <div className="card p-4 flex items-center gap-3">
          <ShieldCheck className="w-5 h-5 text-surface-500" />
          <div>
            <p className="text-xs text-surface-500">Admin Operator</p>
            <StatusPill enabled={Boolean(setup?.has_admin_operator)} />
          </div>
        </div>
        <div className="card p-4 flex items-center gap-3">
          <Users className="w-5 h-5 text-surface-500" />
          <div>
            <p className="text-xs text-surface-500">Social Setup</p>
            <StatusPill enabled={Boolean(setup?.has_social_provider)} />
          </div>
        </div>
        <div className="card p-4 flex items-center gap-3">
          <Network className="w-5 h-5 text-surface-500" />
          <div>
            <p className="text-xs text-surface-500">SSO Setup</p>
            <StatusPill enabled={Boolean(setup?.has_sso_connection)} />
          </div>
        </div>
        <div className="card p-4 flex items-center gap-3">
          <Activity className="w-5 h-5 text-surface-500" />
          <div>
            <p className="text-xs text-surface-500">Proxy Setup</p>
            <StatusPill enabled={Boolean(setup?.has_proxy_route)} />
          </div>
        </div>
      </div>

      <div className="card overflow-hidden">
        <div className="px-4 py-3 border-b border-surface-200">
          <h2 className="text-lg font-semibold text-surface-900">Social Providers</h2>
        </div>
        <form onSubmit={handleSocialSubmit} className="p-4 border-b border-surface-200 grid grid-cols-1 md:grid-cols-3 gap-3">
          <input
            name="slug"
            required
            placeholder="provider slug"
            className="input"
            disabled={!canUpdateConfig || upsertSocialMutation.isPending}
          />
          <input
            name="display_name"
            required
            placeholder="Display name"
            className="input"
            disabled={!canUpdateConfig || upsertSocialMutation.isPending}
          />
          <select
            name="preset"
            defaultValue=""
            className="input"
            disabled={!canUpdateConfig || upsertSocialMutation.isPending}
          >
            <option value="">Custom preset</option>
            {socialPresets.map((preset) => (
              <option key={preset.slug} value={preset.preset || preset.slug}>
                {preset.display_name}
              </option>
            ))}
          </select>
          <input
            name="redirect_uri"
            required
            placeholder="https://app.example.com/auth/callback"
            className="input"
            disabled={!canUpdateConfig || upsertSocialMutation.isPending}
          />
          <input
            name="client_id"
            required
            placeholder="OAuth client ID"
            className="input"
            disabled={!canUpdateConfig || upsertSocialMutation.isPending}
          />
          <input
            name="client_secret"
            placeholder="OAuth client secret (optional)"
            className="input"
            disabled={!canUpdateConfig || upsertSocialMutation.isPending}
          />
          <select
            name="enabled"
            defaultValue="true"
            className="input"
            disabled={!canUpdateConfig || upsertSocialMutation.isPending}
          >
            <option value="true">Enabled</option>
            <option value="false">Disabled</option>
          </select>
          <div className="md:col-span-3">
            <button
              type="submit"
              className="btn btn-primary"
              disabled={!canUpdateConfig || upsertSocialMutation.isPending}
            >
              Save Provider
            </button>
          </div>
        </form>
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead className="bg-surface-50 border-b border-surface-200">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">Name</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">Preset</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">Protocol</th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">Status</th>
                <th className="px-4 py-3 text-right text-xs font-medium text-surface-500 uppercase tracking-wider">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-surface-200">
              {social.length === 0 ? (
                <tr>
                  <td colSpan={5} className="px-4 py-6 text-center text-surface-500">
                    No social providers configured.
                  </td>
                </tr>
              ) : (
                social.map((provider) => (
                  <tr key={provider.slug}>
                    <td className="px-4 py-3 text-sm text-surface-900">{provider.display_name}</td>
                    <td className="px-4 py-3 text-sm text-surface-500">{provider.preset || '-'}</td>
                    <td className="px-4 py-3 text-sm text-surface-500">{provider.protocol || '-'}</td>
                    <td className="px-4 py-3">
                      <StatusPill enabled={provider.enabled} />
                    </td>
                    <td className="px-4 py-3 text-right">
                      <button
                        type="button"
                        className="btn btn-secondary p-2 text-red-600 hover:bg-red-50"
                        disabled={!canUpdateConfig || deleteSocialMutation.isPending}
                        onClick={() => deleteSocialMutation.mutate(provider.slug)}
                        title="Delete social provider"
                      >
                        <Trash2 className="w-4 h-4" />
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <div className="card overflow-hidden">
          <div className="px-4 py-3 border-b border-surface-200">
            <h2 className="text-lg font-semibold text-surface-900">SSO Connections</h2>
          </div>
          <form onSubmit={handleSSOSubmit} className="p-4 border-b border-surface-200 grid grid-cols-1 md:grid-cols-2 gap-3">
            <input
              name="slug"
              required
              placeholder="slug (e.g. okta-acme)"
              className="input"
              disabled={!canUpdateConfig || upsertSSOMutation.isPending}
            />
            <input
              name="display_name"
              required
              placeholder="Display name"
              className="input"
              disabled={!canUpdateConfig || upsertSSOMutation.isPending}
            />
            <input
              name="entity_id"
              required
              placeholder="Entity ID"
              className="input"
              disabled={!canUpdateConfig || upsertSSOMutation.isPending}
            />
            <input
              name="sso_url"
              required
              placeholder="SSO URL"
              className="input"
              disabled={!canUpdateConfig || upsertSSOMutation.isPending}
            />
            <input
              name="metadata_url"
              placeholder="Metadata URL (optional)"
              className="input"
              disabled={!canUpdateConfig || upsertSSOMutation.isPending}
            />
            <input
              name="domains"
              placeholder="Domains (comma-separated)"
              className="input"
              disabled={!canUpdateConfig || upsertSSOMutation.isPending}
            />
            <input
              name="subject_attr"
              defaultValue="subject"
              placeholder="Subject attribute"
              className="input"
              disabled={!canUpdateConfig || upsertSSOMutation.isPending}
            />
            <input
              name="email_attr"
              defaultValue="email"
              placeholder="Email attribute"
              className="input"
              disabled={!canUpdateConfig || upsertSSOMutation.isPending}
            />
            <input
              name="display_name_attr"
              defaultValue="display_name"
              placeholder="Display name attribute"
              className="input"
              disabled={!canUpdateConfig || upsertSSOMutation.isPending}
            />
            <input
              name="default_redirect_to"
              placeholder="Default redirect path"
              className="input"
              disabled={!canUpdateConfig || upsertSSOMutation.isPending}
            />
            <select
              name="enabled"
              defaultValue="true"
              className="input"
              disabled={!canUpdateConfig || upsertSSOMutation.isPending}
            >
              <option value="true">Enabled</option>
              <option value="false">Disabled</option>
            </select>
            <select
              name="jit_provisioning"
              defaultValue="false"
              className="input"
              disabled={!canUpdateConfig || upsertSSOMutation.isPending}
            >
              <option value="false">JIT disabled</option>
              <option value="true">JIT enabled</option>
            </select>
            <textarea
              name="certificate_pem"
              rows={3}
              placeholder="Signing certificate PEM (optional)"
              className="input md:col-span-2 font-mono"
              disabled={!canUpdateConfig || upsertSSOMutation.isPending}
            />
            <div className="md:col-span-2">
              <button
                type="submit"
                className="btn btn-primary"
                disabled={!canUpdateConfig || upsertSSOMutation.isPending}
              >
                Save SSO Connection
              </button>
            </div>
          </form>
          <div className="divide-y divide-surface-200">
            {connections.length === 0 ? (
              <p className="px-4 py-4 text-sm text-surface-500">No SSO connections configured.</p>
            ) : (
              connections.map((connection) => (
                <div key={connection.slug} className="px-4 py-3">
                  <div className="flex items-center justify-between gap-2">
                    <p className="text-sm font-medium text-surface-900">{connection.display_name}</p>
                    <div className="flex items-center gap-2">
                      <StatusPill enabled={connection.enabled} />
                      <button
                        className="btn btn-secondary p-2 text-red-600 hover:bg-red-50"
                        disabled={!canUpdateConfig || deleteSSOMutation.isPending}
                        onClick={() => deleteSSOMutation.mutate(connection.slug)}
                        title="Delete connection"
                      >
                        <Trash2 className="w-4 h-4" />
                      </button>
                    </div>
                  </div>
                  <p className="text-xs text-surface-500">{connection.entity_id}</p>
                </div>
              ))
            )}
          </div>
        </div>

        <div className="card overflow-hidden">
          <div className="px-4 py-3 border-b border-surface-200">
            <h2 className="text-lg font-semibold text-surface-900">Proxy Upstreams</h2>
          </div>
          <form onSubmit={handleUpstreamSubmit} className="p-4 border-b border-surface-200 grid grid-cols-1 md:grid-cols-3 gap-3">
            <input
              name="name"
              required
              placeholder="Upstream name"
              className="input"
              disabled={!canUpdateConfig || upsertUpstreamMutation.isPending}
            />
            <input
              name="url"
              required
              placeholder="https://upstream.internal"
              className="input"
              disabled={!canUpdateConfig || upsertUpstreamMutation.isPending}
            />
            <input
              name="health_check"
              placeholder="/health"
              className="input"
              disabled={!canUpdateConfig || upsertUpstreamMutation.isPending}
            />
            <input
              name="timeout"
              placeholder="5s"
              className="input"
              disabled={!canUpdateConfig || upsertUpstreamMutation.isPending}
            />
            <input
              name="max_connections"
              placeholder="100"
              type="number"
              min={0}
              className="input"
              disabled={!canUpdateConfig || upsertUpstreamMutation.isPending}
            />
            <select
              name="enabled"
              defaultValue="true"
              className="input"
              disabled={!canUpdateConfig || upsertUpstreamMutation.isPending}
            >
              <option value="true">Enabled</option>
              <option value="false">Disabled</option>
            </select>
            <div className="md:col-span-3">
              <button
                type="submit"
                className="btn btn-primary"
                disabled={!canUpdateConfig || upsertUpstreamMutation.isPending}
              >
                Save Upstream
              </button>
            </div>
          </form>
          <div className="divide-y divide-surface-200">
            {upstreams.length === 0 ? (
              <p className="px-4 py-4 text-sm text-surface-500">No proxy upstreams configured.</p>
            ) : (
              upstreams.map((upstream) => (
                <div key={upstream.name} className="px-4 py-3">
                  <div className="flex items-center justify-between gap-2">
                    <p className="text-sm font-medium text-surface-900">{upstream.name}</p>
                    <div className="flex items-center gap-2">
                      <StatusPill enabled={upstream.enabled} />
                      <button
                        type="button"
                        className="btn btn-secondary p-2 text-red-600 hover:bg-red-50"
                        disabled={!canUpdateConfig || deleteUpstreamMutation.isPending}
                        onClick={() => deleteUpstreamMutation.mutate(upstream.name)}
                        title="Delete upstream"
                      >
                        <Trash2 className="w-4 h-4" />
                      </button>
                    </div>
                  </div>
                  <p className="text-xs text-surface-500">{upstream.url}</p>
                </div>
              ))
            )}
          </div>
        </div>
      </div>

      <div className="card overflow-hidden">
        <div className="px-4 py-3 border-b border-surface-200 flex items-center gap-2">
          <Route className="w-4 h-4 text-surface-500" />
          <h2 className="text-lg font-semibold text-surface-900">Proxy Routes</h2>
        </div>
        <form onSubmit={handleRouteSubmit} className="p-4 border-b border-surface-200 grid grid-cols-1 md:grid-cols-3 gap-3">
          <input
            name="id"
            placeholder="Route ID (optional for update)"
            className="input"
            disabled={!canUpdateConfig || upsertRouteMutation.isPending}
          />
          <input
            name="path"
            required
            placeholder="/api/*"
            className="input"
            disabled={!canUpdateConfig || upsertRouteMutation.isPending}
          />
          <input
            name="target"
            required
            placeholder="upstream name"
            className="input"
            disabled={!canUpdateConfig || upsertRouteMutation.isPending}
          />
          <input
            name="methods"
            placeholder="GET,POST"
            className="input"
            disabled={!canUpdateConfig || upsertRouteMutation.isPending}
          />
          <select
            name="require_auth"
            defaultValue="false"
            className="input"
            disabled={!canUpdateConfig || upsertRouteMutation.isPending}
          >
            <option value="false">Auth optional</option>
            <option value="true">Auth required</option>
          </select>
          <input
            name="required_aal"
            placeholder="aal1 / aal2"
            className="input"
            disabled={!canUpdateConfig || upsertRouteMutation.isPending}
          />
          <input
            name="priority"
            type="number"
            defaultValue={0}
            className="input"
            disabled={!canUpdateConfig || upsertRouteMutation.isPending}
          />
          <select
            name="enabled"
            defaultValue="true"
            className="input"
            disabled={!canUpdateConfig || upsertRouteMutation.isPending}
          >
            <option value="true">Enabled</option>
            <option value="false">Disabled</option>
          </select>
          <input
            name="description"
            placeholder="Description"
            className="input"
            disabled={!canUpdateConfig || upsertRouteMutation.isPending}
          />
          <div className="md:col-span-3">
            <button
              type="submit"
              className="btn btn-primary"
              disabled={!canUpdateConfig || upsertRouteMutation.isPending}
            >
              Save Route
            </button>
          </div>
        </form>
        <div className="divide-y divide-surface-200">
          {routes.length === 0 ? (
            <p className="px-4 py-4 text-sm text-surface-500">No proxy routes configured.</p>
          ) : (
            routes.map((route) => (
              <div key={route.id} className="px-4 py-3">
                <div className="flex items-center justify-between gap-2">
                  <p className="text-sm font-medium text-surface-900">{route.path}</p>
                  <div className="flex items-center gap-2">
                    <StatusPill enabled={route.enabled} />
                    <button
                      type="button"
                      className="btn btn-secondary p-2 text-red-600 hover:bg-red-50"
                      disabled={!canUpdateConfig || deleteRouteMutation.isPending}
                      onClick={() => deleteRouteMutation.mutate(route.id)}
                      title="Delete route"
                    >
                      <Trash2 className="w-4 h-4" />
                    </button>
                  </div>
                </div>
                <p className="text-xs text-surface-500">
                  target: {route.target} • priority: {route.priority} • auth: {route.require_auth ? 'required' : 'optional'}
                </p>
              </div>
            ))
          )}
        </div>
      </div>

      <div className="card overflow-hidden">
        <div className="px-4 py-3 border-b border-surface-200">
          <h2 className="text-lg font-semibold text-surface-900">Proxy Route Simulation</h2>
        </div>
        <form onSubmit={handleRouteSimulationSubmit} className="p-4 border-b border-surface-200 grid grid-cols-1 md:grid-cols-3 gap-3">
          <input name="path" required placeholder="/api/private/users" className="input" />
          <select name="method" defaultValue="GET" className="input">
            <option value="GET">GET</option>
            <option value="POST">POST</option>
            <option value="PUT">PUT</option>
            <option value="PATCH">PATCH</option>
            <option value="DELETE">DELETE</option>
          </select>
          <select name="authenticated" defaultValue="false" className="input">
            <option value="false">Unauthenticated</option>
            <option value="true">Authenticated</option>
          </select>
          <input name="aal" placeholder="aal1 / aal2 (optional)" className="input" />
          <input
            name="capabilities"
            placeholder="capabilities (comma separated, optional)"
            className="input md:col-span-2"
          />
          <div className="md:col-span-3">
            <button type="submit" className="btn btn-primary" disabled={simulateRouteMutation.isPending}>
              Simulate Proxy Decision
            </button>
          </div>
        </form>
        {simulateRouteMutation.data && (
          <div className="px-4 py-4 text-sm text-surface-700 space-y-1">
            <p>
              Match:{' '}
              <span className={simulateRouteMutation.data.matched ? 'text-green-700 font-medium' : 'text-red-700 font-medium'}>
                {simulateRouteMutation.data.matched ? 'matched' : 'no match'}
              </span>
            </p>
            <p>
              Decision:{' '}
              <span className={simulateRouteMutation.data.allowed ? 'text-green-700 font-medium' : 'text-red-700 font-medium'}>
                {simulateRouteMutation.data.allowed ? 'allow' : 'deny'}
              </span>
            </p>
            {simulateRouteMutation.data.denial_reason && <p>Deny reason: {simulateRouteMutation.data.denial_reason}</p>}
            {simulateRouteMutation.data.rewritten_path && <p>Rewritten path: {simulateRouteMutation.data.rewritten_path}</p>}
            {simulateRouteMutation.data.evaluation && (
              <p>
                Capability policy: {(simulateRouteMutation.data.evaluation as { capability_fail_closed?: boolean })
                  .capability_fail_closed
                  ? 'fail-closed when capabilities are configured'
                  : 'not applicable'}
              </p>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
