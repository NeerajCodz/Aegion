import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import {
  Users,
  Activity,
  UserPlus,
  ShieldCheck,
  TrendingUp,
  AlertCircle,
  ArrowRight,
  Settings,
  UserCog,
} from 'lucide-react';
import { dashboardApi } from '../api/operators';
import { identitiesApi } from '../api/identities';
import { sessionsApi } from '../api/sessions';

const formatRelativeTime = (iso: string): string => {
  const value = new Date(iso).getTime();
  if (Number.isNaN(value)) {
    return 'Unknown';
  }

  const deltaSeconds = Math.floor((Date.now() - value) / 1000);
  if (deltaSeconds < 60) return 'Just now';
  if (deltaSeconds < 3600) return `${Math.floor(deltaSeconds / 60)}m ago`;
  if (deltaSeconds < 86400) return `${Math.floor(deltaSeconds / 3600)}h ago`;
  return `${Math.floor(deltaSeconds / 86400)}d ago`;
};

const parseUserAgent = (userAgent: string): string => {
  if (!userAgent) return 'Unknown client';
  if (userAgent.includes('Chrome')) return 'Chrome';
  if (userAgent.includes('Firefox')) return 'Firefox';
  if (userAgent.includes('Safari')) return 'Safari';
  if (userAgent.includes('Edge')) return 'Edge';
  return 'Unknown client';
};

export function Dashboard() {
  const statsQuery = useQuery({
    queryKey: ['dashboard-stats'],
    queryFn: dashboardApi.getStats,
    staleTime: 60000,
    refetchInterval: 60000,
  });

  const recentIdentitiesQuery = useQuery({
    queryKey: ['dashboard-recent-identities'],
    queryFn: () => identitiesApi.list({ page: 1, per_page: 5 }),
    staleTime: 30000,
    refetchInterval: 60000,
  });

  const recentSessionsQuery = useQuery({
    queryKey: ['dashboard-recent-sessions'],
    queryFn: () => sessionsApi.list({ page: 1, per_page: 5 }),
    staleTime: 30000,
    refetchInterval: 60000,
  });

  const stats = statsQuery.data;
  const recentIdentities = recentIdentitiesQuery.data?.data ?? [];
  const recentSessions = recentSessionsQuery.data?.data ?? [];

  const isLoading =
    statsQuery.isLoading ||
    recentIdentitiesQuery.isLoading ||
    recentSessionsQuery.isLoading;
  const error =
    statsQuery.error ||
    recentIdentitiesQuery.error ||
    recentSessionsQuery.error;

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
          <span>Failed to load dashboard data</span>
        </div>
      </div>
    );
  }

  const mfaRate = stats?.mfa_adoption_rate ?? 0;
  const totalIdentities = stats?.total_identities ?? 0;
  const activeSessions = stats?.active_sessions ?? 0;
  const sessionPressure = totalIdentities > 0 ? activeSessions / totalIdentities : 0;
  const securityPosture =
    mfaRate >= 80 && sessionPressure <= 1.5
      ? { label: 'Strong', className: 'badge-success' }
      : mfaRate >= 50
      ? { label: 'Moderate', className: 'badge-warning' }
      : { label: 'Needs Attention', className: 'badge-error' };

  const statCards = [
    {
      name: 'Total Identities',
      value: stats?.total_identities.toLocaleString() || '0',
      icon: Users,
      color: 'bg-blue-500',
    },
    {
      name: 'Active Sessions',
      value: stats?.active_sessions.toLocaleString() || '0',
      icon: Activity,
      color: 'bg-green-500',
    },
    {
      name: 'New (24h)',
      value: stats?.identities_last_24h.toLocaleString() || '0',
      icon: UserPlus,
      color: 'bg-purple-500',
    },
    {
      name: 'MFA Adoption',
      value: `${stats?.mfa_adoption_rate.toFixed(1) || '0'}%`,
      icon: ShieldCheck,
      color: 'bg-amber-500',
    },
  ];

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-surface-900">Dashboard</h1>
        <p className="text-surface-500">Overview of your identity platform</p>
        <p className="text-xs text-surface-400 mt-1">Auto-refreshes every minute</p>
      </div>

      {/* Stats grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        {statCards.map((stat) => (
          <div key={stat.name} className="card p-6">
            <div className="flex items-center gap-4">
              <div className={`p-3 rounded-lg ${stat.color}`}>
                <stat.icon className="w-6 h-6 text-white" />
              </div>
              <div>
                <p className="text-sm text-surface-500">{stat.name}</p>
                <p className="text-2xl font-bold text-surface-900">{stat.value}</p>
              </div>
            </div>
          </div>
        ))}
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-3 gap-6">
        <div className="card p-6">
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2">
              <ShieldCheck className="w-5 h-5 text-surface-500" />
              <h2 className="text-lg font-semibold text-surface-900">Security Posture</h2>
            </div>
            <span className={`badge ${securityPosture.className}`}>{securityPosture.label}</span>
          </div>
          <div className="space-y-2 text-sm text-surface-600">
            <p>MFA adoption is <strong>{mfaRate.toFixed(1)}%</strong>.</p>
            <p>
              Active session pressure is{' '}
              <strong>{sessionPressure.toFixed(2)} sessions per identity</strong>.
            </p>
            <p className="text-xs text-surface-500">
              Use Settings and Session controls to tighten risk thresholds.
            </p>
          </div>
        </div>

        <div className="card p-6 xl:col-span-2">
          <div className="flex items-center gap-2 mb-4">
            <TrendingUp className="w-5 h-5 text-surface-500" />
            <h2 className="text-lg font-semibold text-surface-900">Quick Actions</h2>
          </div>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
            <Link to="/identities" className="btn btn-secondary justify-between">
              Review Identities
              <ArrowRight className="w-4 h-4" />
            </Link>
            <Link to="/sessions" className="btn btn-secondary justify-between">
              Review Sessions
              <ArrowRight className="w-4 h-4" />
            </Link>
            <Link to="/settings" className="btn btn-secondary justify-between">
              Update Security Settings
              <Settings className="w-4 h-4" />
            </Link>
          </div>
        </div>
      </div>

      {/* Recent activity section */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div className="card p-6">
          <div className="flex items-center gap-2 mb-4">
            <TrendingUp className="w-5 h-5 text-surface-500" />
            <h2 className="text-lg font-semibold text-surface-900">Recent Signups</h2>
          </div>
          {recentIdentities.length === 0 ? (
            <div className="text-center py-8 text-surface-400">
              <Users className="w-12 h-12 mx-auto mb-2" />
              <p>No recent signups to display</p>
            </div>
          ) : (
            <ul className="space-y-3">
              {recentIdentities.map((identity) => (
                <li
                  key={identity.id}
                  className="flex items-center justify-between border border-surface-200 rounded-lg px-3 py-2"
                >
                  <div>
                    <p className="font-medium text-surface-900">
                      {identity.display_name || identity.email}
                    </p>
                    <p className="text-xs text-surface-500">{identity.email}</p>
                  </div>
                  <div className="text-right">
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
                    <p className="text-xs text-surface-500 mt-1">
                      {formatRelativeTime(identity.created_at)}
                    </p>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>

        <div className="card p-6">
          <div className="flex items-center gap-2 mb-4">
            <Activity className="w-5 h-5 text-surface-500" />
            <h2 className="text-lg font-semibold text-surface-900">Active Sessions</h2>
          </div>
          {recentSessions.length === 0 ? (
            <div className="text-center py-8 text-surface-400">
              <Activity className="w-12 h-12 mx-auto mb-2" />
              <p>No active sessions to display</p>
            </div>
          ) : (
            <ul className="space-y-3">
              {recentSessions.map((session) => (
                <li
                  key={session.id}
                  className="flex items-center justify-between border border-surface-200 rounded-lg px-3 py-2"
                >
                  <div className="flex items-center gap-2">
                    <UserCog className="w-4 h-4 text-surface-500" />
                    <div>
                      <p className="font-medium text-surface-900">
                        {parseUserAgent(session.user_agent)}
                      </p>
                      <p className="text-xs text-surface-500">{session.ip_address || 'Unknown IP'}</p>
                    </div>
                  </div>
                  <div className="text-right">
                    <p className="text-xs text-surface-500">
                      Active {formatRelativeTime(session.last_active_at)}
                    </p>
                    <p className="text-xs text-surface-500">
                      Expires {new Date(session.expires_at).toLocaleDateString()}
                    </p>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  );
}
