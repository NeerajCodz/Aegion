import { useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { AlertCircle, Check, Loader, Plus, Trash2 } from 'lucide-react';

import { analyticsConfigApi, analyticsValidationApi } from '@/api/analytics';
import type { WebhookConfig } from '@/types/analytics';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Skeleton } from '@/components/ui/skeleton';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';

export function WebhookConfig() {
  const [selectedWebhook, setSelectedWebhook] = useState<WebhookConfig | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [formData, setFormData] = useState<Partial<WebhookConfig>>({
    name: '',
    url: '',
    enabled: true,
    filter: { event_types: ['all'] },
  });
  const [testResult, setTestResult] = useState<{ success: boolean; message: string } | null>(null);

  const { data: webhooks, isLoading, refetch } = useQuery({
    queryKey: ['webhooks'],
    queryFn: () => analyticsConfigApi.listWebhooks(),
  });

  const createMutation = useMutation({
    mutationFn: (data: WebhookConfig) => analyticsConfigApi.createWebhook(data),
    onSuccess: () => {
      refetch();
      setShowForm(false);
      setFormData({ name: '', url: '', enabled: true, filter: { event_types: ['all'] } });
    },
  });

  const updateMutation = useMutation({
    mutationFn: (data: WebhookConfig) => 
      analyticsConfigApi.updateWebhook(data.id!, data),
    onSuccess: () => {
      refetch();
      setSelectedWebhook(null);
      setFormData({ name: '', url: '', enabled: true, filter: { event_types: ['all'] } });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => analyticsConfigApi.deleteWebhook(id),
    onSuccess: () => refetch(),
  });

  const testMutation = useMutation({
    mutationFn: (id: string) => analyticsConfigApi.testWebhook(id),
    onSuccess: (result) => {
      setTestResult({
        success: true,
        message: `Test successful! Status: ${result.status_code}, Time: ${result.response_time_ms}ms`,
      });
    },
    onError: (error: any) => {
      setTestResult({
        success: false,
        message: error.message || 'Test failed',
      });
    },
  });

  const handleSubmit = async () => {
    try {
      const validation = await analyticsValidationApi.validateWebhookConfig(
        formData as WebhookConfig
      );

      if (!validation.valid) {
        console.error('Validation errors:', validation.errors);
        return;
      }

      if (selectedWebhook?.id) {
        await updateMutation.mutateAsync({
          ...selectedWebhook,
          ...formData,
        } as WebhookConfig);
      } else {
        await createMutation.mutateAsync(formData as WebhookConfig);
      }
    } catch (error) {
      console.error('Error saving webhook:', error);
    }
  };

  const statusColor = (status?: string) => {
    switch (status) {
      case 'active':
        return 'bg-green-500';
      case 'failing':
        return 'bg-red-500';
      default:
        return 'bg-gray-500';
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Webhook Configuration</h1>
        <p className="text-muted-foreground">Manage webhook endpoints for event delivery</p>
      </div>

      <Tabs defaultValue="list" className="w-full">
        <TabsList>
          <TabsTrigger value="list">Webhooks</TabsTrigger>
          <TabsTrigger value="delivery">Delivery History</TabsTrigger>
        </TabsList>

        <TabsContent value="list" className="space-y-4">
          <div className="flex justify-end">
            <Button
              onClick={() => {
                setSelectedWebhook(null);
                setShowForm(true);
                setFormData({ name: '', url: '', enabled: true, filter: { event_types: ['all'] } });
              }}
            >
              <Plus className="w-4 h-4 mr-2" />
              Add Webhook
            </Button>
          </div>

          {showForm && (
            <Card>
              <CardHeader>
                <CardTitle>{selectedWebhook ? 'Edit' : 'Create'} Webhook</CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div>
                  <label className="text-sm font-medium">Webhook Name</label>
                  <Input
                    value={formData.name || ''}
                    onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                    placeholder="My Webhook"
                  />
                </div>

                <div>
                  <label className="text-sm font-medium">URL</label>
                  <Input
                    value={formData.url || ''}
                    onChange={(e) => setFormData({ ...formData, url: e.target.value })}
                    placeholder="https://example.com/webhook"
                    type="url"
                  />
                </div>

                <div className="flex gap-2">
                  <Button onClick={handleSubmit} disabled={createMutation.isPending || updateMutation.isPending}>
                    {createMutation.isPending || updateMutation.isPending ? (
                      <Loader className="w-4 h-4 mr-2 animate-spin" />
                    ) : (
                      <Check className="w-4 h-4 mr-2" />
                    )}
                    Save
                  </Button>
                  <Button
                    variant="outline"
                    onClick={() => {
                      setShowForm(false);
                      setSelectedWebhook(null);
                    }}
                  >
                    Cancel
                  </Button>
                </div>
              </CardContent>
            </Card>
          )}

          {isLoading ? (
            <Skeleton className="h-96 w-full" />
          ) : (
            <Card>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Name</TableHead>
                    <TableHead>URL</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Last Triggered</TableHead>
                    <TableHead>Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {webhooks?.map((webhook) => (
                    <TableRow key={webhook.id}>
                      <TableCell className="font-medium">{webhook.name}</TableCell>
                      <TableCell className="text-sm text-muted-foreground break-all">{webhook.url}</TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <div className={`h-2 w-2 rounded-full ${statusColor(webhook.status)}`} />
                          <span className="text-sm">{webhook.status || 'unknown'}</span>
                        </div>
                      </TableCell>
                      <TableCell className="text-sm">
                        {webhook.last_triggered_at
                          ? new Date(webhook.last_triggered_at).toLocaleDateString()
                          : 'Never'}
                      </TableCell>
                      <TableCell className="flex gap-2">
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => testMutation.mutate(webhook.id!)}
                        >
                          Test
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => {
                            setSelectedWebhook(webhook);
                            setFormData(webhook);
                            setShowForm(true);
                          }}
                        >
                          Edit
                        </Button>
                        <Button
                          size="sm"
                          variant="destructive"
                          onClick={() => webhook.id && deleteMutation.mutate(webhook.id)}
                        >
                          <Trash2 className="w-4 h-4" />
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </Card>
          )}

          {testResult && (
            <Alert variant={testResult.success ? 'default' : 'destructive'}>
              {testResult.success ? (
                <Check className="h-4 w-4" />
              ) : (
                <AlertCircle className="h-4 w-4" />
              )}
              <AlertTitle>{testResult.success ? 'Test Successful' : 'Test Failed'}</AlertTitle>
              <AlertDescription>{testResult.message}</AlertDescription>
            </Alert>
          )}
        </TabsContent>

        <TabsContent value="delivery" className="space-y-4">
          <p className="text-muted-foreground">
            Select a webhook to view delivery history
          </p>
        </TabsContent>
      </Tabs>
    </div>
  );
}
