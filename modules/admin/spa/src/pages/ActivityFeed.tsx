import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Activity, AlertCircle } from 'lucide-react';
import { activityApi } from '../api/activity';

export function ActivityFeed() {
  const [page, setPage] = useState(1);
  const perPage = 20;

  const { data, isLoading, error } = useQuery({
    queryKey: ['activity-feed', page],
    queryFn: () => activityApi.list(page, perPage),
  });

  if (error) {
    return (
      <div className="card p-6">
        <div className="flex items-center gap-3 text-red-600">
          <AlertCircle className="w-5 h-5" />
          <span>Failed to load activity feed.</span>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-surface-900">Activity Feed</h1>
        <p className="text-surface-500">Recent administrative actions across the control plane.</p>
      </div>

      <div className="card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead className="bg-surface-50 border-b border-surface-200">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Timestamp
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Action
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Resource
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Operator
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  IP
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-surface-200">
              {isLoading ? (
                <tr>
                  <td colSpan={5} className="px-4 py-8 text-center">
                    <div className="animate-spin rounded-full h-6 w-6 border-b-2 border-aegion-600 mx-auto"></div>
                  </td>
                </tr>
              ) : (data?.items.length ?? 0) === 0 ? (
                <tr>
                  <td colSpan={5} className="px-4 py-8 text-center text-surface-400">
                    <Activity className="w-12 h-12 mx-auto mb-2" />
                    <p>No activity records found</p>
                  </td>
                </tr>
              ) : (
                data?.items.map((entry) => (
                  <tr key={entry.id} className="hover:bg-surface-50 transition-colors">
                    <td className="px-4 py-3 text-sm text-surface-500">
                      {new Date(entry.created_at).toLocaleString()}
                    </td>
                    <td className="px-4 py-3 text-sm text-surface-900">{entry.action}</td>
                    <td className="px-4 py-3 text-sm text-surface-500">
                      {entry.resource_type}:{' '}
                      <span className="font-mono text-xs">{entry.resource_id}</span>
                    </td>
                    <td className="px-4 py-3 text-sm text-surface-500">{entry.operator_id ?? 'system'}</td>
                    <td className="px-4 py-3 text-sm text-surface-500">{entry.ip_address || '-'}</td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>

        {data && data.pagination.pages > 1 && (
          <div className="flex items-center justify-between px-4 py-3 border-t border-surface-200">
            <p className="text-sm text-surface-500">
              Showing {(page - 1) * perPage + 1} to {Math.min(page * perPage, data.pagination.total)} of{' '}
              {data.pagination.total} records
            </p>
            <div className="flex items-center gap-2">
              <button
                onClick={() => setPage((current) => Math.max(1, current - 1))}
                disabled={page === 1}
                className="btn btn-secondary"
              >
                Previous
              </button>
              <button
                onClick={() => setPage((current) => Math.min(data.pagination.pages, current + 1))}
                disabled={page >= data.pagination.pages}
                className="btn btn-secondary"
              >
                Next
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
