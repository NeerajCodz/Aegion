import type { FormEvent } from 'react';
import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { AlertCircle, KeyRound } from 'lucide-react';
import { oauth2AdminApi } from '../api/oauth2';
import { useAuth } from '../hooks/useAuth';
import { operatorHasPermission } from '../lib/permissions';
import { Alert, AlertDescription } from '@/components/ui/alert';

export function OAuth2() {
  const queryClient = useQueryClient();
  const { operator } = useAuth();
  const [clientsPage] = useState(1);
  const [tokensPage, setTokensPage] = useState(1);
  const [tokenType, setTokenType] = useState('');
  const [latestClientSecret, setLatestClientSecret] = useState<string | null>(null);

  const canReadClients = operatorHasPermission(operator, 'oauth2:clients:read');
  const canManageClients = operatorHasPermission(operator, 'oauth2:clients:manage');
  const canReadTokens = operatorHasPermission(operator, 'oauth2:tokens:read');
  const canRevokeTokens = operatorHasPermission(operator, 'oauth2:tokens:revoke');

  const clientsQuery = useQuery({
    queryKey: ['oauth2-admin', 'clients', clientsPage],
    queryFn: () => oauth2AdminApi.listClients(clientsPage, 20),
    enabled: canReadClients,
  });

  const tokensQuery = useQuery({
    queryKey: ['oauth2-admin', 'tokens', tokensPage, tokenType],
    queryFn: () => oauth2AdminApi.listTokens(tokensPage, 20, tokenType),
    enabled: canReadTokens,
  });

  const revokeMutation = useMutation({
    mutationFn: ({ tokenType, id }: { tokenType: string; id: string }) => oauth2AdminApi.revokeToken(tokenType, id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['oauth2-admin', 'tokens'] });
    },
  });

  const createClientMutation = useMutation({
    mutationFn: oauth2AdminApi.createClient,
    onSuccess: (result) => {
      setLatestClientSecret(result.client_secret ?? null);
      queryClient.invalidateQueries({ queryKey: ['oauth2-admin', 'clients'] });
    },
  });

  const deleteClientMutation = useMutation({
    mutationFn: oauth2AdminApi.deleteClient,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['oauth2-admin', 'clients'] });
    },
  });

  const rotateSecretMutation = useMutation({
    mutationFn: oauth2AdminApi.rotateClientSecret,
    onSuccess: (result) => {
      setLatestClientSecret(result.client_secret);
    },
  });

  const splitCSV = (value: string): string[] =>
    value
      .split(',')
      .map((entry) => entry.trim())
      .filter(Boolean);

  const handleCreateClient = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canManageClients) {
      return;
    }
    const formData = new FormData(event.currentTarget);
    const authMethod = String(formData.get('token_endpoint_auth_method') ?? 'client_secret_basic').trim();
    const clientSecret = String(formData.get('client_secret') ?? '').trim();
    createClientMutation.mutate({
      name: String(formData.get('name') ?? '').trim(),
      description: String(formData.get('description') ?? '').trim() || undefined,
      redirect_uris: splitCSV(String(formData.get('redirect_uris') ?? '')),
      grant_types: splitCSV(String(formData.get('grant_types') ?? 'authorization_code,refresh_token')),
      response_types: splitCSV(String(formData.get('response_types') ?? 'code')),
      scopes: splitCSV(String(formData.get('scopes') ?? 'openid,email,profile')),
      token_endpoint_auth_method: authMethod,
      require_pkce: String(formData.get('require_pkce') ?? 'true') === 'true',
      require_consent: String(formData.get('require_consent') ?? 'true') === 'true',
      allow_offline_access: String(formData.get('allow_offline_access') ?? 'true') === 'true',
      client_secret: authMethod === 'none' ? undefined : clientSecret || undefined,
    });
    event.currentTarget.reset();
  };

  const hasError = clientsQuery.error || tokensQuery.error;
  if (hasError) {
    return (
      <div className="card p-6">
        <div className="flex items-center gap-3 text-red-600">
          <AlertCircle className="w-5 h-5" />
          <span>Failed to load OAuth2 data.</span>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-foreground">OAuth2</h1>
        <p className="text-muted-foreground">Monitor OAuth2 clients and token activity.</p>
      </div>

      {(revokeMutation.error || createClientMutation.error || deleteClientMutation.error || rotateSecretMutation.error) && (
        <Alert variant="destructive">
          <AlertCircle className="w-5 h-5" />
          <AlertDescription>Failed to apply OAuth2 admin changes.</AlertDescription>
        </Alert>
      )}

      {latestClientSecret && (
        <Alert>
          <AlertDescription>
            New client secret: <span className="font-mono text-xs break-all">{latestClientSecret}</span>
          </AlertDescription>
        </Alert>
      )}

      {!canReadClients && !canReadTokens && (
        <Alert variant="warning">
          <AlertCircle className="w-5 h-5" />
          <AlertDescription>Your role does not have OAuth2 read permissions.</AlertDescription>
        </Alert>
      )}

      <div className="card overflow-hidden">
        <div className="px-4 py-3 border-b border-surface-200 flex items-center gap-2">
          <KeyRound className="w-4 h-4 text-surface-500" />
          <h2 className="text-lg font-semibold text-surface-900">OAuth2 Clients</h2>
        </div>
        <form onSubmit={handleCreateClient} className="p-4 border-b border-surface-200 grid grid-cols-1 md:grid-cols-3 gap-3">
          <input
            name="name"
            required
            placeholder="Client name"
            className="input"
            disabled={!canManageClients || createClientMutation.isPending}
          />
          <input
            name="description"
            placeholder="Description"
            className="input"
            disabled={!canManageClients || createClientMutation.isPending}
          />
          <select
            name="token_endpoint_auth_method"
            defaultValue="client_secret_basic"
            className="input"
            disabled={!canManageClients || createClientMutation.isPending}
          >
            <option value="client_secret_basic">client_secret_basic</option>
            <option value="client_secret_post">client_secret_post</option>
            <option value="none">none (public client)</option>
          </select>
          <input
            name="redirect_uris"
            required
            placeholder="https://app.example.com/callback"
            className="input md:col-span-2"
            disabled={!canManageClients || createClientMutation.isPending}
          />
          <input
            name="scopes"
            defaultValue="openid,email,profile"
            placeholder="openid,email,profile"
            className="input"
            disabled={!canManageClients || createClientMutation.isPending}
          />
          <input
            name="grant_types"
            defaultValue="authorization_code,refresh_token"
            placeholder="authorization_code,refresh_token"
            className="input"
            disabled={!canManageClients || createClientMutation.isPending}
          />
          <input
            name="response_types"
            defaultValue="code"
            placeholder="code"
            className="input"
            disabled={!canManageClients || createClientMutation.isPending}
          />
          <input
            name="client_secret"
            placeholder="Optional custom client secret"
            className="input"
            disabled={!canManageClients || createClientMutation.isPending}
          />
          <select
            name="require_pkce"
            defaultValue="true"
            className="input"
            disabled={!canManageClients || createClientMutation.isPending}
          >
            <option value="true">PKCE required</option>
            <option value="false">PKCE optional</option>
          </select>
          <select
            name="require_consent"
            defaultValue="true"
            className="input"
            disabled={!canManageClients || createClientMutation.isPending}
          >
            <option value="true">Consent required</option>
            <option value="false">Consent optional</option>
          </select>
          <select
            name="allow_offline_access"
            defaultValue="true"
            className="input"
            disabled={!canManageClients || createClientMutation.isPending}
          >
            <option value="true">Offline access enabled</option>
            <option value="false">Offline access disabled</option>
          </select>
          <div className="md:col-span-3">
            <button
              type="submit"
              className="btn btn-primary"
              disabled={!canManageClients || createClientMutation.isPending}
            >
              Create Client
            </button>
          </div>
        </form>
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead className="bg-surface-50 border-b border-surface-200">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Name
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Client ID
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Auth Method
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Grants
                </th>
                <th className="px-4 py-3 text-right text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-surface-200">
              {!canReadClients ? (
                <tr>
                  <td colSpan={5} className="px-4 py-6 text-center text-surface-500">
                    Missing permission: oauth2:clients:read
                  </td>
                </tr>
              ) : clientsQuery.isLoading ? (
                <tr>
                  <td colSpan={5} className="px-4 py-6 text-center text-surface-500">
                    Loading clients...
                  </td>
                </tr>
              ) : (clientsQuery.data?.items.length ?? 0) === 0 ? (
                <tr>
                  <td colSpan={5} className="px-4 py-6 text-center text-surface-500">
                    No OAuth2 clients found.
                  </td>
                </tr>
              ) : (
                clientsQuery.data?.items.map((client) => (
                  <tr key={client.id}>
                    <td className="px-4 py-3 text-sm text-surface-900">{client.name}</td>
                    <td className="px-4 py-3 text-sm text-surface-500 font-mono">{client.id}</td>
                    <td className="px-4 py-3 text-sm text-surface-500">{client.token_endpoint_auth_method}</td>
                    <td className="px-4 py-3 text-sm text-surface-500">{client.grant_types.join(', ')}</td>
                    <td className="px-4 py-3">
                      <div className="flex justify-end gap-2">
                        <button
                          type="button"
                          className="btn btn-secondary"
                          disabled={
                            !canManageClients ||
                            rotateSecretMutation.isPending ||
                            client.token_endpoint_auth_method === 'none'
                          }
                          onClick={() => rotateSecretMutation.mutate(client.id)}
                        >
                          Rotate secret
                        </button>
                        <button
                          type="button"
                          className="btn btn-secondary text-red-600 hover:bg-red-50"
                          disabled={!canManageClients || deleteClientMutation.isPending}
                          onClick={() => deleteClientMutation.mutate(client.id)}
                        >
                          Delete
                        </button>
                      </div>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      <div className="card overflow-hidden">
        <div className="px-4 py-3 border-b border-surface-200 flex items-center justify-between">
          <h2 className="text-lg font-semibold text-surface-900">OAuth2 Tokens</h2>
          <select
            className="input max-w-56"
            value={tokenType}
            onChange={(event) => {
              setTokenType(event.target.value);
              setTokensPage(1);
            }}
          >
            <option value="">All token types</option>
            <option value="access_token">Access token</option>
            <option value="refresh_token">Refresh token</option>
            <option value="id_token">ID token</option>
          </select>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead className="bg-surface-50 border-b border-surface-200">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Type
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Token ID
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Client
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Status
                </th>
                <th className="px-4 py-3 text-right text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-surface-200">
              {!canReadTokens ? (
                <tr>
                  <td colSpan={5} className="px-4 py-6 text-center text-surface-500">
                    Missing permission: oauth2:tokens:read
                  </td>
                </tr>
              ) : tokensQuery.isLoading ? (
                <tr>
                  <td colSpan={5} className="px-4 py-6 text-center text-surface-500">
                    Loading tokens...
                  </td>
                </tr>
              ) : (tokensQuery.data?.items.length ?? 0) === 0 ? (
                <tr>
                  <td colSpan={5} className="px-4 py-6 text-center text-surface-500">
                    No tokens found.
                  </td>
                </tr>
              ) : (
                tokensQuery.data?.items.map((token) => (
                  <tr key={`${token.token_type}:${token.id}`}>
                    <td className="px-4 py-3 text-sm text-surface-500">{token.token_type}</td>
                    <td className="px-4 py-3 text-sm text-surface-500 font-mono">{token.id}</td>
                    <td className="px-4 py-3 text-sm text-surface-500 font-mono">{token.client_id}</td>
                    <td className="px-4 py-3 text-sm text-surface-900">{token.status}</td>
                    <td className="px-4 py-3 text-right">
                      <button
                        className="btn btn-secondary"
                        disabled={!canRevokeTokens || revokeMutation.isPending || token.status === 'revoked'}
                        onClick={() => revokeMutation.mutate({ tokenType: token.token_type, id: token.id })}
                      >
                        Revoke
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
