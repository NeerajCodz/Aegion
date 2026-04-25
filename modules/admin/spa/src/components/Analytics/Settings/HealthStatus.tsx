import { useQuery } from '@tanstack/react-query';
import { TrendingUp } from 'lucide-react';

import { analyticsHealthApi } from '@/api/analytics';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Progress } from '@/components/ui/progress';
import { Skeleton } from '@/components/ui/skeleton';

export function HealthStatus() {
  const { data: health, isLoading } = useQuery({
    queryKey: ['health-status'],
    queryFn: () => analyticsHealthApi.getHealthStatus(),
    refetchInterval: 30000,
  });

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
        return <Badge className="bg-green-600">Healthy</Badge>;
      case 'degraded':
        return <Badge className="bg-yellow-600">Degraded</Badge>;
      case 'offline':
        return <Badge variant="destructive">Offline</Badge>;
      default:
        return <Badge>Unknown</Badge>;
    }
  };

  if (isLoading) {
    return <Skeleton className="h-96 w-full" />;
  }

  if (!health) {
    return <div>No health data available</div>;
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">System Health</h1>
        <p className="text-muted-foreground">Monitor component status and performance metrics</p>
      </div>

      {/* Overall Status */}
      <Card>
        <CardHeader>
          <CardTitle>Overall System Status</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <div className={`h-4 w-4 rounded-full ${getStatusColor(health.overall_status)}`} />
              <span className="text-lg font-semibold capitalize">{health.overall_status}</span>
            </div>
            {getStatusBadge(health.overall_status)}
          </div>
          <p className="text-sm text-muted-foreground">
            Last checked: {new Date(health.last_check_at).toLocaleString()}
          </p>
        </CardContent>
      </Card>

      {/* Component Status Grid */}
      <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-5 gap-4">
        {Object.entries(health.components).map(([component, status]) => (
          <Card key={component}>
            <CardContent className="pt-6 text-center">
              <div className={`h-3 w-3 rounded-full ${getStatusColor(status as string)} mx-auto mb-3`} />
              <p className="text-sm font-medium capitalize mb-1">
                {component.replace(/_/g, ' ')}
              </p>
              <Badge variant="outline" className="text-xs">
                {status}
              </Badge>
            </CardContent>
          </Card>
        ))}
      </div>

      {/* Metrics */}
      {health.metrics && (
        <>
          {health.metrics.api_latency && (
            <Card>
              <CardHeader>
                <CardTitle>API Latency</CardTitle>
                <CardDescription>Response time percentiles</CardDescription>
              </CardHeader>
              <CardContent>
                <div className="space-y-4">
                  {health.metrics.api_latency.map((metric, idx) => (
                    <div key={idx} className="flex items-center justify-between">
                      <span className="text-sm">{metric.p50_ms}ms (p50)</span>
                      <div className="flex-1 mx-4 bg-muted rounded h-2" />
                      <span className="text-sm">{metric.p99_ms}ms (p99)</span>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          )}

          {health.metrics.storage_tiers && (
            <Card>
              <CardHeader>
                <CardTitle>Storage Tier Usage</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                {health.metrics.storage_tiers.map((tier) => (
                  <div key={tier.tier}>
                    <div className="flex justify-between mb-2">
                      <span className="text-sm font-medium capitalize">{tier.tier} Tier</span>
                      <span className="text-sm text-muted-foreground">
                        {((tier.usage_bytes / tier.capacity_bytes) * 100).toFixed(1)}%
                      </span>
                    </div>
                    <Progress
                      value={(tier.usage_bytes / tier.capacity_bytes) * 100}
                      className="h-2"
                    />
                  </div>
                ))}
              </CardContent>
            </Card>
          )}

          {health.metrics.webhook_delivery_success_rate !== undefined && (
            <Card>
              <CardHeader>
                <CardTitle>Webhook Delivery</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="flex items-center justify-between">
                  <span className="text-sm font-medium">Success Rate</span>
                  <div className="flex items-center gap-2">
                    <Progress
                      value={health.metrics.webhook_delivery_success_rate}
                      className="h-2 w-32"
                    />
                    <span className="text-sm font-bold">
                      {health.metrics.webhook_delivery_success_rate.toFixed(1)}%
                    </span>
                  </div>
                </div>
              </CardContent>
            </Card>
          )}

          {health.metrics.sync_lag_seconds !== undefined && (
            <Card>
              <CardHeader>
                <CardTitle>Sync Performance</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="space-y-2">
                  <div className="flex justify-between">
                    <span className="text-sm font-medium">Current Lag</span>
                    <span className="text-sm font-bold">
                      {health.metrics.sync_lag_seconds}s
                    </span>
                  </div>
                  {health.metrics.sync_lag_seconds > 60 && (
                    <div className="flex items-center gap-2 text-sm text-orange-600">
                      <TrendingUp className="w-4 h-4" />
                      Lag is increasing
                    </div>
                  )}
                </div>
              </CardContent>
            </Card>
          )}

          {/* Resource Usage */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            {health.metrics.cpu_usage_percent !== undefined && (
              <Card>
                <CardHeader>
                  <CardTitle className="text-lg">CPU Usage</CardTitle>
                </CardHeader>
                <CardContent>
                  <div className="text-3xl font-bold">
                    {health.metrics.cpu_usage_percent.toFixed(1)}%
                  </div>
                  <Progress
                    value={health.metrics.cpu_usage_percent}
                    className="h-2 mt-2"
                  />
                </CardContent>
              </Card>
            )}

            {health.metrics.memory_usage_percent !== undefined && (
              <Card>
                <CardHeader>
                  <CardTitle className="text-lg">Memory Usage</CardTitle>
                </CardHeader>
                <CardContent>
                  <div className="text-3xl font-bold">
                    {health.metrics.memory_usage_percent.toFixed(1)}%
                  </div>
                  <Progress
                    value={health.metrics.memory_usage_percent}
                    className="h-2 mt-2"
                  />
                </CardContent>
              </Card>
            )}

            {health.metrics.disk_usage_percent !== undefined && (
              <Card>
                <CardHeader>
                  <CardTitle className="text-lg">Disk Usage</CardTitle>
                </CardHeader>
                <CardContent>
                  <div className="text-3xl font-bold">
                    {health.metrics.disk_usage_percent.toFixed(1)}%
                  </div>
                  <Progress
                    value={health.metrics.disk_usage_percent}
                    className="h-2 mt-2"
                  />
                </CardContent>
              </Card>
            )}
          </div>
        </>
      )}
    </div>
  );
}
