import { useState } from 'react';
import { Link } from 'react-router-dom';
import { Search, ChevronLeft, ChevronRight, AlertCircle, User } from 'lucide-react';
import { useIdentities } from '../hooks/useIdentities';

export function Identities() {
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(1);
  const [status, setStatus] = useState('');
  const perPage = 20;

  const { data, isLoading, error } = useIdentities({
    search: search || undefined,
    status: status || undefined,
    page,
    per_page: perPage,
  });

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault();
    setPage(1);
  };

  if (error) {
    return (
      <div className="card p-6">
        <div className="flex items-center gap-3 text-red-600">
          <AlertCircle className="w-5 h-5" />
          <span>Failed to load identities</span>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-surface-900">Identities</h1>
        <p className="text-surface-500">Manage user identities</p>
      </div>

      {/* Filters */}
      <div className="card p-4">
        <form onSubmit={handleSearch} className="flex flex-col sm:flex-row gap-4">
          <div className="flex-1 relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-surface-400" />
            <input
              type="text"
              placeholder="Search by email or name..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="input pl-10"
            />
          </div>
          <select
            value={status}
            onChange={(e) => {
              setStatus(e.target.value);
              setPage(1);
            }}
            className="input w-full sm:w-40"
          >
            <option value="">All Status</option>
            <option value="active">Active</option>
            <option value="suspended">Suspended</option>
            <option value="pending">Pending</option>
          </select>
          <button type="submit" className="btn btn-primary">
            Search
          </button>
        </form>
      </div>

      {/* Table */}
      <div className="card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead className="bg-surface-50 border-b border-surface-200">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Identity
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Status
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  MFA
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Created
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Last Login
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
              ) : data?.data.length === 0 ? (
                <tr>
                  <td colSpan={5} className="px-4 py-8 text-center text-surface-400">
                    No identities found
                  </td>
                </tr>
              ) : (
                data?.data.map((identity) => (
                  <tr key={identity.id} className="hover:bg-surface-50 transition-colors">
                    <td className="px-4 py-4">
                      <Link
                        to={`/identities/${identity.id}`}
                        className="flex items-center gap-3 hover:text-aegion-600"
                      >
                        <div className="w-10 h-10 bg-surface-200 rounded-full flex items-center justify-center flex-shrink-0">
                          {identity.avatar_url ? (
                            <img
                              src={identity.avatar_url}
                              alt=""
                              className="w-10 h-10 rounded-full"
                            />
                          ) : (
                            <User className="w-5 h-5 text-surface-500" />
                          )}
                        </div>
                        <div>
                          <p className="font-medium text-surface-900">
                            {identity.display_name}
                          </p>
                          <p className="text-sm text-surface-500">{identity.email}</p>
                        </div>
                      </Link>
                    </td>
                    <td className="px-4 py-4">
                      <span
                        className={`badge ${
                          identity.status === 'active'
                            ? 'badge-success'
                            : identity.status === 'suspended'
                            ? 'badge-error'
                            : 'badge-warning'
                        }`}
                      >
                        {identity.status}
                      </span>
                    </td>
                    <td className="px-4 py-4">
                      <span
                        className={`badge ${
                          identity.mfa_enabled ? 'badge-success' : 'badge-warning'
                        }`}
                      >
                        {identity.mfa_enabled ? 'Enabled' : 'Disabled'}
                      </span>
                    </td>
                    <td className="px-4 py-4 text-sm text-surface-500">
                      {new Date(identity.created_at).toLocaleDateString()}
                    </td>
                    <td className="px-4 py-4 text-sm text-surface-500">
                      {identity.last_login_at
                        ? new Date(identity.last_login_at).toLocaleDateString()
                        : 'Never'}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>

        {/* Pagination */}
        {data && data.total_pages > 1 && (
          <div className="flex items-center justify-between px-4 py-3 border-t border-surface-200">
            <p className="text-sm text-surface-500">
              Showing {(page - 1) * perPage + 1} to{' '}
              {Math.min(page * perPage, data.total)} of {data.total} results
            </p>
            <div className="flex items-center gap-2">
              <button
                onClick={() => setPage((p) => Math.max(1, p - 1))}
                disabled={page === 1}
                className="btn btn-secondary p-2"
              >
                <ChevronLeft className="w-4 h-4" />
              </button>
              <span className="text-sm text-surface-600">
                Page {page} of {data.total_pages}
              </span>
              <button
                onClick={() => setPage((p) => Math.min(data.total_pages, p + 1))}
                disabled={page >= data.total_pages}
                className="btn btn-secondary p-2"
              >
                <ChevronRight className="w-4 h-4" />
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
