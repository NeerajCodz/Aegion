import apiClient from './client';
import type { PolicyABACRule, PolicyReBACNamespace, PolicyReBACTuple } from '../types';

type ListEnvelope<T> = {
  items: T[];
  count: number;
};

type PolicySimulationResponse = {
  allowed: boolean;
  model_used: string;
  deny_reason: string;
  eval_path: string[];
};

export const policyApi = {
  listABACRules: async (): Promise<PolicyABACRule[]> => {
    const response = await apiClient.get<ListEnvelope<PolicyABACRule>>('/admin/policy/abac-rules');
    return response.data.items ?? [];
  },

  upsertABACRule: async (payload: {
    id?: string;
    name: string;
    description?: string;
    expression: string;
    priority: number;
    effect: 'allow' | 'deny';
    enabled: boolean;
  }): Promise<void> => {
    await apiClient.post('/admin/policy/abac-rules', payload);
  },

  deleteABACRule: async (id: string): Promise<void> => {
    await apiClient.delete(`/admin/policy/abac-rules/${encodeURIComponent(id)}`);
  },

  listReBACTuples: async (): Promise<PolicyReBACTuple[]> => {
    const response = await apiClient.get<ListEnvelope<PolicyReBACTuple>>('/admin/policy/rebac-tuples');
    return response.data.items ?? [];
  },

  listReBACNamespaces: async (): Promise<PolicyReBACNamespace[]> => {
    const response = await apiClient.get<ListEnvelope<PolicyReBACNamespace>>('/admin/policy/rebac-namespaces');
    return response.data.items ?? [];
  },

  upsertReBACNamespace: async (payload: {
    id?: string;
    name: string;
    config?: Record<string, unknown>;
    active: boolean;
  }): Promise<void> => {
    await apiClient.post('/admin/policy/rebac-namespaces', payload);
  },

  deleteReBACNamespace: async (id: string): Promise<void> => {
    await apiClient.delete(`/admin/policy/rebac-namespaces/${encodeURIComponent(id)}`);
  },

  upsertReBACTuple: async (payload: {
    id?: string;
    namespace: string;
    object_id: string;
    relation: string;
    subject_id: string;
  }): Promise<void> => {
    await apiClient.post('/admin/policy/rebac-tuples', payload);
  },

  deleteReBACTuple: async (id: string): Promise<void> => {
    await apiClient.delete(`/admin/policy/rebac-tuples/${encodeURIComponent(id)}`);
  },

  simulate: async (payload: {
    subject: string;
    resource: string;
    resource_type: string;
    action: string;
    model?: string;
    context?: {
      ip?: string;
      tenant_id?: string;
      extra?: Record<string, string>;
    };
  }): Promise<PolicySimulationResponse> => {
    const response = await apiClient.post<PolicySimulationResponse>('/admin/policy/simulate', payload);
    return response.data;
  },
};
