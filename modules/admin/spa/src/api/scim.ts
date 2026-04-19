import apiClient from './client';
import type { SCIMMapping, SCIMToken } from '../types';

export const scimApi = {
  listTokens: async (): Promise<SCIMToken[]> => {
    const response = await apiClient.get<{ tokens: SCIMToken[] }>('/admin/scim/tokens');
    return response.data.tokens ?? [];
  },

  createToken: async (payload: {
    name: string;
    description?: string;
    permissions: string[];
    expires_at?: string;
  }): Promise<{ token: SCIMToken; plain_token: string }> => {
    const body = {
      ...payload,
      expires_at: payload.expires_at ? new Date(payload.expires_at).toISOString() : undefined,
    };
    const response = await apiClient.post<{ token: SCIMToken; plain_token: string }>('/admin/scim/tokens', body);
    return response.data;
  },

  deleteToken: async (id: string): Promise<void> => {
    await apiClient.delete(`/admin/scim/tokens/${id}`);
  },

  listMappings: async (): Promise<SCIMMapping[]> => {
    const response = await apiClient.get<{ mappings: SCIMMapping[] }>('/admin/scim/mappings');
    return response.data.mappings ?? [];
  },

  createMapping: async (
    payload: Omit<SCIMMapping, 'id' | 'created_at' | 'updated_at'>
  ): Promise<SCIMMapping> => {
    const response = await apiClient.post<{ mapping: SCIMMapping }>('/admin/scim/mappings', payload);
    return response.data.mapping;
  },

  updateMapping: async (
    id: string,
    payload: Omit<SCIMMapping, 'id' | 'created_at' | 'updated_at'>
  ): Promise<SCIMMapping> => {
    const response = await apiClient.put<{ mapping: SCIMMapping }>(`/admin/scim/mappings/${id}`, payload);
    return response.data.mapping;
  },

  deleteMapping: async (id: string): Promise<void> => {
    await apiClient.delete(`/admin/scim/mappings/${id}`);
  },
};
