import { useMutation, useQuery } from '@tanstack/react-query';
import { Check, Loader } from 'lucide-react';
import { useState } from 'react';

import { analyticsConfigApi, analyticsValidationApi } from '@/api/analytics';
import type { RetentionPolicy } from '@/types/analytics';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Progress } from '@/components/ui/progress';
import { Skeleton } from '@/components/ui/skeleton';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';

export function RetentionConfig() {
  const [isEditing, setIsEditing] = useState(false);
  const [formData, setFormData] = useState<RetentionPolicy | null>(null);

  const { data: policy, isLoading, refetch } = useQuery({
    queryKey: ['retention-policy'],
    queryFn: () => analyticsConfigApi.getRetentionPolicy(),
  });

  const updateMutation = useMutation({
    mutationFn: (data: RetentionPolicy) => analyticsConfigApi.updateRetentionPolicy(data),
    onSuccess: () => {
      refetch();
      setIsEditing(false);
    },
  });

  const archiveMutation = useMutation({
    mutationFn: (category?: string) => analyticsConfigApi.triggerArchival(category),
  });

  const { data: archiveHistory } = useQuery({
    queryKey: ['archive-history'],
    queryFn: () => analyticsConfigApi.getArchiveHistory(50),
  });

  const handleSave = async () => {
    if (!formData) return;

    try {
      const validation = await analyticsValidationApi.validateRetentionPolicy(formData);
      if (!validation.valid) {
        console.error('Validation errors:', validation.errors);
        return;
      }

      await updateMutation.mutateAsync(formData);
    } catch (error) {
      console.error('Error saving policy:', error);
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Retention Configuration</h1>
        <p className="text-muted-foreground">Manage data retention and archival policies</p>
      </div>

      {isLoading ? (
        <Skeleton className="h-96 w-full" />
      ) : policy ? (
        <div className="space-y-4">
          {/* Tier Overview */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <Card>
              <CardHeader>
                <CardTitle className="text-lg">Hot Tier</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div>
                  <div className="flex justify-between mb-2">
                    <span className="text-sm">TTL: {policy.hot_tier.ttl_days} days</span>
                    <span className="text-xs text-muted-foreground">Latest Data</span>
                  </div>
                  <Progress value={100} className="h-2" />
                </div>
                <p className="text-xs text-muted-foreground">
                  Backend: {policy.hot_tier.storage_backend}
                </p>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle className="text-lg">Warm Tier</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div>
                  <div className="flex justify-between mb-2">
                    <span className="text-sm">TTL: {policy.warm_tier.ttl_days} days</span>
                    <span className="text-xs text-muted-foreground">Medium Age</span>
                  </div>
                  <Progress value={66} className="h-2" />
                </div>
                <p className="text-xs text-muted-foreground">
                  Backend: {policy.warm_tier.storage_backend}
                </p>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle className="text-lg">Cold Tier</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div>
                  <div className="flex justify-between mb-2">
                    <span className="text-sm">TTL: {policy.cold_tier.ttl_days} days</span>
                    <span className="text-xs text-muted-foreground">Archive</span>
                  </div>
                  <Progress value={33} className="h-2" />
                </div>
                <p className="text-xs text-muted-foreground">
                  Backend: {policy.cold_tier.storage_backend}
                </p>
              </CardContent>
            </Card>
          </div>

          {/* Cost Breakdown */}
          {policy.estimated_storage_cost_monthly_usd !== undefined && (
            <Card>
              <CardHeader>
                <CardTitle>Cost Impact</CardTitle>
                <CardDescription>Estimated monthly storage cost</CardDescription>
              </CardHeader>
              <CardContent>
                <div className="space-y-2">
                  <div className="flex justify-between">
                    <span className="text-sm font-medium">Total Estimated Cost</span>
                    <span className="text-lg font-bold">
                      ${policy.estimated_storage_cost_monthly_usd.toFixed(2)}/mo
                    </span>
                  </div>
                  {policy.estimated_monthly_cost_breakdown &&
                    Object.entries(policy.estimated_monthly_cost_breakdown).map(
                      ([tier, cost]) => (
                        <div key={tier} className="flex justify-between text-sm">
                          <span className="text-muted-foreground capitalize">{tier}</span>
                          <span>${(cost as number).toFixed(2)}</span>
                        </div>
                      )
                    )}
                </div>
              </CardContent>
            </Card>
          )}

          {/* Actions and History */}
          <Tabs defaultValue="actions" className="w-full">
            <TabsList>
              <TabsTrigger value="actions">Actions</TabsTrigger>
              <TabsTrigger value="history">Archive History</TabsTrigger>
              <TabsTrigger value="edit">Edit</TabsTrigger>
            </TabsList>

            <TabsContent value="actions" className="space-y-4">
              <Card>
                <CardHeader>
                  <CardTitle className="text-lg">Archival</CardTitle>
                  <CardDescription>Manually trigger data archival</CardDescription>
                </CardHeader>
                <CardContent>
                  <Button
                    onClick={() => archiveMutation.mutate(undefined)}
                    disabled={archiveMutation.isPending}
                  >
                    {archiveMutation.isPending ? (
                      <Loader className="w-4 h-4 mr-2 animate-spin" />
                    ) : null}
                    Trigger Archival
                  </Button>
                </CardContent>
              </Card>
            </TabsContent>

            <TabsContent value="history" className="space-y-4">
              <Card>
                <CardHeader>
                  <CardTitle className="text-lg">Recent Archival Operations</CardTitle>
                </CardHeader>
                <CardContent>
                  {archiveHistory && archiveHistory.length > 0 ? (
                    <div className="space-y-2">
                      {archiveHistory.map((record) => (
                        <div key={record.id} className="flex justify-between p-2 border rounded">
                          <div>
                            <p className="text-sm font-medium">
                              {record.category || 'All Data'}
                            </p>
                            <p className="text-xs text-muted-foreground">
                              {new Date(record.timestamp).toLocaleString()}
                            </p>
                          </div>
                          <div className="text-right">
                            <p className="text-sm font-medium">{record.status}</p>
                            <p className="text-xs text-muted-foreground">
                              {(record.size_bytes / 1073741824).toFixed(2)} GB
                            </p>
                          </div>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <p className="text-sm text-muted-foreground">No archival history</p>
                  )}
                </CardContent>
              </Card>
            </TabsContent>

            <TabsContent value="edit" className="space-y-4">
              {isEditing && formData ? (
                <Card>
                  <CardHeader>
                    <CardTitle>Edit Policy</CardTitle>
                  </CardHeader>
                  <CardContent className="space-y-4">
                    <div>
                      <label className="text-sm font-medium">Hot Tier TTL (days)</label>
                      <Input
                        type="number"
                        value={formData.hot_tier.ttl_days}
                        onChange={(e) =>
                          setFormData({
                            ...formData,
                            hot_tier: {
                              ...formData.hot_tier,
                              ttl_days: parseInt(e.target.value),
                            },
                          })
                        }
                      />
                    </div>
                    <div>
                      <label className="text-sm font-medium">Warm Tier TTL (days)</label>
                      <Input
                        type="number"
                        value={formData.warm_tier.ttl_days}
                        onChange={(e) =>
                          setFormData({
                            ...formData,
                            warm_tier: {
                              ...formData.warm_tier,
                              ttl_days: parseInt(e.target.value),
                            },
                          })
                        }
                      />
                    </div>
                    <div>
                      <label className="text-sm font-medium">Cold Tier TTL (days)</label>
                      <Input
                        type="number"
                        value={formData.cold_tier.ttl_days}
                        onChange={(e) =>
                          setFormData({
                            ...formData,
                            cold_tier: {
                              ...formData.cold_tier,
                              ttl_days: parseInt(e.target.value),
                            },
                          })
                        }
                      />
                    </div>

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
                    setFormData(policy);
                    setIsEditing(true);
                  }}
                  variant="outline"
                >
                  Edit Policy
                </Button>
              )}
            </TabsContent>
          </Tabs>
        </div>
      ) : null}
    </div>
  );
}
