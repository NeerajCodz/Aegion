import apiClient from './client';
import type {
  Operator,
  PaginatedResponse,
  LoginCredentials,
  DashboardStats,
  SystemSettings,
  DashboardConfig,
  ModuleHealthStatus,
  HealthState,
} from '../types';

const HEALTH_TIMEOUT_MS = 8000;

const isObject = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null;

const probeState = (responseOk: boolean, payload: Record<string, unknown>): HealthState => {
  if (!responseOk) {
    return 'offline';
  }

  const rawStatus = typeof payload.status === 'string' ? payload.status.toLowerCase() : '';
  if (rawStatus === 'ok' || rawStatus === 'ready') {
    return 'healthy';
  }
  return 'degraded';
};

const probeHealth = async (
  key: string,
  label: string,
  endpoint: string
): Promise<ModuleHealthStatus> => {
  const controller = new AbortController();
  const timeout = window.setTimeout(() => controller.abort(), HEALTH_TIMEOUT_MS);
  const started = performance.now();

  try {
    const response = await fetch(endpoint, {
      method: 'GET',
      credentials: 'same-origin',
      signal: controller.signal,
      headers: {
        Accept: 'application/json',
      },
    });

    let payload: Record<string, unknown> = {};
    try {
      const body = await response.json();
      payload = isObject(body) ? body : {};
    } catch {
      payload = {};
    }

    const responseTime = Math.round(performance.now() - started);
    const status = probeState(response.ok, payload);
    const message =
      typeof payload.error === 'string'
        ? payload.error
        : typeof payload.status === 'string'
        ? payload.status
        : response.statusText || 'Unknown';

    return {
      key,
      label,
      endpoint,
      status,
      status_code: response.status,
      response_time_ms: responseTime,
      message,
      checked_at: new Date().toISOString(),
    };
  } catch (error) {
    const responseTime = Math.round(performance.now() - started);
    const message = error instanceof Error ? error.message : 'Health probe failed';

    return {
      key,
      label,
      endpoint,
      status: 'offline',
      status_code: 0,
      response_time_ms: responseTime,
      message,
      checked_at: new Date().toISOString(),
    };
  } finally {
    clearTimeout(timeout);
  }
};

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

  getConfig: async (): Promise<DashboardConfig> => {
    const response = await apiClient.get<DashboardConfig>('/admin/dashboard/config');
    return response.data;
  },

  getHealth: async (): Promise<ModuleHealthStatus[]> => {
    const probes = await Promise.all([
      probeHealth('service-health', 'Admin Service', '/health'),
      probeHealth('service-ready', 'Admin Readiness', '/health/ready'),
    ]);
    return probes;
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
