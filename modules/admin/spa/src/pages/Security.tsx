import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { AlertCircle, ShieldBan, Trash2 } from 'lucide-react';
import { securityAdminApi } from '../api/security';
import { useAuth } from '../hooks/useAuth';
import { operatorHasPermission } from '../lib/permissions';
import { Alert, AlertDescription } from '@/components/ui/alert';

export function Security() {
  const queryClient = useQueryClient();
  const { operator } = useAuth();

  const canRead = operatorHasPermission(operator, 'security:read');
  const canCreate = operatorHasPermission(operator, 'security:create');
  const canDelete = operatorHasPermission(operator, 'security:delete');

  const listQuery = useQuery({
    queryKey: ['security', 'ip-bans'],
    queryFn: securityAdminApi.listIPBans,
    enabled: canRead,
  });

  const upsertMutation = useMutation({
    mutationFn: securityAdminApi.upsertIPBan,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['security', 'ip-bans'] }),
  });

  const deleteMutation = useMutation({
    mutationFn: securityAdminApi.deleteIPBan,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['security', 'ip-bans'] }),
  });

  const handleCreate = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!canCreate) return;
    const formData = new FormData(e.currentTarget);
    upsertMutation.mutate({
      cidr: String(formData.get('cidr') ?? '').trim(),
      reason: String(formData.get('reason') ?? '').trim(),
      expires_at: String(formData.get('expires_at') ?? '').trim() || undefined,
    });
    e.currentTarget.reset();
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-foreground">Security</h1>
        <p className="text-muted-foreground">Manage network-level protections and IP bans.</p>
      </div>

      {(listQuery.error || upsertMutation.error || deleteMutation.error) && (
        <Alert variant="destructive">
          <AlertCircle className="w-5 h-5" />
          <AlertDescription>Security operation failed.</AlertDescription>
        </Alert>
      )}

      {!canRead && (
        <Alert variant="warning">
          <AlertCircle className="w-5 h-5" />
          <AlertDescription>Your role does not have security read permission.</AlertDescription>
        </Alert>
      )}

      <div className="card p-6">
        <div className="flex items-center gap-2 mb-4">
          <ShieldBan className="w-5 h-5 text-surface-500" />
          <h2 className="text-lg font-semibold text-surface-900">IP Bans</h2>
        </div>

        <form onSubmit={handleCreate} className="grid grid-cols-1 md:grid-cols-4 gap-3 mb-4">
          <input
            name="cidr"
            required
            placeholder="203.0.113.5 or 203.0.113.0/24"
            className="input"
            disabled={!canCreate || upsertMutation.isPending}
          />
          <input
            name="reason"
            required
            placeholder="Reason"
            className="input"
            disabled={!canCreate || upsertMutation.isPending}
          />
          <input
            name="expires_at"
            type="datetime-local"
            className="input"
            disabled={!canCreate || upsertMutation.isPending}
          />
          <button type="submit" className="btn btn-primary" disabled={!canCreate || upsertMutation.isPending}>
            Add / Update Ban
          </button>
        </form>

        <div className="overflow-x-auto">
          <table className="w-full">
            <thead className="bg-surface-50 border-b border-surface-200">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  CIDR
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Reason
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Expires
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Status
                </th>
                <th className="px-4 py-3 text-right text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-surface-200">
              {listQuery.isLoading ? (
                <tr>
                  <td colSpan={5} className="px-4 py-6 text-center text-surface-500">
                    Loading IP bans...
                  </td>
                </tr>
              ) : (listQuery.data?.length ?? 0) === 0 ? (
                <tr>
                  <td colSpan={5} className="px-4 py-6 text-center text-surface-500">
                    No IP bans configured.
                  </td>
                </tr>
              ) : (
                listQuery.data?.map((entry) => (
                  <tr key={entry.id}>
                    <td className="px-4 py-3 text-sm text-surface-900 font-mono">{entry.cidr}</td>
                    <td className="px-4 py-3 text-sm text-surface-500">{entry.reason}</td>
                    <td className="px-4 py-3 text-sm text-surface-500">
                      {entry.expires_at ? new Date(entry.expires_at).toLocaleString() : 'Never'}
                    </td>
                    <td className="px-4 py-3 text-sm text-surface-900">{entry.active ? 'Active' : 'Expired'}</td>
                    <td className="px-4 py-3 text-right">
                      <button
                        className="btn btn-secondary p-2 text-red-600 hover:bg-red-50"
                        disabled={!canDelete || deleteMutation.isPending}
                        onClick={() => deleteMutation.mutate(entry.id)}
                      >
                        <Trash2 className="w-4 h-4" />
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
