import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Dialog } from '@headlessui/react';
import { AlertCircle, CheckCircle, GitBranch, KeyRound, Pencil, Plus, Trash2, X } from 'lucide-react';
import { scimApi } from '../api/scim';
import type { SCIMMapping } from '../types';
import { useAuth } from '../hooks/useAuth';
import { operatorHasPermission } from '../lib/permissions';
import { Alert, AlertDescription } from '@/components/ui/alert';

type EditableMapping = Omit<SCIMMapping, 'id' | 'created_at' | 'updated_at'>;

const defaultMapping: EditableMapping = {
  name: '',
  description: '',
  username_source: 'email',
  username_custom: '',
  email_source: 'primary',
  name_mapping: {},
  attribute_mapping: {},
  group_mapping: {},
};

function mapToJson(value: Record<string, string>): string {
  return JSON.stringify(value ?? {}, null, 2);
}

function parseJsonMap(raw: string): Record<string, string> {
  const trimmed = raw.trim();
  if (trimmed === '') {
    return {};
  }
  const parsed = JSON.parse(trimmed) as unknown;
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('Mapping JSON must be an object');
  }
  return Object.entries(parsed as Record<string, unknown>).reduce<Record<string, string>>((acc, [key, value]) => {
    if (typeof value === 'string') {
      acc[key] = value;
    }
    return acc;
  }, {});
}

export function SCIM() {
  const queryClient = useQueryClient();
  const { operator } = useAuth();
  const canUpdate = operatorHasPermission(operator, 'config:update');

  const [successMessage, setSuccessMessage] = useState('');
  const [plainToken, setPlainToken] = useState('');
  const [mappingError, setMappingError] = useState('');

  const [editMapping, setEditMapping] = useState<SCIMMapping | null>(null);
  const [editNameMap, setEditNameMap] = useState('{}');
  const [editAttributeMap, setEditAttributeMap] = useState('{}');
  const [editGroupMap, setEditGroupMap] = useState('{}');

  const { data: tokens = [], isLoading: tokensLoading } = useQuery({
    queryKey: ['scim', 'tokens'],
    queryFn: scimApi.listTokens,
  });

  const { data: mappings = [], isLoading: mappingsLoading } = useQuery({
    queryKey: ['scim', 'mappings'],
    queryFn: scimApi.listMappings,
  });

  const createTokenMutation = useMutation({
    mutationFn: scimApi.createToken,
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: ['scim', 'tokens'] });
      setPlainToken(result.plain_token);
      setSuccessMessage('SCIM token created successfully.');
    },
  });

  const deleteTokenMutation = useMutation({
    mutationFn: scimApi.deleteToken,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['scim', 'tokens'] });
      setSuccessMessage('SCIM token deleted.');
    },
  });

  const createMappingMutation = useMutation({
    mutationFn: scimApi.createMapping,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['scim', 'mappings'] });
      setSuccessMessage('SCIM mapping created.');
      setMappingError('');
    },
  });

  const updateMappingMutation = useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: EditableMapping }) => scimApi.updateMapping(id, payload),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['scim', 'mappings'] });
      setEditMapping(null);
      setSuccessMessage('SCIM mapping updated.');
      setMappingError('');
    },
  });

  const deleteMappingMutation = useMutation({
    mutationFn: scimApi.deleteMapping,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['scim', 'mappings'] });
      setSuccessMessage('SCIM mapping deleted.');
    },
  });

  const savingDisabled = useMemo(
    () =>
      !canUpdate ||
      createTokenMutation.isPending ||
      createMappingMutation.isPending ||
      updateMappingMutation.isPending,
    [canUpdate, createMappingMutation.isPending, createTokenMutation.isPending, updateMappingMutation.isPending]
  );

  const handleCreateToken = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!canUpdate) return;
    const formData = new FormData(e.currentTarget);
    const permissions = String(formData.get('permissions') ?? '')
      .split(',')
      .map((entry) => entry.trim())
      .filter(Boolean);
    createTokenMutation.mutate({
      name: String(formData.get('name') ?? '').trim(),
      description: String(formData.get('description') ?? '').trim(),
      permissions,
      expires_at: String(formData.get('expires_at') ?? '').trim() || undefined,
    });
    e.currentTarget.reset();
  };

  const handleCreateMapping = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!canUpdate) return;
    const formData = new FormData(e.currentTarget);
    try {
      const nameMap = parseJsonMap(String(formData.get('name_mapping_json') ?? '{}'));
      const attributeMap = parseJsonMap(String(formData.get('attribute_mapping_json') ?? '{}'));
      const groupMap = parseJsonMap(String(formData.get('group_mapping_json') ?? '{}'));
      createMappingMutation.mutate({
        ...defaultMapping,
        name: String(formData.get('name') ?? '').trim(),
        description: String(formData.get('description') ?? '').trim(),
        username_source: String(formData.get('username_source') ?? 'email').trim() || 'email',
        username_custom: String(formData.get('username_custom') ?? '').trim(),
        email_source: String(formData.get('email_source') ?? 'primary').trim() || 'primary',
        name_mapping: nameMap,
        attribute_mapping: attributeMap,
        group_mapping: groupMap,
      });
      setMappingError('');
      e.currentTarget.reset();
    } catch (error) {
      setMappingError(error instanceof Error ? error.message : 'Invalid mapping JSON');
    }
  };

  const startEdit = (mapping: SCIMMapping) => {
    setEditMapping(mapping);
    setEditNameMap(mapToJson(mapping.name_mapping));
    setEditAttributeMap(mapToJson(mapping.attribute_mapping));
    setEditGroupMap(mapToJson(mapping.group_mapping));
    setMappingError('');
  };

  const handleEditSubmit = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (!editMapping || !canUpdate) return;
    const formData = new FormData(e.currentTarget);
    try {
      updateMappingMutation.mutate({
        id: editMapping.id,
        payload: {
          name: String(formData.get('name') ?? '').trim(),
          description: String(formData.get('description') ?? '').trim(),
          username_source: String(formData.get('username_source') ?? 'email').trim() || 'email',
          username_custom: String(formData.get('username_custom') ?? '').trim(),
          email_source: String(formData.get('email_source') ?? 'primary').trim() || 'primary',
          name_mapping: parseJsonMap(editNameMap),
          attribute_mapping: parseJsonMap(editAttributeMap),
          group_mapping: parseJsonMap(editGroupMap),
        },
      });
    } catch (error) {
      setMappingError(error instanceof Error ? error.message : 'Invalid mapping JSON');
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-foreground">SCIM</h1>
        <p className="text-muted-foreground">Manage provisioning tokens and attribute mappings.</p>
      </div>

      {successMessage && (
        <Alert variant="success">
          <CheckCircle className="w-5 h-5" />
          <AlertDescription>{successMessage}</AlertDescription>
        </Alert>
      )}

      {(createTokenMutation.error || createMappingMutation.error || updateMappingMutation.error) && (
        <Alert variant="destructive">
          <AlertCircle className="w-5 h-5" />
          <AlertDescription>Failed to save SCIM configuration.</AlertDescription>
        </Alert>
      )}

      {mappingError && (
        <Alert variant="warning">
          <AlertCircle className="w-5 h-5" />
          <AlertDescription>{mappingError}</AlertDescription>
        </Alert>
      )}

      {plainToken && (
        <Alert variant="warning">
          <AlertCircle className="w-5 h-5" />
          <AlertDescription>
            Copy this token now (it will not be shown again): <code className="ml-1">{plainToken}</code>
          </AlertDescription>
        </Alert>
      )}

      <div className="card p-6">
        <div className="flex items-center gap-2 mb-4">
          <KeyRound className="w-5 h-5 text-surface-500" />
          <h2 className="text-lg font-semibold text-surface-900">Provisioning Tokens</h2>
        </div>

        <form onSubmit={handleCreateToken} className="grid grid-cols-1 md:grid-cols-4 gap-3 mb-4">
          <input name="name" required placeholder="Token name" className="input" disabled={savingDisabled} />
          <input name="description" placeholder="Description" className="input" disabled={savingDisabled} />
          <input
            name="permissions"
            placeholder="users:read,users:write,groups:read"
            className="input"
            disabled={savingDisabled}
          />
          <input name="expires_at" type="datetime-local" className="input" disabled={savingDisabled} />
          <div className="md:col-span-4">
            <button type="submit" className="btn btn-primary" disabled={savingDisabled}>
              <Plus className="w-4 h-4 mr-2" />
              Create Token
            </button>
          </div>
        </form>

        <div className="overflow-x-auto">
          <table className="w-full">
            <thead className="bg-surface-50 border-b border-surface-200">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Name
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Prefix
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
              {tokensLoading ? (
                <tr>
                  <td colSpan={4} className="px-4 py-6 text-center text-surface-500">
                    Loading tokens...
                  </td>
                </tr>
              ) : tokens.length === 0 ? (
                <tr>
                  <td colSpan={4} className="px-4 py-6 text-center text-surface-500">
                    No SCIM tokens configured.
                  </td>
                </tr>
              ) : (
                tokens.map((token) => (
                  <tr key={token.id}>
                    <td className="px-4 py-3 text-sm text-surface-900">{token.name}</td>
                    <td className="px-4 py-3 text-sm text-surface-500">{token.prefix}</td>
                    <td className="px-4 py-3 text-sm text-surface-500">{token.permissions.join(', ') || '-'}</td>
                    <td className="px-4 py-3 text-right">
                      <button
                        className="btn btn-secondary p-2 text-red-600 hover:bg-red-50"
                        title="Delete token"
                        disabled={!canUpdate || deleteTokenMutation.isPending}
                        onClick={() => deleteTokenMutation.mutate(token.id)}
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

      <div className="card p-6">
        <div className="flex items-center gap-2 mb-4">
          <GitBranch className="w-5 h-5 text-surface-500" />
          <h2 className="text-lg font-semibold text-surface-900">Attribute Mappings</h2>
        </div>

        <form onSubmit={handleCreateMapping} className="grid grid-cols-1 md:grid-cols-2 gap-3 mb-4">
          <input name="name" required placeholder="Mapping name" className="input" disabled={savingDisabled} />
          <input name="description" placeholder="Description" className="input" disabled={savingDisabled} />
          <input
            name="username_source"
            defaultValue="email"
            placeholder="username_source (e.g. email)"
            className="input"
            disabled={savingDisabled}
          />
          <input
            name="email_source"
            defaultValue="primary"
            placeholder="email_source (e.g. primary)"
            className="input"
            disabled={savingDisabled}
          />
          <input name="username_custom" placeholder="username_custom" className="input" disabled={savingDisabled} />
          <div />
          <textarea
            name="name_mapping_json"
            rows={4}
            defaultValue="{}"
            className="input font-mono"
            placeholder='{"givenName":"first_name"}'
            disabled={savingDisabled}
          />
          <textarea
            name="attribute_mapping_json"
            rows={4}
            defaultValue="{}"
            className="input font-mono"
            placeholder='{"department":"traits.department"}'
            disabled={savingDisabled}
          />
          <textarea
            name="group_mapping_json"
            rows={4}
            defaultValue="{}"
            className="input font-mono md:col-span-2"
            placeholder='{"Engineering":"role:operator"}'
            disabled={savingDisabled}
          />
          <div className="md:col-span-2">
            <button type="submit" className="btn btn-primary" disabled={savingDisabled}>
              <Plus className="w-4 h-4 mr-2" />
              Create Mapping
            </button>
          </div>
        </form>

        <div className="overflow-x-auto">
          <table className="w-full">
            <thead className="bg-surface-50 border-b border-surface-200">
              <tr>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Name
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Username Source
                </th>
                <th className="px-4 py-3 text-left text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Email Source
                </th>
                <th className="px-4 py-3 text-right text-xs font-medium text-surface-500 uppercase tracking-wider">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-surface-200">
              {mappingsLoading ? (
                <tr>
                  <td colSpan={4} className="px-4 py-6 text-center text-surface-500">
                    Loading mappings...
                  </td>
                </tr>
              ) : mappings.length === 0 ? (
                <tr>
                  <td colSpan={4} className="px-4 py-6 text-center text-surface-500">
                    No SCIM mappings configured.
                  </td>
                </tr>
              ) : (
                mappings.map((mapping) => (
                  <tr key={mapping.id}>
                    <td className="px-4 py-3 text-sm text-surface-900">{mapping.name}</td>
                    <td className="px-4 py-3 text-sm text-surface-500">{mapping.username_source}</td>
                    <td className="px-4 py-3 text-sm text-surface-500">{mapping.email_source}</td>
                    <td className="px-4 py-3 text-right">
                      <div className="flex justify-end gap-2">
                        <button
                          className="btn btn-secondary p-2"
                          title="Edit mapping"
                          disabled={!canUpdate}
                          onClick={() => startEdit(mapping)}
                        >
                          <Pencil className="w-4 h-4" />
                        </button>
                        <button
                          className="btn btn-secondary p-2 text-red-600 hover:bg-red-50"
                          title="Delete mapping"
                          disabled={!canUpdate || deleteMappingMutation.isPending}
                          onClick={() => deleteMappingMutation.mutate(mapping.id)}
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
      </div>

      <Dialog open={Boolean(editMapping)} onClose={() => setEditMapping(null)} className="relative z-50">
        <div className="fixed inset-0 bg-black/30" aria-hidden="true" />
        <div className="fixed inset-0 flex items-center justify-center p-4">
          <Dialog.Panel className="card p-6 max-w-3xl w-full">
            <div className="flex items-center justify-between mb-4">
              <Dialog.Title className="text-lg font-semibold text-surface-900">Edit SCIM Mapping</Dialog.Title>
              <button onClick={() => setEditMapping(null)} className="text-surface-400 hover:text-surface-600">
                <X className="w-5 h-5" />
              </button>
            </div>

            {editMapping && (
              <form onSubmit={handleEditSubmit} className="grid grid-cols-1 md:grid-cols-2 gap-3">
                <input name="name" defaultValue={editMapping.name} required className="input" />
                <input name="description" defaultValue={editMapping.description} className="input" />
                <input name="username_source" defaultValue={editMapping.username_source} className="input" />
                <input name="email_source" defaultValue={editMapping.email_source} className="input" />
                <input name="username_custom" defaultValue={editMapping.username_custom ?? ''} className="input" />
                <div />
                <textarea
                  rows={5}
                  className="input font-mono"
                  value={editNameMap}
                  onChange={(e) => setEditNameMap(e.target.value)}
                />
                <textarea
                  rows={5}
                  className="input font-mono"
                  value={editAttributeMap}
                  onChange={(e) => setEditAttributeMap(e.target.value)}
                />
                <textarea
                  rows={5}
                  className="input font-mono md:col-span-2"
                  value={editGroupMap}
                  onChange={(e) => setEditGroupMap(e.target.value)}
                />
                <div className="md:col-span-2 flex justify-end gap-2">
                  <button type="button" className="btn btn-secondary" onClick={() => setEditMapping(null)}>
                    Cancel
                  </button>
                  <button type="submit" className="btn btn-primary" disabled={savingDisabled}>
                    Save Mapping
                  </button>
                </div>
              </form>
            )}
          </Dialog.Panel>
        </div>
      </Dialog>
    </div>
  );
}
