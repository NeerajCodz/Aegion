import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Dialog } from '@headlessui/react';
import { AlertCircle, Edit, Plus, Shield, Trash2, X } from 'lucide-react';
import { rolesApi } from '../api/operators';
import type { Role } from '../types';
import { Badge } from '@/components/ui/badge';
import { useAuth } from '../hooks/useAuth';
import { operatorHasPermission } from '../lib/permissions';

type RoleFormState = {
  name: string;
  description: string;
  permissions: string[];
};

const EMPTY_ROLE_FORM: RoleFormState = {
  name: '',
  description: '',
  permissions: [],
};

function togglePermission(current: string[], permission: string): string[] {
  if (current.includes(permission)) {
    return current.filter((it) => it !== permission);
  }
  return [...current, permission].sort();
}

export function Roles() {
  const [page, setPage] = useState(1);
  const [createOpen, setCreateOpen] = useState(false);
  const [editRole, setEditRole] = useState<Role | null>(null);
  const [deleteRoleName, setDeleteRoleName] = useState<string | null>(null);
  const [form, setForm] = useState<RoleFormState>(EMPTY_ROLE_FORM);
  const perPage = 50;
  const queryClient = useQueryClient();
  const { operator } = useAuth();

  const { data: rolesData, isLoading, error } = useQuery({
    queryKey: ['roles', page],
    queryFn: () => rolesApi.list(page, perPage),
  });

  const permissionsQuery = useQuery({
    queryKey: ['roles', 'permissions'],
    queryFn: rolesApi.listPermissions,
  });

  const createMutation = useMutation({
    mutationFn: rolesApi.create,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['roles'] });
      setCreateOpen(false);
      setForm(EMPTY_ROLE_FORM);
    },
  });

  const updateMutation = useMutation({
    mutationFn: ({ name, payload }: { name: string; payload: { description?: string; permissions?: string[] } }) =>
      rolesApi.update(name, payload),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['roles'] });
      setEditRole(null);
      setForm(EMPTY_ROLE_FORM);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: rolesApi.delete,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['roles'] });
      queryClient.invalidateQueries({ queryKey: ['operators'] });
      setDeleteRoleName(null);
    },
  });

  const roleRows = rolesData?.data ?? [];
  const permissionCatalog = permissionsQuery.data ?? [];

  const editing = editRole !== null;
  const canCreateRole = operatorHasPermission(operator, 'roles:create');
  const canUpdateRole = operatorHasPermission(operator, 'roles:update');
  const canDeleteRole = operatorHasPermission(operator, 'roles:delete');

  const submitting = createMutation.isPending || updateMutation.isPending;

  const submitForm = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (editing && editRole) {
      if (!canUpdateRole) {
        return;
      }
      updateMutation.mutate({
        name: editRole.name,
        payload: {
          description: form.description,
          permissions: form.permissions,
        },
      });
      return;
    }
    if (!canCreateRole) {
      return;
    }
    createMutation.mutate({
      name: form.name,
      description: form.description,
      permissions: form.permissions,
    });
  };

  if (error) {
    return (
      <div className="card p-6">
        <div className="flex items-center gap-3 text-red-600">
          <AlertCircle className="w-5 h-5" />
          <span>Failed to load roles</span>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-surface-900">Roles</h1>
          <p className="text-surface-500">Create custom roles and assign granular permissions</p>
        </div>
        {canCreateRole && (
          <button
            onClick={() => {
              setEditRole(null);
              setForm(EMPTY_ROLE_FORM);
              setCreateOpen(true);
            }}
            className="btn btn-primary"
          >
            <Plus className="w-4 h-4 mr-2" />
            New Role
          </button>
        )}
      </div>

      <div className="card overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead className="bg-surface-50 border-b border-surface-200">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Role
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Description
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Permissions
                </th>
                <th className="px-4 py-3 text-right text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-surface-200">
              {isLoading ? (
                <tr>
                  <td colSpan={4} className="px-4 py-8 text-center">
                    <div className="animate-spin rounded-full h-6 w-6 border-b-2 border-aegion-600 mx-auto"></div>
                  </td>
                </tr>
              ) : roleRows.length === 0 ? (
                <tr>
                  <td colSpan={4} className="px-4 py-8 text-center text-surface-400">
                    <Shield className="w-12 h-12 mx-auto mb-2" />
                    <p>No roles found</p>
                  </td>
                </tr>
              ) : (
                roleRows.map((role) => (
                  <tr key={role.id} className="hover:bg-surface-50 transition-colors">
                    <td className="px-4 py-4">
                      <div className="flex items-center gap-2">
                        <span className="font-medium text-surface-900">{role.name}</span>
                        {role.is_system ? <Badge variant="secondary">system</Badge> : <Badge variant="outline">custom</Badge>}
                      </div>
                    </td>
                    <td className="px-4 py-4 text-sm text-surface-600">{role.description || '—'}</td>
                    <td className="px-4 py-4 text-sm text-surface-600">
                      {role.permissions?.length ?? 0} permission(s)
                    </td>
                    <td className="px-4 py-4">
                      <div className="flex justify-end gap-2">
                        <button
                          onClick={() => {
                            setCreateOpen(false);
                            setEditRole(role);
                            setForm({
                              name: role.name,
                              description: role.description ?? '',
                              permissions: [...(role.permissions ?? [])].sort(),
                            });
                          }}
                          disabled={role.is_system || !canUpdateRole}
                          className="btn btn-secondary p-2 disabled:opacity-50"
                          title={role.is_system ? 'System roles cannot be edited' : !canUpdateRole ? 'Missing roles:update permission' : 'Edit role'}
                        >
                          <Edit className="w-4 h-4" />
                        </button>
                        <button
                          onClick={() => setDeleteRoleName(role.name)}
                          disabled={role.is_system || !canDeleteRole}
                          className="btn btn-secondary p-2 text-red-600 hover:bg-red-50 disabled:opacity-50"
                          title={role.is_system ? 'System roles cannot be deleted' : !canDeleteRole ? 'Missing roles:delete permission' : 'Delete role'}
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

        {rolesData && rolesData.total_pages > 1 && (
          <div className="flex items-center justify-between px-4 py-3 border-t border-surface-200">
            <p className="text-sm text-surface-500">
              Showing {(page - 1) * perPage + 1} to {Math.min(page * perPage, rolesData.total)} of {rolesData.total} roles
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
                onClick={() => setPage((current) => Math.min(rolesData.total_pages, current + 1))}
                disabled={page >= rolesData.total_pages}
                className="btn btn-secondary"
              >
                Next
              </button>
            </div>
          </div>
        )}
      </div>

      <Dialog
        open={createOpen || editing}
        onClose={() => {
          setCreateOpen(false);
          setEditRole(null);
          setForm(EMPTY_ROLE_FORM);
        }}
        className="relative z-50"
      >
        <div className="fixed inset-0 bg-black/30" aria-hidden="true" />
        <div className="fixed inset-0 flex items-center justify-center p-4">
          <Dialog.Panel className="card p-6 max-w-3xl w-full max-h-[90vh] overflow-y-auto">
            <div className="flex items-center justify-between mb-4">
              <Dialog.Title className="text-lg font-semibold text-surface-900">
                {editing ? `Edit role: ${editRole?.name}` : 'Create role'}
              </Dialog.Title>
              <button
                onClick={() => {
                  setCreateOpen(false);
                  setEditRole(null);
                  setForm(EMPTY_ROLE_FORM);
                }}
                className="text-surface-400 hover:text-surface-600"
              >
                <X className="w-5 h-5" />
              </button>
            </div>

            <form onSubmit={submitForm} className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-surface-700 mb-1">Role name</label>
                <input
                  type="text"
                  name="name"
                  value={form.name}
                  onChange={(event) => setForm((previous) => ({ ...previous, name: event.target.value }))}
                  required
                  disabled={editing}
                  pattern="[a-z][a-z0-9_]{2,31}"
                  className="input"
                />
                <p className="text-xs text-surface-500 mt-1">Use lowercase letters, numbers, and underscores.</p>
              </div>

              <div>
                <label className="block text-sm font-medium text-surface-700 mb-1">Description</label>
                <input
                  type="text"
                  name="description"
                  value={form.description}
                  onChange={(event) => setForm((previous) => ({ ...previous, description: event.target.value }))}
                  className="input"
                />
              </div>

              <div>
                <label className="block text-sm font-medium text-surface-700 mb-2">Permissions</label>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-2 border border-surface-200 rounded-lg p-3 bg-surface-50 max-h-80 overflow-y-auto">
                  {permissionCatalog.map((permission) => (
                    <label key={permission} className="flex items-center gap-2 text-sm text-surface-700">
                      <input
                        type="checkbox"
                        checked={form.permissions.includes(permission)}
                        onChange={() =>
                          setForm((previous) => ({
                            ...previous,
                            permissions: togglePermission(previous.permissions, permission),
                          }))
                        }
                      />
                      <span>{permission}</span>
                    </label>
                  ))}
                </div>
              </div>

              {(createMutation.error || updateMutation.error) && (
                <div className="text-sm text-red-600">Unable to save role. Check the role name and permissions.</div>
              )}

              <div className="flex justify-end gap-3 pt-4">
                <button
                  type="button"
                  onClick={() => {
                    setCreateOpen(false);
                    setEditRole(null);
                    setForm(EMPTY_ROLE_FORM);
                  }}
                  className="btn btn-secondary"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={submitting || (editing ? !canUpdateRole : !canCreateRole)}
                  className="btn btn-primary"
                >
                  {editing ? 'Save changes' : 'Create role'}
                </button>
              </div>
            </form>
          </Dialog.Panel>
        </div>
      </Dialog>

      <Dialog
        open={!!deleteRoleName}
        onClose={() => setDeleteRoleName(null)}
        className="relative z-50"
      >
        <div className="fixed inset-0 bg-black/30" aria-hidden="true" />
        <div className="fixed inset-0 flex items-center justify-center p-4">
          <Dialog.Panel className="card p-6 max-w-md w-full">
            <Dialog.Title className="text-lg font-semibold text-surface-900">Delete role</Dialog.Title>
            <Dialog.Description className="text-surface-500 mt-2">
              This removes <span className="font-medium">{deleteRoleName}</span>. The role must not be assigned to any operator.
            </Dialog.Description>

            <div className="mt-6 flex justify-end gap-3">
              <button onClick={() => setDeleteRoleName(null)} className="btn btn-secondary">
                Cancel
              </button>
              <button
                onClick={() => deleteRoleName && deleteMutation.mutate(deleteRoleName)}
                disabled={deleteMutation.isPending || !canDeleteRole}
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
