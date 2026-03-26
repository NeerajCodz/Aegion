import apiClient from './client';
import type { Identity, PaginatedResponse } from '../types';

export interface IdentityFilters {
  search?: string;
  status?: string;
  page?: number;
  per_page?: number;
}

export const identitiesApi = {
  list: async (filters: IdentityFilters = {}): Promise<PaginatedResponse<Identity>> => {
    const params = new URLSearchParams();
    if (filters.search) params.append('search', filters.search);
    if (filters.status) params.append('status', filters.status);
    if (filters.page) params.append('page', filters.page.toString());
    if (filters.per_page) params.append('per_page', filters.per_page.toString());
    
    const response = await apiClient.get<PaginatedResponse<Identity>>(`/admin/identities?${params}`);
    return response.data;
  },

  get: async (id: string): Promise<Identity> => {
    const response = await apiClient.get<Identity>(`/admin/identities/${id}`);
    return response.data;
  },

  update: async (id: string, data: Partial<Identity>): Promise<Identity> => {
    const response = await apiClient.patch<Identity>(`/admin/identities/${id}`, data);
    return response.data;
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
