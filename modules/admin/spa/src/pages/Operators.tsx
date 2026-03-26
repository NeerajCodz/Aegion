import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Shield, Plus, AlertCircle, Trash2, Edit, X } from 'lucide-react';
import { Dialog } from '@headlessui/react';
import { operatorsApi } from '../api/operators';
import type { Operator } from '../types';

export function Operators() {
  const [page, setPage] = useState(1);
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [editOperator, setEditOperator] = useState<Operator | null>(null);
  const [deleteId, setDeleteId] = useState<string | null>(null);
  const perPage = 20;

  const queryClient = useQueryClient();

  const { data, isLoading, error } = useQuery({
    queryKey: ['operators', page],
    queryFn: () => operatorsApi.list(page, perPage),
  });

  const createMutation = useMutation({
    mutationFn: operatorsApi.create,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['operators'] });
      setIsCreateOpen(false);
    },
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: Partial<Operator> }) =>
      operatorsApi.update(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['operators'] });
      setEditOperator(null);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: operatorsApi.delete,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['operators'] });
      setDeleteId(null);
    },
  });

  const handleCreate = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const formData = new FormData(e.currentTarget);
    createMutation.mutate({
      email: formData.get('email') as string,
      name: formData.get('name') as string,
      role: formData.get('role') as 'admin' | 'operator' | 'viewer',
      password: formData.get('password') as string,
      status: 'active',
    });
  };

  const handleUpdate = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!editOperator) return;
    const formData = new FormData(e.currentTarget);
    updateMutation.mutate({
      id: editOperator.id,
      data: {
        name: formData.get('name') as string,
        role: formData.get('role') as 'admin' | 'operator' | 'viewer',
      },
    });
  };

  if (error) {
    return (
      <div className="card p-6">
        <div className="flex items-center gap-3 text-red-600">
          <AlertCircle className="w-5 h-5" />
          <span>Failed to load operators</span>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-surface-900">Operators</h1>
          <p className="text-surface-500">Manage admin users and their permissions</p>
        </div>
        <button onClick={() => setIsCreateOpen(true)} className="btn btn-primary">
          <Plus className="w-4 h-4 mr-2" />
          Add Operator
        </button>
      </div>

      <div className="card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead className="bg-surface-50 border-b border-surface-200">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Operator
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Role
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Status
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Last Login
                </th>
                <th className="px-4 py-3 text-right text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Actions
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
                    <Shield className="w-12 h-12 mx-auto mb-2" />
                    <p>No operators found</p>
                  </td>
                </tr>
              ) : (
                data?.data.map((operator) => (
                  <tr key={operator.id} className="hover:bg-surface-50 transition-colors">
                    <td className="px-4 py-4">
                      <div>
                        <p className="font-medium text-surface-900">{operator.name}</p>
                        <p className="text-sm text-surface-500">{operator.email}</p>
                      </div>
                    </td>
                    <td className="px-4 py-4">
                      <span
                        className={`badge ${
                          operator.role === 'admin'
                            ? 'badge-error'
                            : operator.role === 'operator'
                            ? 'badge-warning'
                            : 'badge-info'
                        }`}
                      >
                        {operator.role}
                      </span>
                    </td>
                    <td className="px-4 py-4">
                      <span
                        className={`badge ${
                          operator.status === 'active' ? 'badge-success' : 'badge-error'
                        }`}
                      >
                        {operator.status}
                      </span>
                    </td>
                    <td className="px-4 py-4 text-sm text-surface-500">
                      {operator.last_login_at
                        ? new Date(operator.last_login_at).toLocaleString()
                        : 'Never'}
                    </td>
                    <td className="px-4 py-4">
                      <div className="flex justify-end gap-2">
                        <button
                          onClick={() => setEditOperator(operator)}
                          className="btn btn-secondary p-2"
                          title="Edit"
                        >
                          <Edit className="w-4 h-4" />
                        </button>
                        <button
                          onClick={() => setDeleteId(operator.id)}
                          className="btn btn-secondary p-2 text-red-600 hover:bg-red-50"
                          title="Delete"
                        >
                          <Trash2 className="w-4 h-4" />
                        </button>
                      </div>
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
              {Math.min(page * perPage, data.total)} of {data.total} operators
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

      {/* Create operator dialog */}
      <Dialog
        open={isCreateOpen}
        onClose={() => setIsCreateOpen(false)}
        className="relative z-50"
      >
        <div className="fixed inset-0 bg-black/30" aria-hidden="true" />
        <div className="fixed inset-0 flex items-center justify-center p-4">
          <Dialog.Panel className="card p-6 max-w-md w-full">
            <div className="flex items-center justify-between mb-4">
              <Dialog.Title className="text-lg font-semibold text-surface-900">
                Add Operator
              </Dialog.Title>
              <button
                onClick={() => setIsCreateOpen(false)}
                className="text-surface-400 hover:text-surface-600"
              >
                <X className="w-5 h-5" />
              </button>
            </div>

            <form onSubmit={handleCreate} className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-surface-700 mb-1">
                  Name
                </label>
                <input type="text" name="name" required className="input" />
              </div>
              <div>
                <label className="block text-sm font-medium text-surface-700 mb-1">
                  Email
                </label>
                <input type="email" name="email" required className="input" />
              </div>
              <div>
                <label className="block text-sm font-medium text-surface-700 mb-1">
                  Password
                </label>
                <input type="password" name="password" required minLength={8} className="input" />
              </div>
              <div>
                <label className="block text-sm font-medium text-surface-700 mb-1">
                  Role
                </label>
                <select name="role" required className="input">
                  <option value="viewer">Viewer</option>
                  <option value="operator">Operator</option>
                  <option value="admin">Admin</option>
                </select>
              </div>
              <div className="flex justify-end gap-3 pt-4">
                <button
                  type="button"
                  onClick={() => setIsCreateOpen(false)}
                  className="btn btn-secondary"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={createMutation.isPending}
                  className="btn btn-primary"
                >
                  Create
                </button>
              </div>
            </form>
          </Dialog.Panel>
        </div>
      </Dialog>

      {/* Edit operator dialog */}
      <Dialog
        open={!!editOperator}
        onClose={() => setEditOperator(null)}
        className="relative z-50"
      >
        <div className="fixed inset-0 bg-black/30" aria-hidden="true" />
        <div className="fixed inset-0 flex items-center justify-center p-4">
          <Dialog.Panel className="card p-6 max-w-md w-full">
            <div className="flex items-center justify-between mb-4">
              <Dialog.Title className="text-lg font-semibold text-surface-900">
                Edit Operator
              </Dialog.Title>
              <button
                onClick={() => setEditOperator(null)}
                className="text-surface-400 hover:text-surface-600"
              >
                <X className="w-5 h-5" />
              </button>
            </div>

            <form onSubmit={handleUpdate} className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-surface-700 mb-1">
                  Name
                </label>
                <input
                  type="text"
                  name="name"
                  defaultValue={editOperator?.name}
                  required
                  className="input"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-surface-700 mb-1">
                  Email
                </label>
                <input
                  type="email"
                  value={editOperator?.email || ''}
                  disabled
                  className="input bg-surface-100"
                />
              </div>
              <div>
                <label className="block text-sm font-medium text-surface-700 mb-1">
                  Role
                </label>
                <select name="role" defaultValue={editOperator?.role} required className="input">
                  <option value="viewer">Viewer</option>
                  <option value="operator">Operator</option>
                  <option value="admin">Admin</option>
                </select>
              </div>
              <div className="flex justify-end gap-3 pt-4">
                <button
                  type="button"
                  onClick={() => setEditOperator(null)}
                  className="btn btn-secondary"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={updateMutation.isPending}
                  className="btn btn-primary"
                >
                  Save
                </button>
              </div>
            </form>
          </Dialog.Panel>
        </div>
      </Dialog>

      {/* Delete confirmation dialog */}
      <Dialog
        open={!!deleteId}
        onClose={() => setDeleteId(null)}
        className="relative z-50"
      >
        <div className="fixed inset-0 bg-black/30" aria-hidden="true" />
        <div className="fixed inset-0 flex items-center justify-center p-4">
          <Dialog.Panel className="card p-6 max-w-md w-full">
            <Dialog.Title className="text-lg font-semibold text-surface-900">
              Delete Operator
            </Dialog.Title>
            <Dialog.Description className="text-surface-500 mt-2">
              Are you sure you want to delete this operator? This action cannot be undone.
            </Dialog.Description>

            <div className="mt-6 flex justify-end gap-3">
              <button
                onClick={() => setDeleteId(null)}
                className="btn btn-secondary"
              >
                Cancel
              </button>
              <button
                onClick={() => deleteId && deleteMutation.mutate(deleteId)}
                disabled={deleteMutation.isPending}
                className="btn btn-danger"
              >
                Delete
              </button>
            </div>
          </Dialog.Panel>
        </div>
      </Dialog>
    </div>
  );
}
