import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Settings as SettingsIcon, Save, AlertCircle, CheckCircle } from 'lucide-react';
import { settingsApi } from '../api/operators';
import type { SystemSettings } from '../types';

export function Settings() {
  const [successMessage, setSuccessMessage] = useState('');
  const queryClient = useQueryClient();

  const { data: settings, isLoading, error } = useQuery({
    queryKey: ['settings'],
    queryFn: settingsApi.get,
  });

  const updateMutation = useMutation({
    mutationFn: settingsApi.update,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['settings'] });
      setSuccessMessage('Settings saved successfully');
      setTimeout(() => setSuccessMessage(''), 3000);
    },
  });

  const handleSubmit = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const formData = new FormData(e.currentTarget);
    
    const updatedSettings: Partial<SystemSettings> = {
      session_lifetime_hours: parseInt(formData.get('session_lifetime_hours') as string),
      mfa_required: formData.get('mfa_required') === 'true',
      password_min_length: parseInt(formData.get('password_min_length') as string),
      max_login_attempts: parseInt(formData.get('max_login_attempts') as string),
      lockout_duration_minutes: parseInt(formData.get('lockout_duration_minutes') as string),
      allowed_domains: (formData.get('allowed_domains') as string)
        .split(',')
        .map((d) => d.trim())
        .filter(Boolean),
    };

    updateMutation.mutate(updatedSettings);
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-aegion-600"></div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="card p-6">
        <div className="flex items-center gap-3 text-red-600">
          <AlertCircle className="w-5 h-5" />
          <span>Failed to load settings</span>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-surface-900">Settings</h1>
        <p className="text-surface-500">Configure system-wide security settings</p>
      </div>

      {successMessage && (
        <div className="flex items-center gap-2 p-4 bg-green-50 border border-green-200 rounded-lg text-green-700">
          <CheckCircle className="w-5 h-5" />
          {successMessage}
        </div>
      )}

      {updateMutation.error && (
        <div className="flex items-center gap-2 p-4 bg-red-50 border border-red-200 rounded-lg text-red-700">
          <AlertCircle className="w-5 h-5" />
          Failed to save settings
        </div>
      )}

      <form onSubmit={handleSubmit} className="space-y-6">
        {/* Session Settings */}
        <div className="card p-6">
          <div className="flex items-center gap-2 mb-4">
            <SettingsIcon className="w-5 h-5 text-surface-500" />
            <h2 className="text-lg font-semibold text-surface-900">Session Settings</h2>
          </div>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            <div>
              <label className="block text-sm font-medium text-surface-700 mb-1">
                Session Lifetime (hours)
              </label>
              <input
                type="number"
                name="session_lifetime_hours"
                defaultValue={settings?.session_lifetime_hours || 24}
                min={1}
                max={720}
                className="input"
              />
              <p className="text-xs text-surface-500 mt-1">
                How long sessions remain valid (1-720 hours)
              </p>
            </div>
          </div>
        </div>

        {/* Authentication Settings */}
        <div className="card p-6">
          <h2 className="text-lg font-semibold text-surface-900 mb-4">
            Authentication Settings
          </h2>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            <div>
              <label className="block text-sm font-medium text-surface-700 mb-1">
                Require MFA
              </label>
              <select
                name="mfa_required"
                defaultValue={settings?.mfa_required ? 'true' : 'false'}
                className="input"
              >
                <option value="false">Optional</option>
                <option value="true">Required</option>
              </select>
              <p className="text-xs text-surface-500 mt-1">
                Enforce MFA for all users
              </p>
            </div>

            <div>
              <label className="block text-sm font-medium text-surface-700 mb-1">
                Minimum Password Length
              </label>
              <input
                type="number"
                name="password_min_length"
                defaultValue={settings?.password_min_length || 8}
                min={6}
                max={128}
                className="input"
              />
              <p className="text-xs text-surface-500 mt-1">
                Minimum characters required (6-128)
              </p>
            </div>

            <div>
              <label className="block text-sm font-medium text-surface-700 mb-1">
                Max Login Attempts
              </label>
              <input
                type="number"
                name="max_login_attempts"
                defaultValue={settings?.max_login_attempts || 5}
                min={1}
                max={20}
                className="input"
              />
              <p className="text-xs text-surface-500 mt-1">
                Attempts before account lockout
              </p>
            </div>

            <div>
              <label className="block text-sm font-medium text-surface-700 mb-1">
                Lockout Duration (minutes)
              </label>
              <input
                type="number"
                name="lockout_duration_minutes"
                defaultValue={settings?.lockout_duration_minutes || 15}
                min={1}
                max={1440}
                className="input"
              />
              <p className="text-xs text-surface-500 mt-1">
                How long accounts stay locked
              </p>
            </div>
          </div>
        </div>

        {/* Domain Settings */}
        <div className="card p-6">
          <h2 className="text-lg font-semibold text-surface-900 mb-4">Domain Restrictions</h2>
          <div>
            <label className="block text-sm font-medium text-surface-700 mb-1">
              Allowed Email Domains
            </label>
            <input
              type="text"
              name="allowed_domains"
              defaultValue={settings?.allowed_domains?.join(', ') || ''}
              placeholder="example.com, company.org"
              className="input"
            />
            <p className="text-xs text-surface-500 mt-1">
              Comma-separated list. Leave empty to allow all domains.
            </p>
          </div>
        </div>

        <div className="flex justify-end">
          <button
            type="submit"
            disabled={updateMutation.isPending}
            className="btn btn-primary"
          >
            <Save className="w-4 h-4 mr-2" />
            {updateMutation.isPending ? 'Saving...' : 'Save Settings'}
          </button>
        </div>
      </form>
    </div>
  );
}
