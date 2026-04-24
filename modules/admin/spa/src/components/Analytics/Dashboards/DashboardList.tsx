import { useQuery } from '@tanstack/react-query';
import { Plus } from 'lucide-react';
import { useState } from 'react';
import { Link } from 'react-router-dom';

import { analyticsPublicDashboardsApi } from '@/api/analytics';
import type { DashboardDefinition } from '@/types/analytics';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';

export function DashboardList() {
  const [favorites, setFavorites] = useState<Set<string>>(new Set());

  const { data: dashboards, isLoading } = useQuery({
    queryKey: ['dashboards'],
    queryFn: () => analyticsPublicDashboardsApi.listDashboards(),
  });

  const publicDashboards = dashboards?.filter((d) => d.is_public) || [];
  const customDashboards = dashboards?.filter((d) => !d.is_public) || [];

  const toggleFavorite = (id: string) => {
    setFavorites((prev) => {
      const newSet = new Set(prev);
      if (newSet.has(id)) {
        newSet.delete(id);
      } else {
        newSet.add(id);
      }
      return newSet;
    });
  };

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-start">
        <div>
          <h1 className="text-3xl font-bold">Dashboards</h1>
          <p className="text-muted-foreground">View and manage analytics dashboards</p>
        </div>
        <Link to="/analytics/dashboards/builder">
          <Button>
            <Plus className="w-4 h-4 mr-2" />
            Create Dashboard
          </Button>
        </Link>
      </div>

      <Tabs defaultValue="all" className="w-full">
        <TabsList>
          <TabsTrigger value="all">All</TabsTrigger>
          <TabsTrigger value="public">Public ({publicDashboards.length})</TabsTrigger>
          <TabsTrigger value="custom">Custom ({customDashboards.length})</TabsTrigger>
          <TabsTrigger value="favorites">Favorites ({favorites.size})</TabsTrigger>
        </TabsList>

        <TabsContent value="all" className="space-y-4">
          <DashboardTable
            dashboards={dashboards || []}
            isLoading={isLoading}
            favorites={favorites}
            onToggleFavorite={toggleFavorite}
          />
        </TabsContent>

        <TabsContent value="public" className="space-y-4">
          <DashboardTable
            dashboards={publicDashboards}
            isLoading={isLoading}
            favorites={favorites}
            onToggleFavorite={toggleFavorite}
          />
        </TabsContent>

        <TabsContent value="custom" className="space-y-4">
          <DashboardTable
            dashboards={customDashboards}
            isLoading={isLoading}
            favorites={favorites}
            onToggleFavorite={toggleFavorite}
          />
        </TabsContent>

        <TabsContent value="favorites" className="space-y-4">
          <DashboardTable
            dashboards={
              dashboards?.filter((d) => favorites.has(d.id!)) || []
            }
            isLoading={isLoading}
            favorites={favorites}
            onToggleFavorite={toggleFavorite}
          />
        </TabsContent>
      </Tabs>
    </div>
  );
}

function DashboardTable({
  dashboards,
  isLoading,
  favorites,
  onToggleFavorite,
}: {
  dashboards: DashboardDefinition[];
  isLoading: boolean;
  favorites: Set<string>;
  onToggleFavorite: (id: string) => void;
}) {
  if (isLoading) {
    return <Skeleton className="h-96 w-full" />;
  }

  return (
    <Card>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>Description</TableHead>
            <TableHead>Type</TableHead>
            <TableHead>Components</TableHead>
            <TableHead>Updated</TableHead>
            <TableHead>Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {dashboards.map((dashboard) => (
            <TableRow key={dashboard.id}>
              <TableCell className="font-medium">{dashboard.name}</TableCell>
              <TableCell className="text-muted-foreground text-sm">
                {dashboard.description || '-'}
              </TableCell>
              <TableCell>{dashboard.is_public ? 'Public' : 'Custom'}</TableCell>
              <TableCell>{dashboard.components?.length || 0}</TableCell>
              <TableCell className="text-sm">
                {dashboard.updated_at
                  ? new Date(dashboard.updated_at).toLocaleDateString()
                  : 'N/A'}
              </TableCell>
              <TableCell className="flex gap-2">
                <Link to={`/analytics/dashboards/${dashboard.id}`}>
                  <Button size="sm" variant="outline">
                    View
                  </Button>
                </Link>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => onToggleFavorite(dashboard.id!)}
                >
                  {favorites.has(dashboard.id!) ? '⭐' : '☆'}
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {dashboards.length === 0 && (
        <div className="p-4 text-center text-muted-foreground">
          No dashboards found
        </div>
      )}
    </Card>
  );
}
