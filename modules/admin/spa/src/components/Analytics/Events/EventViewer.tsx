import { useQuery } from '@tanstack/react-query';
import { Download, Filter } from 'lucide-react';
import { useState } from 'react';

import { analyticsEventsApi } from '@/api/analytics';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Skeleton } from '@/components/ui/skeleton';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';

export function EventViewer() {
  const [page, setPage] = useState(1);
  const [searchQuery, setSearchQuery] = useState('');
  const filters: Record<string, unknown> = {};

  const { data: events, isLoading, refetch } = useQuery({
    queryKey: ['analytics-events', page, searchQuery, filters],
    queryFn: () => {
      if (searchQuery) {
        return analyticsEventsApi.searchEvents(searchQuery, filters);
      }
      return analyticsEventsApi.listEvents(page, 50, filters);
    },
  });

  const handleExport = async () => {
    try {
      const blob = await analyticsEventsApi.exportEvents('csv', filters);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `events-${new Date().toISOString()}.csv`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    } catch (error) {
      console.error('Error exporting events:', error);
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-start">
        <div>
          <h1 className="text-3xl font-bold">Events</h1>
          <p className="text-muted-foreground">View and analyze analytics events</p>
        </div>
        <Button onClick={handleExport} variant="outline">
          <Download className="w-4 h-4 mr-2" />
          Export
        </Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-lg">Filters</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex gap-2">
            <Input
              placeholder="Search events..."
              value={searchQuery}
              onChange={(e) => {
                setSearchQuery(e.target.value);
                setPage(1);
              }}
              className="flex-1"
            />
            <Button variant="outline" onClick={() => refetch()}>
              <Filter className="w-4 h-4 mr-2" />
              Search
            </Button>
          </div>
        </CardContent>
      </Card>

      {isLoading ? (
        <Skeleton className="h-96 w-full" />
      ) : events ? (
        <>
          <Card>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Timestamp</TableHead>
                  <TableHead>Event Type</TableHead>
                  <TableHead>Category</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Actor</TableHead>
                  <TableHead>Resource</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {events.data.map((event) => (
                  <TableRow key={event.id}>
                    <TableCell className="text-sm">
                      {new Date(event.timestamp).toLocaleString()}
                    </TableCell>
                    <TableCell>{event.event_type}</TableCell>
                    <TableCell>{event.category}</TableCell>
                    <TableCell>
                      <span
                        className={`px-2 py-1 rounded text-xs font-medium ${
                          event.status === 'success'
                            ? 'bg-green-100 text-green-800'
                            : 'bg-red-100 text-red-800'
                        }`}
                      >
                        {event.status}
                      </span>
                    </TableCell>
                    <TableCell className="text-sm">{event.actor_id || '-'}</TableCell>
                    <TableCell className="text-sm">
                      {event.resource_type || '-'}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Card>

          {events.pagination && (
            <div className="flex justify-between items-center">
              <p className="text-sm text-muted-foreground">
                Showing {events.data.length} of {events.pagination.total} events
              </p>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  onClick={() => setPage(Math.max(1, page - 1))}
                  disabled={page === 1}
                >
                  Previous
                </Button>
                <Button
                  variant="outline"
                  onClick={() => setPage(page + 1)}
                  disabled={!events.pagination.has_next}
                >
                  Next
                </Button>
              </div>
            </div>
          )}
        </>
      ) : null}
    </div>
  );
}
