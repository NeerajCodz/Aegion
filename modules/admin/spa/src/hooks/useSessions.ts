import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { sessionsApi, type SessionFilters } from '../api/sessions';

export function useSessions(filters: SessionFilters = {}) {
  return useQuery({
    queryKey: ['sessions', filters],
    queryFn: () => sessionsApi.list(filters),
    staleTime: 15000,
    refetchInterval: 30000,
  });
}

export function useSession(id: string) {
  return useQuery({
    queryKey: ['session', id],
    queryFn: () => sessionsApi.get(id),
    enabled: !!id,
  });
}

export function useRevokeSession() {
  const queryClient = useQueryClient();
  
  return useMutation({
    mutationFn: (id: string) => sessionsApi.revoke(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['sessions'] });
    },
  });
}

export function useRevokeAllSessions() {
  const queryClient = useQueryClient();
  
  return useMutation({
    mutationFn: (identityId: string) => sessionsApi.revokeAll(identityId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['sessions'] });
    },
  });
}
