import apiClient from './client';

export interface ActivityFeedItem {
  id: string;
  operator_id?: string;
  action: string;
  resource_type: string;
  resource_id: string;
  details?: Record<string, unknown>;
  ip_address: string;
  created_at: string;
}

interface ActivityFeedResponse {
  items: ActivityFeedItem[];
  count: number;
  pagination: {
    page: number;
    per_page: number;
    total: number;
    pages: number;
  };
  total_items: number;
}

export const activityApi = {
  list: async (page = 1, perPage = 20): Promise<ActivityFeedResponse> => {
    const response = await apiClient.get<ActivityFeedResponse>(`/admin/logs/activity?page=${page}&per_page=${perPage}`);
    return response.data;
  },
};
