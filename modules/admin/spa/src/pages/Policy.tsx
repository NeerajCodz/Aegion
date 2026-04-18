import type { FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { AlertCircle, Trash2 } from 'lucide-react';
import { policyApi } from '../api/policy';
import { useAuth } from '../hooks/useAuth';
import { operatorHasPermission } from '../lib/permissions';

export function Policy() {
  const queryClient = useQueryClient();
  const { operator } = useAuth();
  const canUpdateConfig = operatorHasPermission(operator, 'config:update');

  const rulesQuery = useQuery({
    queryKey: ['policy', 'abac-rules'],
    queryFn: policyApi.listABACRules,
  });
  const tuplesQuery = useQuery({
    queryKey: ['policy', 'rebac-tuples'],
    queryFn: policyApi.listReBACTuples,
  });
  const namespacesQuery = useQuery({
    queryKey: ['policy', 'rebac-namespaces'],
    queryFn: policyApi.listReBACNamespaces,
  });

  const upsertRuleMutation = useMutation({
    mutationFn: policyApi.upsertABACRule,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['policy', 'abac-rules'] }),
  });
  const deleteRuleMutation = useMutation({
    mutationFn: policyApi.deleteABACRule,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['policy', 'abac-rules'] }),
  });

  const upsertTupleMutation = useMutation({
    mutationFn: policyApi.upsertReBACTuple,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['policy', 'rebac-tuples'] }),
  });
  const deleteTupleMutation = useMutation({
    mutationFn: policyApi.deleteReBACTuple,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['policy', 'rebac-tuples'] }),
  });

  const upsertNamespaceMutation = useMutation({
    mutationFn: policyApi.upsertReBACNamespace,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['policy', 'rebac-namespaces'] }),
  });
  const deleteNamespaceMutation = useMutation({
    mutationFn: policyApi.deleteReBACNamespace,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['policy', 'rebac-namespaces'] }),
  });

  const simulateMutation = useMutation({
    mutationFn: policyApi.simulate,
  });

  const anyError =
    rulesQuery.error ||
    tuplesQuery.error ||
    namespacesQuery.error ||
    upsertRuleMutation.error ||
    deleteRuleMutation.error ||
    upsertTupleMutation.error ||
    deleteTupleMutation.error ||
    upsertNamespaceMutation.error ||
    deleteNamespaceMutation.error ||
    simulateMutation.error;

  if (rulesQuery.error || tuplesQuery.error || namespacesQuery.error) {
    return (
      <div className="card p-6">
        <div className="flex items-center gap-3 text-red-600">
          <AlertCircle className="w-5 h-5" />
          <span>Failed to load policy data.</span>
        </div>
      </div>
    );
  }

  const rules = rulesQuery.data ?? [];
  const tuples = tuplesQuery.data ?? [];
  const namespaces = namespacesQuery.data ?? [];

  const handleRuleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canUpdateConfig) {
      return;
    }
    const formData = new FormData(event.currentTarget);
    upsertRuleMutation.mutate({
      id: String(formData.get('id') ?? '').trim() || undefined,
      name: String(formData.get('name') ?? '').trim(),
      description: String(formData.get('description') ?? '').trim() || undefined,
      expression: String(formData.get('expression') ?? '').trim(),
      priority: Number.parseInt(String(formData.get('priority') ?? '0'), 10) || 0,
      effect: String(formData.get('effect') ?? 'allow') === 'deny' ? 'deny' : 'allow',
      enabled: String(formData.get('enabled') ?? 'true') === 'true',
    });
    event.currentTarget.reset();
  };

  const handleTupleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canUpdateConfig) {
      return;
    }
    const formData = new FormData(event.currentTarget);
    upsertTupleMutation.mutate({
      id: String(formData.get('id') ?? '').trim() || undefined,
      namespace: String(formData.get('namespace') ?? '').trim(),
      object_id: String(formData.get('object_id') ?? '').trim(),
      relation: String(formData.get('relation') ?? '').trim(),
      subject_id: String(formData.get('subject_id') ?? '').trim(),
    });
    event.currentTarget.reset();
  };

  const handleNamespaceSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canUpdateConfig) {
      return;
    }
    const formData = new FormData(event.currentTarget);
    const rawConfig = String(formData.get('config') ?? '').trim();
    let parsedConfig: Record<string, unknown> = {};
    if (rawConfig !== '') {
      try {
        parsedConfig = JSON.parse(rawConfig) as Record<string, unknown>;
      } catch {
        parsedConfig = {};
      }
    }
    upsertNamespaceMutation.mutate({
      id: String(formData.get('id') ?? '').trim() || undefined,
      name: String(formData.get('name') ?? '').trim(),
      config: parsedConfig,
      active: String(formData.get('active') ?? 'false') === 'true',
    });
    event.currentTarget.reset();
  };

  const handleSimulateSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const formData = new FormData(event.currentTarget);
    const extraRaw = String(formData.get('extra') ?? '').trim();
    const extraEntries = extraRaw
      .split(',')
      .map((entry) => entry.trim())
      .filter(Boolean)
      .map((entry) => entry.split('='))
      .filter((parts) => parts.length === 2)
      .reduce<Record<string, string>>((acc, [key, value]) => {
        acc[key.trim()] = value.trim();
        return acc;
      }, {});

    simulateMutation.mutate({
      subject: String(formData.get('subject') ?? '').trim(),
      resource: String(formData.get('resource') ?? '').trim(),
      resource_type: String(formData.get('resource_type') ?? '').trim(),
      action: String(formData.get('action') ?? '').trim(),
      model: String(formData.get('model') ?? '').trim() || undefined,
      context: {
        ip: String(formData.get('ip') ?? '').trim() || undefined,
        tenant_id: String(formData.get('tenant_id') ?? '').trim() || undefined,
        extra: Object.keys(extraEntries).length > 0 ? extraEntries : undefined,
      },
    });
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-foreground">Policy</h1>
        <p className="text-muted-foreground">Author ABAC/ReBAC policy data and simulate authorization decisions.</p>
      </div>

      {anyError && (
        <div className="card p-4 border border-red-200 bg-red-50 text-red-700 text-sm">Failed to apply policy changes.</div>
      )}

      <div className="card overflow-hidden">
        <div className="px-4 py-3 border-b border-surface-200">
          <h2 className="text-lg font-semibold text-surface-900">Policy simulation</h2>
        </div>
        <form onSubmit={handleSimulateSubmit} className="p-4 grid grid-cols-1 md:grid-cols-3 gap-3">
          <input name="subject" required placeholder="identity UUID or user:alice" className="input" />
          <input name="resource_type" required placeholder="resource type" className="input" />
          <input name="action" required placeholder="read / write" className="input" />
          <input name="resource" placeholder="resource ID (optional)" className="input" />
          <select name="model" defaultValue="" className="input">
            <option value="">default model</option>
            <option value="rbac">rbac</option>
            <option value="abac">abac</option>
            <option value="rebac">rebac</option>
          </select>
          <input name="ip" placeholder="client IP (optional)" className="input" />
          <input name="tenant_id" placeholder="tenant id (optional)" className="input" />
          <input name="extra" placeholder="k1=v1,k2=v2" className="input md:col-span-2" />
          <div className="md:col-span-3">
            <button type="submit" className="btn btn-primary" disabled={simulateMutation.isPending}>
              Simulate
            </button>
          </div>
        </form>
        {simulateMutation.data && (
          <div className="px-4 pb-4 text-sm text-surface-700">
            <p>
              Decision:{' '}
              <span className={simulateMutation.data.allowed ? 'text-green-700 font-medium' : 'text-red-700 font-medium'}>
                {simulateMutation.data.allowed ? 'allow' : 'deny'}
              </span>
            </p>
            <p>Model: {simulateMutation.data.model_used || 'default'}</p>
            {simulateMutation.data.deny_reason && <p>Deny reason: {simulateMutation.data.deny_reason}</p>}
            <p>Eval path: {(simulateMutation.data.eval_path ?? []).join(' → ') || '-'}</p>
          </div>
        )}
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-2 gap-4">
        <div className="card overflow-hidden">
          <div className="px-4 py-3 border-b border-surface-200">
            <h2 className="text-lg font-semibold text-surface-900">ABAC rules</h2>
          </div>
          <form onSubmit={handleRuleSubmit} className="p-4 border-b border-surface-200 grid grid-cols-1 md:grid-cols-2 gap-3">
            <input
              name="id"
              placeholder="Rule ID (optional for update)"
              className="input md:col-span-2"
              disabled={!canUpdateConfig || upsertRuleMutation.isPending}
            />
            <input
              name="name"
              required
              placeholder="Rule name"
              className="input"
              disabled={!canUpdateConfig || upsertRuleMutation.isPending}
            />
            <input
              name="priority"
              type="number"
              defaultValue={100}
              className="input"
              disabled={!canUpdateConfig || upsertRuleMutation.isPending}
            />
            <select
              name="effect"
              defaultValue="allow"
              className="input"
              disabled={!canUpdateConfig || upsertRuleMutation.isPending}
            >
              <option value="allow">allow</option>
              <option value="deny">deny</option>
            </select>
            <select
              name="enabled"
              defaultValue="true"
              className="input"
              disabled={!canUpdateConfig || upsertRuleMutation.isPending}
            >
              <option value="true">enabled</option>
              <option value="false">disabled</option>
            </select>
            <input
              name="description"
              placeholder="Description"
              className="input md:col-span-2"
              disabled={!canUpdateConfig || upsertRuleMutation.isPending}
            />
            <textarea
              name="expression"
              required
              placeholder="subject['department'] == 'engineering' && action == 'read'"
              className="input min-h-[84px] md:col-span-2"
              disabled={!canUpdateConfig || upsertRuleMutation.isPending}
            />
            <div className="md:col-span-2">
              <button type="submit" className="btn btn-primary" disabled={!canUpdateConfig || upsertRuleMutation.isPending}>
                Save ABAC Rule
              </button>
            </div>
          </form>
          <div className="divide-y divide-surface-200">
            {rules.length === 0 ? (
              <p className="px-4 py-4 text-sm text-surface-500">No ABAC rules configured.</p>
            ) : (
              rules.map((rule) => (
                <div key={rule.id} className="px-4 py-3">
                  <div className="flex items-start justify-between gap-2">
                    <div>
                      <p className="text-sm font-medium text-surface-900">
                        {rule.name} ({rule.effect}) #{rule.priority}
                      </p>
                      <p className="text-xs text-surface-500">{rule.expression}</p>
                    </div>
                    <button
                      type="button"
                      className="btn btn-secondary p-2 text-red-600 hover:bg-red-50"
                      disabled={!canUpdateConfig || deleteRuleMutation.isPending}
                      onClick={() => deleteRuleMutation.mutate(rule.id)}
                      title="Delete ABAC rule"
                    >
                      <Trash2 className="w-4 h-4" />
                    </button>
                  </div>
                </div>
              ))
            )}
          </div>
        </div>

        <div className="card overflow-hidden">
          <div className="px-4 py-3 border-b border-surface-200">
            <h2 className="text-lg font-semibold text-surface-900">ReBAC tuples</h2>
          </div>
          <form onSubmit={handleTupleSubmit} className="p-4 border-b border-surface-200 grid grid-cols-1 md:grid-cols-2 gap-3">
            <input
              name="id"
              placeholder="Tuple ID (optional for update)"
              className="input md:col-span-2"
              disabled={!canUpdateConfig || upsertTupleMutation.isPending}
            />
            <input
              name="namespace"
              required
              placeholder="namespace"
              className="input"
              disabled={!canUpdateConfig || upsertTupleMutation.isPending}
            />
            <input
              name="relation"
              required
              placeholder="relation"
              className="input"
              disabled={!canUpdateConfig || upsertTupleMutation.isPending}
            />
            <input
              name="object_id"
              required
              placeholder="object ID"
              className="input"
              disabled={!canUpdateConfig || upsertTupleMutation.isPending}
            />
            <input
              name="subject_id"
              required
              placeholder="subject ID"
              className="input"
              disabled={!canUpdateConfig || upsertTupleMutation.isPending}
            />
            <div className="md:col-span-2">
              <button type="submit" className="btn btn-primary" disabled={!canUpdateConfig || upsertTupleMutation.isPending}>
                Save ReBAC Tuple
              </button>
            </div>
          </form>
          <div className="divide-y divide-surface-200 max-h-[480px] overflow-y-auto">
            {tuples.length === 0 ? (
              <p className="px-4 py-4 text-sm text-surface-500">No ReBAC tuples configured.</p>
            ) : (
              tuples.map((tuple) => (
                <div key={tuple.id} className="px-4 py-3">
                  <div className="flex items-center justify-between gap-2">
                    <p className="text-sm text-surface-900">
                      {tuple.namespace}:{tuple.object_id}#{tuple.relation} @ {tuple.subject_id}
                    </p>
                    <button
                      type="button"
                      className="btn btn-secondary p-2 text-red-600 hover:bg-red-50"
                      disabled={!canUpdateConfig || deleteTupleMutation.isPending}
                      onClick={() => deleteTupleMutation.mutate(tuple.id)}
                      title="Delete ReBAC tuple"
                    >
                      <Trash2 className="w-4 h-4" />
                    </button>
                  </div>
                </div>
              ))
            )}
          </div>
        </div>
      </div>

      <div className="card overflow-hidden">
        <div className="px-4 py-3 border-b border-surface-200">
          <h2 className="text-lg font-semibold text-surface-900">ReBAC namespaces</h2>
        </div>
        <form onSubmit={handleNamespaceSubmit} className="p-4 border-b border-surface-200 grid grid-cols-1 md:grid-cols-3 gap-3">
          <input
            name="id"
            placeholder="Namespace ID (optional for update)"
            className="input"
            disabled={!canUpdateConfig || upsertNamespaceMutation.isPending}
          />
          <input
            name="name"
            required
            placeholder="namespace name"
            className="input"
            disabled={!canUpdateConfig || upsertNamespaceMutation.isPending}
          />
          <select
            name="active"
            defaultValue="false"
            className="input"
            disabled={!canUpdateConfig || upsertNamespaceMutation.isPending}
          >
            <option value="true">active</option>
            <option value="false">inactive</option>
          </select>
          <textarea
            name="config"
            placeholder='{"relations":["owner","viewer"]}'
            className="input min-h-[80px] md:col-span-3 font-mono"
            disabled={!canUpdateConfig || upsertNamespaceMutation.isPending}
          />
          <div className="md:col-span-3">
            <button
              type="submit"
              className="btn btn-primary"
              disabled={!canUpdateConfig || upsertNamespaceMutation.isPending}
            >
              Save ReBAC Namespace
            </button>
          </div>
        </form>
        <div className="divide-y divide-surface-200">
          {namespaces.length === 0 ? (
            <p className="px-4 py-4 text-sm text-surface-500">No ReBAC namespaces configured.</p>
          ) : (
            namespaces.map((namespace) => (
              <div key={namespace.id} className="px-4 py-3">
                <div className="flex items-center justify-between gap-2">
                  <p className="text-sm text-surface-900">
                    {namespace.name} • v{namespace.version} • {namespace.active ? 'active' : 'inactive'}
                  </p>
                  <button
                    type="button"
                    className="btn btn-secondary p-2 text-red-600 hover:bg-red-50"
                    disabled={!canUpdateConfig || deleteNamespaceMutation.isPending}
                    onClick={() => deleteNamespaceMutation.mutate(namespace.id)}
                    title="Delete ReBAC namespace"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                </div>
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
