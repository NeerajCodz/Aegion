import { useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { Check, Loader, Plus, Trash2 } from 'lucide-react';

import { analyticsPublicDashboardsApi } from '@/api/analytics';
import type { DashboardDefinition, DashboardComponent } from '@/types/analytics';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';

export function DashboardBuilder() {
  const [dashboard, setDashboard] = useState<DashboardDefinition>({
    name: '',
    description: '',
    components: [],
    is_public: false,
  });

  const createMutation = useMutation({
    mutationFn: (data: DashboardDefinition) => analyticsPublicDashboardsApi.createDashboard(data),
  });

  const addComponent = () => {
    const newComponent: DashboardComponent = {
      id: `component-${Date.now()}`,
      type: 'chart',
      title: 'New Component',
      position: { x: 0, y: dashboard.components.length },
      size: { width: 1, height: 1 },
      config: {},
    };
    setDashboard({
      ...dashboard,
      components: [...dashboard.components, newComponent],
    });
  };

  const removeComponent = (id: string) => {
    setDashboard({
      ...dashboard,
      components: dashboard.components.filter((c) => c.id !== id),
    });
  };

  const handleSave = async () => {
    try {
      await createMutation.mutateAsync(dashboard);
      setDashboard({
        name: '',
        description: '',
        components: [],
        is_public: false,
      });
    } catch (error) {
      console.error('Error saving dashboard:', error);
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Dashboard Builder</h1>
        <p className="text-muted-foreground">Create and customize analytics dashboards</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Dashboard Details</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div>
            <label className="text-sm font-medium">Dashboard Name</label>
            <Input
              value={dashboard.name}
              onChange={(e) => setDashboard({ ...dashboard, name: e.target.value })}
              placeholder="My Dashboard"
            />
          </div>
          <div>
            <label className="text-sm font-medium">Description</label>
            <Input
              value={dashboard.description}
              onChange={(e) => setDashboard({ ...dashboard, description: e.target.value })}
              placeholder="Dashboard description"
            />
          </div>
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={dashboard.is_public}
              onChange={(e) => setDashboard({ ...dashboard, is_public: e.target.checked })}
            />
            <span className="text-sm font-medium">Make Public</span>
          </label>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <div>
            <CardTitle>Components</CardTitle>
            <CardDescription>{dashboard.components.length} components added</CardDescription>
          </div>
          <Button onClick={addComponent} size="sm">
            <Plus className="w-4 h-4 mr-2" />
            Add Component
          </Button>
        </CardHeader>
        <CardContent>
          {dashboard.components.length === 0 ? (
            <p className="text-muted-foreground text-center py-8">
              No components yet. Click "Add Component" to get started.
            </p>
          ) : (
            <div className="space-y-2">
              {dashboard.components.map((component) => (
                <div key={component.id} className="flex items-center justify-between p-3 border rounded">
                  <div>
                    <p className="font-medium">{component.title}</p>
                    <p className="text-sm text-muted-foreground">{component.type}</p>
                  </div>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => removeComponent(component.id)}
                  >
                    <Trash2 className="w-4 h-4" />
                  </Button>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <div className="flex gap-2">
        <Button onClick={handleSave} disabled={createMutation.isPending || !dashboard.name}>
          {createMutation.isPending ? (
            <Loader className="w-4 h-4 mr-2 animate-spin" />
          ) : (
            <Check className="w-4 h-4 mr-2" />
          )}
          Save Dashboard
        </Button>
      </div>
    </div>
  );
}
