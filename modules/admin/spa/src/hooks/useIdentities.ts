import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { identitiesApi, type IdentityFilters } from '../api/identities';
import type { Identity } from '../types';

export function useIdentities(filters: IdentityFilters = {}) {
  return useQuery({
    queryKey: ['identities', filters],
    queryFn: () => identitiesApi.list(filters),
    staleTime: 30000,
  });
}

export function useIdentity(id: string) {
  return useQuery({
    queryKey: ['identity', id],
    queryFn: () => identitiesApi.get(id),
    enabled: !!id,
  });
}

export function useUpdateIdentity() {
  const queryClient = useQueryClient();
  
  return useMutation({
    mutationFn: ({ id, data }: { id: string; data: Partial<Identity> }) => 
      identitiesApi.update(id, data),
    onSuccess: (_, { id }) => {
      queryClient.invalidateQueries({ queryKey: ['identity', id] });
      queryClient.invalidateQueries({ queryKey: ['identities'] });
    },
  });
}

export function useSuspendIdentity() {
  const queryClient = useQueryClient();
  
  return useMutation({
    mutationFn: (id: string) => identitiesApi.suspend(id),
    onSuccess: (_, id) => {
      queryClient.invalidateQueries({ queryKey: ['identity', id] });
      queryClient.invalidateQueries({ queryKey: ['identities'] });
    },
  });
}

export function useActivateIdentity() {
  const queryClient = useQueryClient();
  
  return useMutation({
    mutationFn: (id: string) => identitiesApi.activate(id),
    onSuccess: (_, id) => {
      queryClient.invalidateQueries({ queryKey: ['identity', id] });
      queryClient.invalidateQueries({ queryKey: ['identities'] });
    },
  });
}

export function useResetMfa() {
  const queryClient = useQueryClient();
  
  return useMutation({
    mutationFn: (id: string) => identitiesApi.resetMfa(id),
    onSuccess: (_, id) => {
      queryClient.invalidateQueries({ queryKey: ['identity', id] });
    },
  });
}

export function useDeleteIdentity() {
  const queryClient = useQueryClient();
  
  return useMutation({
    mutationFn: (id: string) => identitiesApi.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['identities'] });
    },
  });
}
