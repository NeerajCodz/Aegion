import { useQuery } from '@tanstack/react-query';
import { useParams } from 'react-router-dom';

import { analyticsPublicDashboardsApi } from '@/api/analytics';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';

export function DashboardViewer() {
  const { id } = useParams<{ id: string }>();

  const { data: dashboard, isLoading } = useQuery({
    queryKey: ['dashboard', id],
    queryFn: () => analyticsPublicDashboardsApi.getDashboard(id!),
    enabled: !!id,
  });

  return (
    <div className="space-y-6">
      {isLoading ? (
        <Skeleton className="h-96 w-full" />
      ) : dashboard ? (
        <>
          <div>
            <h1 className="text-3xl font-bold">{dashboard.name}</h1>
            <p className="text-muted-foreground">{dashboard.description}</p>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {dashboard.components?.map((component) => (
              <Card key={component.id}>
                <CardHeader>
                  <CardTitle className="text-lg">{component.title}</CardTitle>
                  <CardDescription>{component.type}</CardDescription>
                </CardHeader>
                <CardContent>
                  <div className="h-64 flex items-center justify-center bg-muted rounded">
                    <p className="text-muted-foreground">Chart rendering in production</p>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>

          {(!dashboard.components || dashboard.components.length === 0) && (
            <Card>
              <CardContent className="p-8 text-center text-muted-foreground">
                This dashboard has no components yet.
              </CardContent>
            </Card>
          )}
        </>
      ) : (
        <Card>
          <CardContent className="p-8 text-center text-muted-foreground">
            Dashboard not found
          </CardContent>
        </Card>
      )}
    </div>
  );
}
