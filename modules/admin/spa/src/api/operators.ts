import apiClient from './client';
import type { Operator, PaginatedResponse, LoginCredentials, DashboardStats, SystemSettings } from '../types';

export const operatorsApi = {
  list: async (page = 1, per_page = 20): Promise<PaginatedResponse<Operator>> => {
    const response = await apiClient.get<PaginatedResponse<Operator>>(
      `/admin/operators?page=${page}&per_page=${per_page}`
    );
    return response.data;
  },

  get: async (id: string): Promise<Operator> => {
    const response = await apiClient.get<Operator>(`/admin/operators/${id}`);
    return response.data;
  },

  create: async (data: Omit<Operator, 'id' | 'created_at' | 'last_login_at'> & { password: string }): Promise<Operator> => {
    const response = await apiClient.post<Operator>('/admin/operators', data);
    return response.data;
  },

  update: async (id: string, data: Partial<Operator>): Promise<Operator> => {
    const response = await apiClient.patch<Operator>(`/admin/operators/${id}`, data);
    return response.data;
  },

  delete: async (id: string): Promise<void> => {
    await apiClient.delete(`/admin/operators/${id}`);
  },

  resetPassword: async (id: string, newPassword: string): Promise<void> => {
    await apiClient.post(`/admin/operators/${id}/reset-password`, { password: newPassword });
  },
};

export const authApi = {
  login: async (credentials: LoginCredentials): Promise<{ token: string; operator: Operator }> => {
    const response = await apiClient.post<{ token: string; operator: Operator }>('/admin/auth/login', credentials);
    return response.data;
  },

  logout: async (): Promise<void> => {
    await apiClient.post('/admin/auth/logout');
  },

  me: async (): Promise<Operator> => {
    const response = await apiClient.get<Operator>('/admin/auth/me');
    return response.data;
  },
};

export const dashboardApi = {
  getStats: async (): Promise<DashboardStats> => {
    const response = await apiClient.get<DashboardStats>('/admin/dashboard/stats');
    return response.data;
  },
};

export const settingsApi = {
  get: async (): Promise<SystemSettings> => {
    const response = await apiClient.get<SystemSettings>('/admin/settings');
    return response.data;
  },

  update: async (settings: Partial<SystemSettings>): Promise<SystemSettings> => {
    const response = await apiClient.patch<SystemSettings>('/admin/settings', settings);
    return response.data;
  },
};
