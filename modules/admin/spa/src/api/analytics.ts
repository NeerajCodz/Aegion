import { apiClient } from './client';
import type {
  StorageBackendConfig,
  SyncStrategyConfig,
  RetentionPolicy,
  WebhookConfig,
  WebhookDeliveryRecord,
  DashboardDefinition,
  AnalyticsEvent,
  ScheduledReport,
  HealthStatus,
  AnalyticsStats,
  PaginatedAnalyticsResponse,
  AnalyticsPreferences,
  ValidationResult,
} from '../types/analytics';

const BASE_URL = '/v1/analytics';

// Configuration Endpoints
export const analyticsConfigApi = {
  // Storage Configuration
  async getStorageConfig(): Promise<StorageBackendConfig> {
    const response = await apiClient.get<StorageBackendConfig>(
      `${BASE_URL}/config/storage`
    );
    return response.data;
  },

  async updateStorageConfig(config: StorageBackendConfig): Promise<StorageBackendConfig> {
    const response = await apiClient.put<StorageBackendConfig>(
      `${BASE_URL}/config/storage`,
      config
    );
    return response.data;
  },

  async testStorageConnection(config: StorageBackendConfig): Promise<{ success: boolean; message: string }> {
    const response = await apiClient.post<{ success: boolean; message: string }>(
      `${BASE_URL}/config/storage/test`,
      config
    );
    return response.data;
  },

  // Sync Configuration
  async getSyncConfig(): Promise<SyncStrategyConfig> {
    const response = await apiClient.get<SyncStrategyConfig>(
      `${BASE_URL}/config/sync`
    );
    return response.data;
  },

  async updateSyncConfig(config: SyncStrategyConfig): Promise<SyncStrategyConfig> {
    const response = await apiClient.put<SyncStrategyConfig>(
      `${BASE_URL}/config/sync`,
      config
    );
    return response.data;
  },

  async triggerManualSync(): Promise<{ sync_id: string; status: string }> {
    const response = await apiClient.post<{ sync_id: string; status: string }>(
      `${BASE_URL}/config/sync/trigger`
    );
    return response.data;
  },

  async getSyncStatus(syncId: string): Promise<{ status: string; progress: number; last_updated: string }> {
    const response = await apiClient.get<{ status: string; progress: number; last_updated: string }>(
      `${BASE_URL}/config/sync/${syncId}/status`
    );
    return response.data;
  },

  // Retention Configuration
  async getRetentionPolicy(): Promise<RetentionPolicy> {
    const response = await apiClient.get<RetentionPolicy>(
      `${BASE_URL}/config/retention`
    );
    return response.data;
  },

  async updateRetentionPolicy(policy: RetentionPolicy): Promise<RetentionPolicy> {
    const response = await apiClient.put<RetentionPolicy>(
      `${BASE_URL}/config/retention`,
      policy
    );
    return response.data;
  },

  async triggerArchival(category?: string): Promise<{ archival_id: string; status: string }> {
    const response = await apiClient.post<{ archival_id: string; status: string }>(
      `${BASE_URL}/config/retention/archive`,
      { category }
    );
    return response.data;
  },

  async getArchiveHistory(limit: number = 50): Promise<Array<{ id: string; category?: string; timestamp: string; status: string; size_bytes: number }>> {
    const response = await apiClient.get<Array<{ id: string; category?: string; timestamp: string; status: string; size_bytes: number }>>(
      `${BASE_URL}/config/retention/archive-history`,
      { params: { limit } }
    );
    return response.data;
  },

  // Webhook Configuration
  async listWebhooks(): Promise<WebhookConfig[]> {
    const response = await apiClient.get<{ data: WebhookConfig[] }>(
      `${BASE_URL}/webhooks`
    );
    return response.data.data;
  },

  async getWebhook(webhookId: string): Promise<WebhookConfig> {
    const response = await apiClient.get<WebhookConfig>(
      `${BASE_URL}/webhooks/${webhookId}`
    );
    return response.data;
  },

  async createWebhook(webhook: WebhookConfig): Promise<WebhookConfig> {
    const response = await apiClient.post<WebhookConfig>(
      `${BASE_URL}/webhooks`,
      webhook
    );
    return response.data;
  },

  async updateWebhook(webhookId: string, webhook: WebhookConfig): Promise<WebhookConfig> {
    const response = await apiClient.put<WebhookConfig>(
      `${BASE_URL}/webhooks/${webhookId}`,
      webhook
    );
    return response.data;
  },

  async deleteWebhook(webhookId: string): Promise<{ success: boolean }> {
    const response = await apiClient.delete<{ success: boolean }>(
      `${BASE_URL}/webhooks/${webhookId}`
    );
    return response.data;
  },

  async testWebhook(webhookId: string): Promise<{ success: boolean; status_code: number; response_time_ms: number }> {
    const response = await apiClient.post<{ success: boolean; status_code: number; response_time_ms: number }>(
      `${BASE_URL}/webhooks/${webhookId}/test`
    );
    return response.data;
  },

  async getWebhookDeliveryHistory(
    webhookId: string,
    limit: number = 50,
    offset: number = 0
  ): Promise<{ data: WebhookDeliveryRecord[]; total: number }> {
    const response = await apiClient.get<{ data: WebhookDeliveryRecord[]; total: number }>(
      `${BASE_URL}/webhooks/${webhookId}/delivery-history`,
      { params: { limit, offset } }
    );
    return response.data;
  },

  async replayWebhookDeliveries(
    webhookId: string,
    deliveryIds: string[]
  ): Promise<{ success: boolean; replayed_count: number }> {
    const response = await apiClient.post<{ success: boolean; replayed_count: number }>(
      `${BASE_URL}/webhooks/${webhookId}/replay`,
      { delivery_ids: deliveryIds }
    );
    return response.data;
  },
};

// Dashboard Endpoints
export const analyticsPublicDashboardsApi = {
  async listDashboards(): Promise<DashboardDefinition[]> {
    const response = await apiClient.get<{ data: DashboardDefinition[] }>(
      `${BASE_URL}/dashboards`
    );
    return response.data.data;
  },

  async getDashboard(dashboardId: string): Promise<DashboardDefinition> {
    const response = await apiClient.get<DashboardDefinition>(
      `${BASE_URL}/dashboards/${dashboardId}`
    );
    return response.data;
  },

  async createDashboard(dashboard: DashboardDefinition): Promise<DashboardDefinition> {
    const response = await apiClient.post<DashboardDefinition>(
      `${BASE_URL}/dashboards`,
      dashboard
    );
    return response.data;
  },

  async updateDashboard(dashboardId: string, dashboard: DashboardDefinition): Promise<DashboardDefinition> {
    const response = await apiClient.put<DashboardDefinition>(
      `${BASE_URL}/dashboards/${dashboardId}`,
      dashboard
    );
    return response.data;
  },

  async deleteDashboard(dashboardId: string): Promise<{ success: boolean }> {
    const response = await apiClient.delete<{ success: boolean }>(
      `${BASE_URL}/dashboards/${dashboardId}`
    );
    return response.data;
  },

  async shareDashboard(
    dashboardId: string,
    options: { expiration_hours?: number; public: boolean }
  ): Promise<{ share_token: string; share_url: string; expires_at?: string }> {
    const response = await apiClient.post<{ share_token: string; share_url: string; expires_at?: string }>(
      `${BASE_URL}/dashboards/${dashboardId}/share`,
      options
    );
    return response.data;
  },

  async executeDashboardQuery(
    dashboardId: string,
    componentId: string,
    filters?: Record<string, unknown>
  ): Promise<PaginatedAnalyticsResponse<Record<string, unknown>>> {
    const response = await apiClient.post<PaginatedAnalyticsResponse<Record<string, unknown>>>(
      `${BASE_URL}/dashboards/${dashboardId}/components/${componentId}/execute`,
      { filters }
    );
    return response.data;
  },
};

// Events Endpoints
export const analyticsEventsApi = {
  async listEvents(
    page: number = 1,
    pageSize: number = 50,
    filters?: Record<string, unknown>,
    sort?: string
  ): Promise<PaginatedAnalyticsResponse<AnalyticsEvent>> {
    const response = await apiClient.get<PaginatedAnalyticsResponse<AnalyticsEvent>>(
      `${BASE_URL}/events`,
      { params: { page, page_size: pageSize, ...filters, sort } }
    );
    return response.data;
  },

  async getEvent(eventId: string): Promise<AnalyticsEvent> {
    const response = await apiClient.get<AnalyticsEvent>(
      `${BASE_URL}/events/${eventId}`
    );
    return response.data;
  },

  async searchEvents(
    query: string,
    filters?: Record<string, unknown>
  ): Promise<PaginatedAnalyticsResponse<AnalyticsEvent>> {
    const response = await apiClient.post<PaginatedAnalyticsResponse<AnalyticsEvent>>(
      `${BASE_URL}/events/search`,
      { query, filters }
    );
    return response.data;
  },

  async exportEvents(
    format: 'csv' | 'json' | 'parquet',
    filters?: Record<string, unknown>
  ): Promise<Blob> {
    const response = await apiClient.post(
      `${BASE_URL}/events/export`,
      { format, filters },
      { responseType: 'blob' }
    );
    return response.data;
  },

  async getRelatedEvents(eventId: string): Promise<AnalyticsEvent[]> {
    const response = await apiClient.get<{ data: AnalyticsEvent[] }>(
      `${BASE_URL}/events/${eventId}/related`
    );
    return response.data.data;
  },
};

// Reports Endpoints
export const analyticsReportsApi = {
  async listReports(): Promise<ScheduledReport[]> {
    const response = await apiClient.get<{ data: ScheduledReport[] }>(
      `${BASE_URL}/reports`
    );
    return response.data.data;
  },

  async getReport(reportId: string): Promise<ScheduledReport> {
    const response = await apiClient.get<ScheduledReport>(
      `${BASE_URL}/reports/${reportId}`
    );
    return response.data;
  },

  async createReport(report: ScheduledReport): Promise<ScheduledReport> {
    const response = await apiClient.post<ScheduledReport>(
      `${BASE_URL}/reports`,
      report
    );
    return response.data;
  },

  async updateReport(reportId: string, report: ScheduledReport): Promise<ScheduledReport> {
    const response = await apiClient.put<ScheduledReport>(
      `${BASE_URL}/reports/${reportId}`,
      report
    );
    return response.data;
  },

  async deleteReport(reportId: string): Promise<{ success: boolean }> {
    const response = await apiClient.delete<{ success: boolean }>(
      `${BASE_URL}/reports/${reportId}`
    );
    return response.data;
  },

  async generateReport(reportId: string): Promise<{ report_id: string; status: string; generated_at: string }> {
    const response = await apiClient.post<{ report_id: string; status: string; generated_at: string }>(
      `${BASE_URL}/reports/${reportId}/generate`
    );
    return response.data;
  },

  async downloadReport(reportId: string): Promise<Blob> {
    const response = await apiClient.get(
      `${BASE_URL}/reports/${reportId}/download`,
      { responseType: 'blob' }
    );
    return response.data;
  },
};

// Health & Status Endpoints
export const analyticsHealthApi = {
  async getHealthStatus(): Promise<HealthStatus> {
    const response = await apiClient.get<HealthStatus>(
      `${BASE_URL}/health`
    );
    return response.data;
  },

  async getStats(): Promise<AnalyticsStats> {
    const response = await apiClient.get<AnalyticsStats>(
      `${BASE_URL}/stats`
    );
    return response.data;
  },

  async getMetrics(
    metric_type: 'api_latency' | 'error_rate' | 'storage' | 'all' = 'all'
  ): Promise<Record<string, unknown>> {
    const response = await apiClient.get<Record<string, unknown>>(
      `${BASE_URL}/metrics`,
      { params: { metric_type } }
    );
    return response.data;
  },
};

// Preferences Endpoints
export const analyticsPreferencesApi = {
  async getUserPreferences(): Promise<AnalyticsPreferences> {
    const response = await apiClient.get<AnalyticsPreferences>(
      `${BASE_URL}/user/preferences`
    );
    return response.data;
  },

  async updateUserPreferences(prefs: AnalyticsPreferences): Promise<AnalyticsPreferences> {
    const response = await apiClient.put<AnalyticsPreferences>(
      `${BASE_URL}/user/preferences`,
      prefs
    );
    return response.data;
  },

  async addFavoriteDashboard(dashboardId: string): Promise<{ success: boolean }> {
    const response = await apiClient.post<{ success: boolean }>(
      `${BASE_URL}/user/favorites/dashboards/${dashboardId}`
    );
    return response.data;
  },

  async removeFavoriteDashboard(dashboardId: string): Promise<{ success: boolean }> {
    const response = await apiClient.delete<{ success: boolean }>(
      `${BASE_URL}/user/favorites/dashboards/${dashboardId}`
    );
    return response.data;
  },
};

// Validation Endpoints
export const analyticsValidationApi = {
  async validateStorageConfig(config: StorageBackendConfig): Promise<ValidationResult> {
    const response = await apiClient.post<ValidationResult>(
      `${BASE_URL}/validate/storage`,
      config
    );
    return response.data;
  },

  async validateSyncConfig(config: SyncStrategyConfig): Promise<ValidationResult> {
    const response = await apiClient.post<ValidationResult>(
      `${BASE_URL}/validate/sync`,
      config
    );
    return response.data;
  },

  async validateRetentionPolicy(policy: RetentionPolicy): Promise<ValidationResult> {
    const response = await apiClient.post<ValidationResult>(
      `${BASE_URL}/validate/retention`,
      policy
    );
    return response.data;
  },

  async validateWebhookConfig(config: WebhookConfig): Promise<ValidationResult> {
    const response = await apiClient.post<ValidationResult>(
      `${BASE_URL}/validate/webhook`,
      config
    );
    return response.data;
  },
};
