import { useQuery } from '@tanstack/react-query';
import { Plus, Trash2 } from 'lucide-react';
import { useState } from 'react';

import { analyticsReportsApi } from '@/api/analytics';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Skeleton } from '@/components/ui/skeleton';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';

export function ReportList() {
  const [showForm, setShowForm] = useState(false);
  const [formData, setFormData] = useState({
    name: '',
    description: '',
    template: 'standard',
    schedule: '0 0 * * 0',
    email_recipients: [] as string[],
    enabled: true,
  });

  const { data: reports, isLoading, refetch } = useQuery({
    queryKey: ['reports'],
    queryFn: () => analyticsReportsApi.listReports(),
  });

  const handleCreate = async () => {
    try {
      await analyticsReportsApi.createReport({
        ...formData,
        schedule: formData.schedule,
      });
      refetch();
      setShowForm(false);
      setFormData({
        name: '',
        description: '',
        template: 'standard',
        schedule: '0 0 * * 0',
        email_recipients: [],
        enabled: true,
      });
    } catch (error) {
      console.error('Error creating report:', error);
    }
  };

  const handleDelete = async (id: string) => {
    try {
      await analyticsReportsApi.deleteReport(id);
      refetch();
    } catch (error) {
      console.error('Error deleting report:', error);
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-start">
        <div>
          <h1 className="text-3xl font-bold">Reports</h1>
          <p className="text-muted-foreground">Schedule and manage analytics reports</p>
        </div>
        <Button onClick={() => setShowForm(true)}>
          <Plus className="w-4 h-4 mr-2" />
          New Report
        </Button>
      </div>

      {showForm && (
        <Card>
          <CardHeader>
            <CardTitle>Create Scheduled Report</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div>
              <label className="text-sm font-medium">Report Name</label>
              <Input
                value={formData.name}
                onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                placeholder="Weekly Analytics Report"
              />
            </div>

            <div>
              <label className="text-sm font-medium">Schedule (Cron)</label>
              <Input
                value={formData.schedule}
                onChange={(e) => setFormData({ ...formData, schedule: e.target.value })}
                placeholder="0 0 * * 0"
              />
            </div>

            <div className="flex gap-2">
              <Button onClick={handleCreate}>Create</Button>
              <Button variant="outline" onClick={() => setShowForm(false)}>
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
                <TableHead>Schedule</TableHead>
                <TableHead>Enabled</TableHead>
                <TableHead>Last Sent</TableHead>
                <TableHead>Next Send</TableHead>
                <TableHead>Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {reports?.map((report) => (
                <TableRow key={report.id}>
                  <TableCell className="font-medium">{report.name}</TableCell>
                  <TableCell>{report.schedule}</TableCell>
                  <TableCell>{report.enabled ? 'Yes' : 'No'}</TableCell>
                  <TableCell className="text-sm">
                    {report.last_sent_at
                      ? new Date(report.last_sent_at).toLocaleDateString()
                      : 'Never'}
                  </TableCell>
                  <TableCell className="text-sm">
                    {report.next_send_at
                      ? new Date(report.next_send_at).toLocaleDateString()
                      : 'N/A'}
                  </TableCell>
                  <TableCell className="flex gap-2">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => report.id && handleDelete(report.id)}
                    >
                      <Trash2 className="w-4 h-4" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {!reports || reports.length === 0 && (
            <div className="p-4 text-center text-muted-foreground">
              No reports found
            </div>
          )}
        </Card>
      )}
    </div>
  );
}
