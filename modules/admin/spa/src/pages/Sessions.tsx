import { useState } from 'react';
import { Activity, AlertCircle, Trash2, Monitor, Smartphone } from 'lucide-react';
import { Dialog } from '@headlessui/react';
import { useSessions, useRevokeSession } from '../hooks/useSessions';

export function Sessions() {
  const [page, setPage] = useState(1);
  const [revokeId, setRevokeId] = useState<string | null>(null);
  const perPage = 20;

  const { data, isLoading, error } = useSessions({
    page,
    per_page: perPage,
  });

  const revokeMutation = useRevokeSession();

  const handleRevoke = async () => {
    if (revokeId) {
      await revokeMutation.mutateAsync(revokeId);
      setRevokeId(null);
    }
  };

  const getDeviceIcon = (userAgent: string) => {
    const ua = userAgent.toLowerCase();
    if (ua.includes('mobile') || ua.includes('android') || ua.includes('iphone')) {
      return Smartphone;
    }
    return Monitor;
  };

  const parseUserAgent = (userAgent: string) => {
    const parts = [];
    if (userAgent.includes('Chrome')) parts.push('Chrome');
    else if (userAgent.includes('Firefox')) parts.push('Firefox');
    else if (userAgent.includes('Safari')) parts.push('Safari');
    else if (userAgent.includes('Edge')) parts.push('Edge');
    else parts.push('Unknown Browser');

    if (userAgent.includes('Windows')) parts.push('Windows');
    else if (userAgent.includes('Mac')) parts.push('macOS');
    else if (userAgent.includes('Linux')) parts.push('Linux');
    else if (userAgent.includes('Android')) parts.push('Android');
    else if (userAgent.includes('iOS') || userAgent.includes('iPhone')) parts.push('iOS');

    return parts.join(' on ');
  };

  if (error) {
    return (
      <div className="card p-6">
        <div className="flex items-center gap-3 text-red-600">
          <AlertCircle className="w-5 h-5" />
          <span>Failed to load sessions</span>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-surface-900">Active Sessions</h1>
        <p className="text-surface-500">Monitor and manage active user sessions</p>
      </div>

      <div className="card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead className="bg-surface-50 border-b border-surface-200">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Device
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  IP Address
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Created
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Last Active
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Expires
                </th>
                <th className="px-4 py-3 text-right text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-surface-200">
              {isLoading ? (
                <tr>
                  <td colSpan={6} className="px-4 py-8 text-center">
                    <div className="animate-spin rounded-full h-6 w-6 border-b-2 border-aegion-600 mx-auto"></div>
                  </td>
                </tr>
              ) : data?.data.length === 0 ? (
                <tr>
                  <td colSpan={6} className="px-4 py-8 text-center text-surface-400">
                    <Activity className="w-12 h-12 mx-auto mb-2" />
                    <p>No active sessions</p>
                  </td>
                </tr>
              ) : (
                data?.data.map((session) => {
                  const DeviceIcon = getDeviceIcon(session.user_agent);
                  return (
                    <tr key={session.id} className="hover:bg-surface-50 transition-colors">
                      <td className="px-4 py-4">
                        <div className="flex items-center gap-3">
                          <div className="p-2 bg-surface-100 rounded-lg">
                            <DeviceIcon className="w-5 h-5 text-surface-500" />
                          </div>
                          <div>
                            <p className="font-medium text-surface-900">
                              {parseUserAgent(session.user_agent)}
                            </p>
                            {session.is_current && (
                              <span className="badge badge-info">Current</span>
                            )}
                          </div>
                        </div>
                      </td>
                      <td className="px-4 py-4">
                        <code className="text-sm text-surface-600 bg-surface-100 px-2 py-1 rounded">
                          {session.ip_address}
                        </code>
                      </td>
                      <td className="px-4 py-4 text-sm text-surface-500">
                        {new Date(session.created_at).toLocaleString()}
                      </td>
                      <td className="px-4 py-4 text-sm text-surface-500">
                        {new Date(session.last_active_at).toLocaleString()}
                      </td>
                      <td className="px-4 py-4 text-sm text-surface-500">
                        {new Date(session.expires_at).toLocaleString()}
                      </td>
                      <td className="px-4 py-4 text-right">
                        <button
                          onClick={() => setRevokeId(session.id)}
                          disabled={session.is_current}
                          className="btn btn-secondary p-2 text-red-600 hover:bg-red-50 disabled:opacity-50"
                          title={session.is_current ? 'Cannot revoke current session' : 'Revoke session'}
                        >
                          <Trash2 className="w-4 h-4" />
                        </button>
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>

        {/* Pagination */}
        {data && data.total_pages > 1 && (
          <div className="flex items-center justify-between px-4 py-3 border-t border-surface-200">
            <p className="text-sm text-surface-500">
              Showing {(page - 1) * perPage + 1} to{' '}
              {Math.min(page * perPage, data.total)} of {data.total} sessions
            </p>
            <div className="flex items-center gap-2">
              <button
                onClick={() => setPage((p) => Math.max(1, p - 1))}
                disabled={page === 1}
                className="btn btn-secondary"
              >
                Previous
              </button>
              <button
                onClick={() => setPage((p) => Math.min(data.total_pages, p + 1))}
                disabled={page >= data.total_pages}
                className="btn btn-secondary"
              >
                Next
              </button>
            </div>
          </div>
        )}
      </div>

      {/* Revoke confirmation dialog */}
      <Dialog
        open={!!revokeId}
        onClose={() => setRevokeId(null)}
        className="relative z-50"
      >
        <div className="fixed inset-0 bg-black/30" aria-hidden="true" />
        <div className="fixed inset-0 flex items-center justify-center p-4">
          <Dialog.Panel className="card p-6 max-w-md w-full">
            <Dialog.Title className="text-lg font-semibold text-surface-900">
              Revoke Session
            </Dialog.Title>
            <Dialog.Description className="text-surface-500 mt-2">
              Are you sure you want to revoke this session? The user will be logged out.
            </Dialog.Description>

            <div className="mt-6 flex justify-end gap-3">
              <button
                onClick={() => setRevokeId(null)}
                className="btn btn-secondary"
              >
                Cancel
              </button>
              <button
                onClick={handleRevoke}
                disabled={revokeMutation.isPending}
                className="btn btn-danger"
              >
                Revoke
              </button>
            </div>
          </Dialog.Panel>
        </div>
      </Dialog>
    </div>
  );
}
