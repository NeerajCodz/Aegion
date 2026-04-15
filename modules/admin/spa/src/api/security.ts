import apiClient from './client';
import type { IPBanRecord } from '../types';

interface ListIPBansResponse {
  items: IPBanRecord[];
  count: number;
}

export const securityAdminApi = {
  listIPBans: async (): Promise<IPBanRecord[]> => {
    const response = await apiClient.get<ListIPBansResponse>('/admin/security/ip-bans');
    return response.data.items ?? [];
  },

  upsertIPBan: async (payload: { cidr: string; reason: string; expires_at?: string }): Promise<IPBanRecord> => {
    const body = {
      cidr: payload.cidr,
      reason: payload.reason,
      expires_at: payload.expires_at ? new Date(payload.expires_at).toISOString() : undefined,
    };
    const response = await apiClient.post<IPBanRecord>('/admin/security/ip-bans', body);
    return response.data;
  },

  deleteIPBan: async (id: string): Promise<void> => {
    await apiClient.delete(`/admin/security/ip-bans/${id}`);
  },
};
