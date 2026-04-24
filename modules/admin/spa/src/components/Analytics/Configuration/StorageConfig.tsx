import { useMutation, useQuery } from '@tanstack/react-query';
import { AlertCircle, Check, Loader } from 'lucide-react';
import { useState } from 'react';

import { analyticsConfigApi, analyticsValidationApi } from '@/api/analytics';
import type { StorageBackendConfig } from '@/types/analytics';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Progress } from '@/components/ui/progress';
import { Skeleton } from '@/components/ui/skeleton';

export function StorageConfig() {
  const [testResult, setTestResult] = useState<{ success: boolean; message: string } | null>(null);
  const [isEditing, setIsEditing] = useState(false);
  const [formData, setFormData] = useState<StorageBackendConfig | null>(null);

  const { data: config, isLoading, refetch } = useQuery({
    queryKey: ['storage-config'],
    queryFn: () => analyticsConfigApi.getStorageConfig(),
  });

  const updateMutation = useMutation({
    mutationFn: (data: StorageBackendConfig) => analyticsConfigApi.updateStorageConfig(data),
    onSuccess: () => {
      refetch();
      setIsEditing(false);
    },
  });

  const testMutation = useMutation({
    mutationFn: (data: StorageBackendConfig) => analyticsConfigApi.testStorageConnection(data),
    onSuccess: (result) => {
      setTestResult({
        success: result.success,
        message: result.message,
      });
    },
  });

  const handleSave = async () => {
    if (!formData) return;

    try {
      const validation = await analyticsValidationApi.validateStorageConfig(formData);
      if (!validation.valid) {
        console.error('Validation errors:', validation.errors);
        return;
      }

      await updateMutation.mutateAsync(formData);
    } catch (error) {
      console.error('Error saving config:', error);
    }
  };

  const handleTest = async () => {
    if (!formData) return;
    await testMutation.mutateAsync(formData);
  };

  const getBackendIcon = (backend: string) => {
    const icons: Record<string, string> = {
      local: '💾',
      s3: '☁️',
      iceberg: '🧊',
      k8s: '⚙️',
    };
    return icons[backend] || '📦';
  };

  const usagePercent = config
    ? (((config.current_usage_bytes || 0) / 1099511627776) * 100).toFixed(2) // 1TB
    : 0;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Storage Configuration</h1>
        <p className="text-muted-foreground">Configure analytics data storage backend</p>
      </div>

      {testResult && (
        <Alert variant={testResult.success ? 'default' : 'destructive'}>
          {testResult.success ? (
            <Check className="h-4 w-4" />
          ) : (
            <AlertCircle className="h-4 w-4" />
          )}
          <AlertTitle>{testResult.success ? 'Connection Successful' : 'Connection Failed'}</AlertTitle>
          <AlertDescription>{testResult.message}</AlertDescription>
        </Alert>
      )}

      {isLoading ? (
        <Skeleton className="h-96 w-full" />
      ) : config ? (
        <div className="space-y-4">
          {/* Backend Info Card */}
          <Card>
            <CardHeader>
              <CardTitle>Active Backend</CardTitle>
              <CardDescription>Current storage configuration</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                  <span className="text-3xl">{getBackendIcon(config.backend)}</span>
                  <div>
                    <h3 className="font-semibold capitalize">{config.backend}</h3>
                    <p className="text-sm text-muted-foreground">
                      {config.backend === 'local'
                        ? `Local storage at ${config.local_config?.path}`
                        : config.backend === 's3'
                        ? `S3 bucket: ${config.s3_config?.bucket}`
                        : config.backend === 'iceberg'
                        ? `Iceberg warehouse: ${config.iceberg_config?.warehouse_path}`
                        : `K8s namespace: ${config.k8s_config?.namespace}`}
                    </p>
                  </div>
                </div>
                <Badge variant="outline">
                  {config.backend === 'local' ? 'On-Premise' : 'Cloud'}
                </Badge>
              </div>

              {/* Storage Usage */}
              <div className="space-y-2">
                <div className="flex justify-between text-sm">
                  <span>Storage Usage</span>
                  <span>{usagePercent}%</span>
                </div>
                <Progress value={parseFloat(usagePercent as string)} />
                <p className="text-xs text-muted-foreground">
                  {((config.current_usage_bytes || 0) / 1073741824).toFixed(2)} GB used
                </p>
              </div>

              {/* Cost Estimate */}
              {config.estimated_monthly_cost_usd !== undefined && (
                <div className="pt-4 border-t">
                  <div className="flex justify-between items-center">
                    <span className="text-sm font-medium">Estimated Monthly Cost</span>
                    <span className="text-lg font-bold">
                      ${config.estimated_monthly_cost_usd.toFixed(2)}/mo
                    </span>
                  </div>
                </div>
              )}
            </CardContent>
          </Card>

          {/* Configuration Editor */}
          {isEditing && (
            <Card>
              <CardHeader>
                <CardTitle>Edit Configuration</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                {config.s3_config && (
                  <>
                    <div>
                      <label className="text-sm font-medium">Bucket</label>
                      <Input
                        value={formData?.s3_config?.bucket || ''}
                        onChange={(e) =>
                          setFormData({
                            ...formData!,
                            s3_config: { ...formData?.s3_config!, bucket: e.target.value },
                          })
                        }
                      />
                    </div>
                    <div>
                      <label className="text-sm font-medium">Region</label>
                      <Input
                        value={formData?.s3_config?.region || ''}
                        onChange={(e) =>
                          setFormData({
                            ...formData!,
                            s3_config: { ...formData?.s3_config!, region: e.target.value },
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
          )}

          {/* Actions */}
          <div className="flex gap-2">
            {!isEditing && (
              <>
                <Button
                  onClick={() => {
                    setFormData(config);
                    setIsEditing(true);
                  }}
                  variant="outline"
                >
                  Edit Configuration
                </Button>
                <Button
                  onClick={handleTest}
                  variant="outline"
                  disabled={testMutation.isPending}
                >
                  {testMutation.isPending ? (
                    <Loader className="w-4 h-4 mr-2 animate-spin" />
                  ) : null}
                  Test Connection
                </Button>
              </>
            )}
          </div>
        </div>
      ) : null}
    </div>
  );
}
