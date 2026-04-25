import { useMutation, useQuery } from '@tanstack/react-query';
import { Check, Loader } from 'lucide-react';
import { useState } from 'react';

import { analyticsConfigApi, analyticsValidationApi } from '@/api/analytics';
import type { SyncStrategyConfig } from '@/types/analytics';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Skeleton } from '@/components/ui/skeleton';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';

export function SyncConfig() {
  const [isEditing, setIsEditing] = useState(false);
  const [formData, setFormData] = useState<SyncStrategyConfig | null>(null);

  const { data: config, isLoading, refetch } = useQuery({
    queryKey: ['sync-config'],
    queryFn: () => analyticsConfigApi.getSyncConfig(),
  });

  const updateMutation = useMutation({
    mutationFn: (data: SyncStrategyConfig) => analyticsConfigApi.updateSyncConfig(data),
    onSuccess: () => {
      refetch();
      setIsEditing(false);
    },
  });

  const triggerSyncMutation = useMutation({
    mutationFn: () => analyticsConfigApi.triggerManualSync(),
  });

  const handleSave = async () => {
    if (!formData) return;

    try {
      const validation = await analyticsValidationApi.validateSyncConfig(formData);
      if (!validation.valid) {
        console.error('Validation errors:', validation.errors);
        return;
      }

      await updateMutation.mutateAsync(formData);
    } catch (error) {
      console.error('Error saving config:', error);
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Sync Configuration</h1>
        <p className="text-muted-foreground">Configure data synchronization strategies</p>
      </div>

      {isLoading ? (
        <Skeleton className="h-96 w-full" />
      ) : config ? (
        <div className="space-y-4">
          {/* Status Card */}
          <Card>
            <CardHeader>
              <CardTitle>Sync Status</CardTitle>
              <CardDescription>Current synchronization state</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="grid grid-cols-3 gap-4">
                <div>
                  <p className="text-sm text-muted-foreground">Active Strategies</p>
                  <div className="flex flex-wrap gap-2 mt-2">
                    {config.active_strategies.map((strategy) => (
                      <Badge key={strategy} variant="outline">
                        {strategy}
                      </Badge>
                    ))}
                  </div>
                </div>
                <div>
                  <p className="text-sm text-muted-foreground">Last Sync</p>
                  <p className="text-sm font-medium mt-2">
                    {config.last_sync_at
                      ? new Date(config.last_sync_at).toLocaleString()
                      : 'Never'}
                  </p>
                </div>
                <div>
                  <p className="text-sm text-muted-foreground">Sync Lag</p>
                  <p className="text-sm font-medium mt-2">
                    {config.sync_lag_seconds ? `${config.sync_lag_seconds}s` : 'N/A'}
                  </p>
                </div>
              </div>

              <Button
                onClick={() => triggerSyncMutation.mutate()}
                disabled={triggerSyncMutation.isPending}
              >
                {triggerSyncMutation.isPending ? (
                  <Loader className="w-4 h-4 mr-2 animate-spin" />
                ) : null}
                Trigger Manual Sync
              </Button>
            </CardContent>
          </Card>

          {/* Strategy Details */}
          <Tabs defaultValue="strategies" className="w-full">
            <TabsList>
              <TabsTrigger value="strategies">Strategies</TabsTrigger>
              <TabsTrigger value="edit">Edit</TabsTrigger>
            </TabsList>

            <TabsContent value="strategies" className="space-y-4">
              {config.real_time && (
                <Card>
                  <CardHeader>
                    <CardTitle className="text-lg">Real-Time Sync</CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-2">
                    <div className="flex justify-between">
                      <span className="text-sm text-muted-foreground">Enabled</span>
                      <Badge variant={config.real_time.enabled ? 'default' : 'secondary'}>
                        {config.real_time.enabled ? 'On' : 'Off'}
                      </Badge>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-sm text-muted-foreground">Batch Size</span>
                      <span className="text-sm font-medium">{config.real_time.batch_size}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-sm text-muted-foreground">Flush Interval</span>
                      <span className="text-sm font-medium">
                        {config.real_time.flush_interval_ms}ms
                      </span>
                    </div>
                  </CardContent>
                </Card>
              )}

              {config.batch && (
                <Card>
                  <CardHeader>
                    <CardTitle className="text-lg">Batch Sync</CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-2">
                    <div className="flex justify-between">
                      <span className="text-sm text-muted-foreground">Enabled</span>
                      <Badge variant={config.batch.enabled ? 'default' : 'secondary'}>
                        {config.batch.enabled ? 'On' : 'Off'}
                      </Badge>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-sm text-muted-foreground">Schedule</span>
                      <span className="text-sm font-medium">{config.batch.schedule}</span>
                    </div>
                  </CardContent>
                </Card>
              )}

              {config.async && (
                <Card>
                  <CardHeader>
                    <CardTitle className="text-lg">Async Sync</CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-2">
                    <div className="flex justify-between">
                      <span className="text-sm text-muted-foreground">Enabled</span>
                      <Badge variant={config.async.enabled ? 'default' : 'secondary'}>
                        {config.async.enabled ? 'On' : 'Off'}
                      </Badge>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-sm text-muted-foreground">Broker Type</span>
                      <span className="text-sm font-medium">{config.async.broker_type}</span>
                    </div>
                  </CardContent>
                </Card>
              )}
            </TabsContent>

            <TabsContent value="edit" className="space-y-4">
              {isEditing && formData ? (
                <Card>
                  <CardHeader>
                    <CardTitle>Edit Configuration</CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-4">
                    {formData.real_time && (
                      <>
                        <div>
                          <label className="text-sm font-medium">Batch Size</label>
                          <Input
                            type="number"
                            value={formData.real_time.batch_size}
                            onChange={(e) =>
                              setFormData({
                                ...formData,
                                real_time: {
                                  ...formData.real_time!,
                                  batch_size: parseInt(e.target.value),
                                },
                              })
                            }
                          />
                        </div>
                        <div>
                          <label className="text-sm font-medium">Flush Interval (ms)</label>
                          <Input
                            type="number"
                            value={formData.real_time.flush_interval_ms}
                            onChange={(e) =>
                              setFormData({
                                ...formData,
                                real_time: {
                                  ...formData.real_time!,
                                  flush_interval_ms: parseInt(e.target.value),
                                },
                              })
                            }
                          />
                        </div>
                      </>
                    )}

                    <div className="flex gap-2">
                      <Button
                        onClick={handleSave}
                        disabled={updateMutation.isPending}
                      >
                        {updateMutation.isPending ? (
                          <Loader className="w-4 h-4 mr-2 animate-spin" />
                        ) : (
                          <Check className="w-4 h-4 mr-2" />
                        )}
                        Save
                      </Button>
                      <Button variant="outline" onClick={() => setIsEditing(false)}>
                        Cancel
                      </Button>
                    </div>
                  </CardContent>
                </Card>
              ) : (
                <Button
                  onClick={() => {
                    setFormData(config);
                    setIsEditing(true);
                  }}
                  variant="outline"
                >
                  Edit Configuration
                </Button>
              )}
            </TabsContent>
          </Tabs>
        </div>
      ) : null}
    </div>
  );
}
