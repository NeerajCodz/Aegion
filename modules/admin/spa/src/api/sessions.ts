import apiClient from './client';
import type { IdentitySession, PaginatedResponse } from '../types';

export interface SessionFilters {
  identity_id?: string;
  page?: number;
  per_page?: number;
}

export const sessionsApi = {
  list: async (filters: SessionFilters = {}): Promise<PaginatedResponse<IdentitySession>> => {
    const params = new URLSearchParams();
    if (filters.identity_id) params.append('identity_id', filters.identity_id);
    if (filters.page) params.append('page', filters.page.toString());
    if (filters.per_page) params.append('per_page', filters.per_page.toString());
    
    const response = await apiClient.get<PaginatedResponse<IdentitySession>>(`/admin/sessions?${params}`);
    return response.data;
  },

  get: async (id: string): Promise<IdentitySession> => {
    const response = await apiClient.get<IdentitySession>(`/admin/sessions/${id}`);
    return response.data;
  },

  revoke: async (id: string): Promise<void> => {
    await apiClient.delete(`/admin/sessions/${id}`);
  },

  revokeAll: async (identityId: string): Promise<void> => {
    await apiClient.delete(`/admin/identities/${identityId}/sessions`);
  },
};
