import apiClient from './client';
import type { Identity, PaginatedResponse } from '../types';

export interface IdentityFilters {
  search?: string;
  status?: string;
  page?: number;
  per_page?: number;
}

interface RawIdentity {
  id: string | number;
  state?: string;
  created_at: string;
  updated_at: string;
  traits?: Record<string, unknown>;
}

interface BackendPaginationResponse<T> {
  items: T[];
  pagination: {
    page: number;
    per_page: number;
    total: number;
    pages: number;
  };
}

const getTraitValue = (traits: Record<string, unknown> | undefined, key: string): string | undefined => {
  const value = traits?.[key];
  return typeof value === 'string' ? value : undefined;
};

const mapIdentity = (raw: RawIdentity): Identity => ({
  id: String(raw.id),
  email: getTraitValue(raw.traits, 'email') ?? '',
  display_name:
    getTraitValue(raw.traits, 'display_name') ??
    getTraitValue(raw.traits, 'name') ??
    getTraitValue(raw.traits, 'email') ??
    '',
  avatar_url: undefined,
  status: raw.state === 'blocked' ? 'suspended' : raw.state === 'inactive' ? 'pending' : 'active',
  mfa_enabled: false,
  created_at: raw.created_at,
  updated_at: raw.updated_at,
  last_login_at: undefined,
  metadata: raw.traits ?? {},
});

export const identitiesApi = {
  list: async (filters: IdentityFilters = {}): Promise<PaginatedResponse<Identity>> => {
    const params = new URLSearchParams();
    if (filters.search) params.append('filter', filters.search);
    if (filters.status) params.append('filter', filters.status);
    if (filters.page) params.append('page', filters.page.toString());
    if (filters.per_page) params.append('per_page', filters.per_page.toString());
    
    const response = await apiClient.get<BackendPaginationResponse<RawIdentity>>(`/admin/identities?${params}`);
    return {
      data: response.data.items.map(mapIdentity),
      total: response.data.pagination.total,
      page: response.data.pagination.page,
      per_page: response.data.pagination.per_page,
      total_pages: response.data.pagination.pages,
    };
  },

  get: async (id: string): Promise<Identity> => {
    const response = await apiClient.get<RawIdentity>(`/admin/identities/${id}`);
    return mapIdentity(response.data);
  },

  update: async (id: string, data: Partial<Identity>): Promise<Identity> => {
    const payload: { traits?: Record<string, unknown>; state?: string } = {};
    if (data.display_name) {
      payload.traits = { display_name: data.display_name, name: data.display_name };
    }
    if (data.status) {
      payload.state =
        data.status === 'suspended' ? 'blocked' :
        data.status === 'pending' ? 'inactive' :
        'active';
    }
    const response = await apiClient.patch<RawIdentity>(`/admin/identities/${id}`, payload);
    return mapIdentity(response.data);
  },

  suspend: async (id: string): Promise<void> => {
    await apiClient.post(`/admin/identities/${id}/suspend`);
  },

  activate: async (id: string): Promise<void> => {
    await apiClient.post(`/admin/identities/${id}/activate`);
  },

  resetMfa: async (id: string): Promise<void> => {
    await apiClient.post(`/admin/identities/${id}/reset-mfa`);
  },

  delete: async (id: string): Promise<void> => {
    await apiClient.delete(`/admin/identities/${id}`);
  },
};
