import { useState } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import { 
  ArrowLeft, 
  User, 
  Mail, 
  Shield, 
  Calendar, 
  Clock, 
  AlertCircle,
  Save,
  Ban,
  CheckCircle,
  RefreshCw,
  Trash2,
} from 'lucide-react';
import { Dialog } from '@headlessui/react';
import { 
  useIdentity, 
  useUpdateIdentity, 
  useSuspendIdentity, 
  useActivateIdentity,
  useResetMfa,
  useDeleteIdentity,
} from '../hooks/useIdentities';

export function IdentityDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [isDeleteOpen, setIsDeleteOpen] = useState(false);
  const [editedName, setEditedName] = useState('');
  const [isEditing, setIsEditing] = useState(false);

  const { data: identity, isLoading, error } = useIdentity(id || '');
  const updateMutation = useUpdateIdentity();
  const suspendMutation = useSuspendIdentity();
  const activateMutation = useActivateIdentity();
  const resetMfaMutation = useResetMfa();
  const deleteMutation = useDeleteIdentity();

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-aegion-600"></div>
      </div>
    );
  }

  if (error || !identity) {
    return (
      <div className="card p-6">
        <div className="flex items-center gap-3 text-red-600">
          <AlertCircle className="w-5 h-5" />
          <span>Failed to load identity</span>
        </div>
        <Link to="/identities" className="btn btn-secondary mt-4">
          Back to Identities
        </Link>
      </div>
    );
  }

  const handleSave = async () => {
    if (id && editedName) {
      await updateMutation.mutateAsync({ id, data: { display_name: editedName } });
      setIsEditing(false);
    }
  };

  const handleSuspend = async () => {
    if (id) {
      await suspendMutation.mutateAsync(id);
    }
  };

  const handleActivate = async () => {
    if (id) {
      await activateMutation.mutateAsync(id);
    }
  };

  const handleResetMfa = async () => {
    if (id) {
      await resetMfaMutation.mutateAsync(id);
    }
  };

  const handleDelete = async () => {
    if (id) {
      await deleteMutation.mutateAsync(id);
      navigate('/identities');
    }
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center gap-4">
        <Link
          to="/identities"
          className="p-2 hover:bg-surface-100 rounded-lg transition-colors"
        >
          <ArrowLeft className="w-5 h-5 text-surface-500" />
        </Link>
        <div>
          <h1 className="text-2xl font-bold text-surface-900">Identity Details</h1>
          <p className="text-surface-500">{identity.email}</p>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Main info */}
        <div className="lg:col-span-2 space-y-6">
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-surface-900 mb-4">Profile</h2>
            
            <div className="flex items-start gap-6">
              <div className="w-20 h-20 bg-surface-200 rounded-full flex items-center justify-center flex-shrink-0">
                {identity.avatar_url ? (
                  <img src={identity.avatar_url} alt="" className="w-20 h-20 rounded-full" />
                ) : (
                  <User className="w-10 h-10 text-surface-500" />
                )}
              </div>

              <div className="flex-1 space-y-4">
                <div>
                  <label className="block text-sm font-medium text-surface-500 mb-1">
                    Display Name
                  </label>
                  {isEditing ? (
                    <div className="flex gap-2">
                      <input
                        type="text"
                        value={editedName}
                        onChange={(e) => setEditedName(e.target.value)}
                        className="input flex-1"
                      />
                      <button onClick={handleSave} className="btn btn-primary">
                        <Save className="w-4 h-4" />
                      </button>
                    </div>
                  ) : (
                    <p
                      className="text-surface-900 cursor-pointer hover:text-aegion-600"
                      onClick={() => {
                        setEditedName(identity.display_name);
                        setIsEditing(true);
                      }}
                    >
                      {identity.display_name}
                    </p>
                  )}
                </div>

                <div>
                  <label className="block text-sm font-medium text-surface-500 mb-1">
                    <Mail className="w-4 h-4 inline mr-1" />
                    Email
                  </label>
                  <p className="text-surface-900">{identity.email}</p>
                </div>

                <div>
                  <label className="block text-sm font-medium text-surface-500 mb-1">
                    ID
                  </label>
                  <code className="text-sm text-surface-600 bg-surface-100 px-2 py-1 rounded">
                    {identity.id}
                  </code>
                </div>
              </div>
            </div>
          </div>

          {/* Metadata */}
          {identity.metadata && Object.keys(identity.metadata).length > 0 && (
            <div className="card p-6">
              <h2 className="text-lg font-semibold text-surface-900 mb-4">Metadata</h2>
              <pre className="text-sm text-surface-600 bg-surface-50 p-4 rounded-lg overflow-x-auto">
                {JSON.stringify(identity.metadata, null, 2)}
              </pre>
            </div>
          )}
        </div>

        {/* Sidebar info */}
        <div className="space-y-6">
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-surface-900 mb-4">Status</h2>
            <div className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-surface-500 mb-1">
                  Account Status
                </label>
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
              </div>

              <div>
                <label className="block text-sm font-medium text-surface-500 mb-1">
                  <Shield className="w-4 h-4 inline mr-1" />
                  MFA
                </label>
                <span
                  className={`badge ${
                    identity.mfa_enabled ? 'badge-success' : 'badge-warning'
                  }`}
                >
                  {identity.mfa_enabled ? 'Enabled' : 'Disabled'}
                </span>
              </div>

              <div>
                <label className="block text-sm font-medium text-surface-500 mb-1">
                  <Calendar className="w-4 h-4 inline mr-1" />
                  Created
                </label>
                <p className="text-surface-900">
                  {new Date(identity.created_at).toLocaleString()}
                </p>
              </div>

              <div>
                <label className="block text-sm font-medium text-surface-500 mb-1">
                  <Clock className="w-4 h-4 inline mr-1" />
                  Last Login
                </label>
                <p className="text-surface-900">
                  {identity.last_login_at
                    ? new Date(identity.last_login_at).toLocaleString()
                    : 'Never'}
                </p>
              </div>
            </div>
          </div>

          {/* Actions */}
          <div className="card p-6">
            <h2 className="text-lg font-semibold text-surface-900 mb-4">Actions</h2>
            <div className="space-y-2">
              {identity.status === 'active' ? (
                <button
                  onClick={handleSuspend}
                  disabled={suspendMutation.isPending}
                  className="btn btn-secondary w-full justify-start"
                >
                  <Ban className="w-4 h-4 mr-2" />
                  Suspend Identity
                </button>
              ) : (
                <button
                  onClick={handleActivate}
                  disabled={activateMutation.isPending}
                  className="btn btn-secondary w-full justify-start"
                >
                  <CheckCircle className="w-4 h-4 mr-2" />
                  Activate Identity
                </button>
              )}

              {identity.mfa_enabled && (
                <button
                  onClick={handleResetMfa}
                  disabled={resetMfaMutation.isPending}
                  className="btn btn-secondary w-full justify-start"
                >
                  <RefreshCw className="w-4 h-4 mr-2" />
                  Reset MFA
                </button>
              )}

              <button
                onClick={() => setIsDeleteOpen(true)}
                className="btn btn-danger w-full justify-start"
              >
                <Trash2 className="w-4 h-4 mr-2" />
                Delete Identity
              </button>
            </div>
          </div>
        </div>
      </div>

      {/* Delete confirmation dialog */}
      <Dialog
        open={isDeleteOpen}
        onClose={() => setIsDeleteOpen(false)}
        className="relative z-50"
      >
        <div className="fixed inset-0 bg-black/30" aria-hidden="true" />
        <div className="fixed inset-0 flex items-center justify-center p-4">
          <Dialog.Panel className="card p-6 max-w-md w-full">
            <Dialog.Title className="text-lg font-semibold text-surface-900">
              Delete Identity
            </Dialog.Title>
            <Dialog.Description className="text-surface-500 mt-2">
              Are you sure you want to delete this identity? This action cannot be undone.
            </Dialog.Description>

            <div className="mt-6 flex justify-end gap-3">
              <button
                onClick={() => setIsDeleteOpen(false)}
                className="btn btn-secondary"
              >
                Cancel
              </button>
              <button
                onClick={handleDelete}
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
