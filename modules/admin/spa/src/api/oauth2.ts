import apiClient from './client';
import type { OAuth2ClientSummary, OAuth2TokenSummary } from '../types';

interface PaginationMeta {
  page: number;
  per_page: number;
  total: number;
  pages: number;
}

interface ListResponse<T> {
  items: T[];
  count: number;
  pagination: PaginationMeta;
}

interface ClientWriteResponse {
  client: OAuth2ClientSummary;
  client_secret?: string;
}

interface RotateSecretResponse {
  id: string;
  client_secret: string;
  rotated_at: string;
}

export const oauth2AdminApi = {
  listClients: async (page = 1, perPage = 20): Promise<ListResponse<OAuth2ClientSummary>> => {
    const response = await apiClient.get<ListResponse<OAuth2ClientSummary>>(
      `/admin/oauth2/clients?page=${page}&per_page=${perPage}`
    );
    return response.data;
  },

  listTokens: async (page = 1, perPage = 20, tokenType = ''): Promise<ListResponse<OAuth2TokenSummary>> => {
    const params = new URLSearchParams({
      page: String(page),
      per_page: String(perPage),
    });
    if (tokenType.trim() !== '') {
      params.set('token_type', tokenType.trim());
    }
    const response = await apiClient.get<ListResponse<OAuth2TokenSummary>>(`/admin/oauth2/tokens?${params.toString()}`);
    return response.data;
  },

  revokeToken: async (tokenType: string, id: string, reason = ''): Promise<void> => {
    await apiClient.post('/admin/oauth2/tokens/revoke', {
      token_type: tokenType,
      id,
      reason,
    });
  },

  createClient: async (payload: {
    name: string;
    description?: string;
    redirect_uris: string[];
    grant_types?: string[];
    response_types?: string[];
    scopes?: string[];
    token_endpoint_auth_method?: string;
    require_pkce?: boolean;
    require_consent?: boolean;
    allow_offline_access?: boolean;
    client_secret?: string;
  }): Promise<ClientWriteResponse> => {
    const response = await apiClient.post<ClientWriteResponse>('/admin/oauth2/clients', payload);
    return response.data;
  },

  deleteClient: async (id: string): Promise<void> => {
    await apiClient.delete(`/admin/oauth2/clients/${encodeURIComponent(id)}`);
  },

  rotateClientSecret: async (id: string): Promise<RotateSecretResponse> => {
    const response = await apiClient.post<RotateSecretResponse>(`/admin/oauth2/clients/${encodeURIComponent(id)}/rotate-secret`);
    return response.data;
  },
};
