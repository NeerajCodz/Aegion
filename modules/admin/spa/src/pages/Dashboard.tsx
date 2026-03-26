import { useQuery } from '@tanstack/react-query';
import { Users, Activity, UserPlus, ShieldCheck, TrendingUp, AlertCircle } from 'lucide-react';
import { dashboardApi } from '../api/operators';

export function Dashboard() {
  const { data: stats, isLoading, error } = useQuery({
    queryKey: ['dashboard-stats'],
    queryFn: dashboardApi.getStats,
    staleTime: 60000,
    refetchInterval: 60000,
  });

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

      {/* Recent activity section */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div className="card p-6">
          <div className="flex items-center gap-2 mb-4">
            <TrendingUp className="w-5 h-5 text-surface-500" />
            <h2 className="text-lg font-semibold text-surface-900">Recent Signups</h2>
          </div>
          <div className="text-center py-8 text-surface-400">
            <Users className="w-12 h-12 mx-auto mb-2" />
            <p>No recent signups to display</p>
          </div>
        </div>

        <div className="card p-6">
          <div className="flex items-center gap-2 mb-4">
            <Activity className="w-5 h-5 text-surface-500" />
            <h2 className="text-lg font-semibold text-surface-900">Active Sessions</h2>
          </div>
          <div className="text-center py-8 text-surface-400">
            <Activity className="w-12 h-12 mx-auto mb-2" />
            <p>No active sessions to display</p>
          </div>
        </div>
      </div>
    </div>
  );
}
