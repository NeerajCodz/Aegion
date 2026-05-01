import { useQuery } from '@tanstack/react-query';
import {
  Activity,
  AlertCircle,
  ArrowRight,
  Database,
  Gauge,
  Zap,
} from 'lucide-react';
import { Link } from 'react-router-dom';

import { analyticsHealthApi, analyticsPublicDashboardsApi } from '@/api/analytics';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';

export function AnalyticsOverview() {
  const { data: health, isLoading: healthLoading } = useQuery({
    queryKey: ['analytics-health'],
    queryFn: () => analyticsHealthApi.getHealthStatus(),
    refetchInterval: 30000,
  });

  const { data: stats, isLoading: statsLoading } = useQuery({
    queryKey: ['analytics-stats'],
    queryFn: () => analyticsHealthApi.getStats(),
    refetchInterval: 60000,
  });

  const { data: dashboards, isLoading: dashboardsLoading } = useQuery({
    queryKey: ['analytics-dashboards'],
    queryFn: () => analyticsPublicDashboardsApi.listDashboards(),
  });

  const isLoading = healthLoading || statsLoading || dashboardsLoading;

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'healthy':
        return 'bg-green-500';
      case 'degraded':
        return 'bg-yellow-500';
      case 'offline':
        return 'bg-red-500';
      default:
        return 'bg-gray-500';
    }
  };

  const getStatusBadge = (status: string) => {
    switch (status) {
      case 'healthy':
        return <Badge variant="default" className="bg-green-600">Healthy</Badge>;
      case 'degraded':
        return <Badge variant="default" className="bg-yellow-600">Degraded</Badge>;
      case 'offline':
        return <Badge variant="destructive">Offline</Badge>;
      default:
        return <Badge variant="default">Unknown</Badge>;
    }
  };

  return (
    <div className="space-y-8">
      {/* Header */}
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Analytics</h1>
        <p className="text-muted-foreground mt-2">
          Monitor and manage your analytics infrastructure
        </p>
      </div>

      {/* Health Alert */}
      {health && health.overall_status === 'degraded' && (
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4" />
          <AlertTitle>System Degraded</AlertTitle>
          <AlertDescription>
            Some components are experiencing issues. Check the health status below for details.
          </AlertDescription>
        </Alert>
      )}

      {/* Quick Stats */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        {isLoading ? (
          <>
            {[1, 2, 3, 4].map((i) => (
              <Card key={i}>
                <CardContent className="pt-6">
                  <Skeleton className="h-20 w-full" />
                </CardContent>
              </Card>
            ))}
          </>
        ) : stats ? (
          <>
            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">Total Events</CardTitle>
                <Activity className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">{stats.total_events.toLocaleString()}</div>
                <p className="text-xs text-muted-foreground">
                  {stats.events_today.toLocaleString()} today
                </p>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">This Month</CardTitle>
                <Gauge className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">{stats.events_this_month.toLocaleString()}</div>
                <p className="text-xs text-muted-foreground">
                  {stats.unique_users.toLocaleString()} unique users
                </p>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">Top Event Type</CardTitle>
                <Zap className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">
                  {stats.top_event_types[0]?.type || 'N/A'}
                </div>
                <p className="text-xs text-muted-foreground">
                  {stats.top_event_types[0]?.count.toLocaleString() || 0} events
                </p>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">Dashboards</CardTitle>
                <Database className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">{dashboards?.length || 0}</div>
                <p className="text-xs text-muted-foreground">
                  {dashboards?.filter((d) => d.is_public).length || 0} public
                </p>
              </CardContent>
            </Card>
          </>
        ) : null}
      </div>

      {/* System Health */}
      <Card>
        <CardHeader>
          <CardTitle>System Health</CardTitle>
          <CardDescription>Component status and performance</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {isLoading ? (
            <Skeleton className="h-32 w-full" />
          ) : health ? (
            <>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <div className={`h-3 w-3 rounded-full ${getStatusColor(health.overall_status)}`} />
                  <span className="font-medium">Overall Status</span>
                </div>
                {getStatusBadge(health.overall_status)}
              </div>

              <div className="grid grid-cols-2 md:grid-cols-5 gap-4 mt-4">
                {Object.entries(health.components).map(([component, status]) => (
                  <div key={component} className="flex flex-col items-center gap-2 p-2">
                    <div className={`h-3 w-3 rounded-full ${getStatusColor(status as string)}`} />
                    <span className="text-xs font-medium text-center capitalize">
                      {component.replace(/_/g, ' ')}
                    </span>
                    <span className="text-xs text-muted-foreground">{status}</span>
                  </div>
                ))}
              </div>

              <p className="text-xs text-muted-foreground">
                Last checked: {new Date(health.last_check_at).toLocaleString()}
              </p>
            </>
          ) : null}
        </CardContent>
      </Card>

      {/* Quick Actions */}
      <Card>
        <CardHeader>
          <CardTitle>Quick Actions</CardTitle>
          <CardDescription>Access analytics features</CardDescription>
        </CardHeader>
        <CardContent className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <Link to="/analytics/dashboards">
            <Button variant="outline" className="w-full justify-between">
              <span>View Dashboards</span>
              <ArrowRight className="h-4 w-4" />
            </Button>
          </Link>
          <Link to="/analytics/configuration/storage">
            <Button variant="outline" className="w-full justify-between">
              <span>Configure Storage</span>
              <ArrowRight className="h-4 w-4" />
            </Button>
          </Link>
          <Link to="/analytics/events">
            <Button variant="outline" className="w-full justify-between">
              <span>View Events</span>
              <ArrowRight className="h-4 w-4" />
            </Button>
          </Link>
          <Link to="/analytics/settings/health">
            <Button variant="outline" className="w-full justify-between">
              <span>System Metrics</span>
              <ArrowRight className="h-4 w-4" />
            </Button>
          </Link>
        </CardContent>
      </Card>
    </div>
  );
}
