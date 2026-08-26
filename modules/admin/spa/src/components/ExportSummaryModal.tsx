import { useState } from 'react';
import {
  Download,
  X,
  FileJson,
  FileSpreadsheet,
  CheckCircle,
  Shield,
  Activity,
  HeartPulse,
  Users,
} from 'lucide-react';
import type {
  DashboardStats,
  SecurityPosture,
  ModuleHealthStatus,
  Operator,
  IdentitySession,
} from '../types';

export interface ExportSummaryModalProps {
  isOpen: boolean;
  onClose: () => void;
  stats?: DashboardStats | null;
  securityPosture?: SecurityPosture | null;
  healthProbes?: ModuleHealthStatus[];
  operators?: Operator[];
  sessions?: IdentitySession[];
}

export function ExportSummaryModal({
  isOpen,
  onClose,
  stats,
  securityPosture,
  healthProbes = [],
  operators = [],
  sessions = [],
}: ExportSummaryModalProps) {
  const [format, setFormat] = useState<'json' | 'csv'>('json');
  const [includeStats, setIncludeStats] = useState(true);
  const [includePosture, setIncludePosture] = useState(true);
  const [includeHealth, setIncludeHealth] = useState(true);
  const [includeOperators, setIncludeOperators] = useState(true);
  const [includeSessions, setIncludeSessions] = useState(true);
  const [downloaded, setDownloaded] = useState(false);

  if (!isOpen) return null;

  const handleDownload = () => {
    const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
    const filename = `aegion-security-report-${timestamp}.${format}`;

    if (format === 'json') {
      const data: Record<string, unknown> = {
        generated_at: new Date().toISOString(),
        platform: 'Aegion IAM & Gateway',
      };

      if (includeStats && stats) {
        data.metrics = stats;
      }
      if (includePosture && securityPosture) {
        data.security_posture = securityPosture;
      }
      if (includeHealth) {
        data.subsystem_probes = healthProbes;
      }
      if (includeOperators) {
        data.operators = operators;
      }
      if (includeSessions) {
        data.active_sessions = sessions;
      }

      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = filename;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    } else {
      // CSV Export
      let csvContent = 'SECTION,FIELD,VALUE,TIMESTAMP\n';
      const now = new Date().toISOString();

      if (includeStats && stats) {
        csvContent += `Metrics,Total Identities,${stats.total_identities},${now}\n`;
        csvContent += `Metrics,Active Sessions,${stats.active_sessions},${now}\n`;
        csvContent += `Metrics,Signups (24h),${stats.identities_last_24h},${now}\n`;
        csvContent += `Metrics,MFA Adoption Rate,${stats.mfa_adoption_rate}%,${now}\n`;
        if (stats.passkey_adoption_rate !== undefined) {
          csvContent += `Metrics,Passkey Adoption Rate,${stats.passkey_adoption_rate}%,${now}\n`;
        }
        if (stats.active_ip_bans !== undefined) {
          csvContent += `Metrics,Active IP Bans,${stats.active_ip_bans},${now}\n`;
        }
      }

      if (includePosture && securityPosture) {
        csvContent += `Security Posture,Risk Score,${securityPosture.risk_score}/100,${now}\n`;
        csvContent += `Security Posture,Risk Level,${securityPosture.risk_level},${now}\n`;
        csvContent += `Security Posture,MFA Coverage,${securityPosture.mfa_coverage_pct}%,${now}\n`;
        csvContent += `Security Posture,Failed Logins (24h),${securityPosture.failed_logins_last_24h},${now}\n`;
        csvContent += `Security Posture,Active Threats Count,${securityPosture.threat_indicators.length},${now}\n`;
      }

      if (includeHealth) {
        for (const probe of healthProbes) {
          csvContent += `Health Probe,${probe.label},${probe.status.toUpperCase()} (${probe.response_time_ms}ms),${probe.checked_at}\n`;
        }
      }

      if (includeOperators) {
        for (const op of operators) {
          csvContent += `Operator,${op.name || op.email || op.id},${op.role} (${op.status}),${op.created_at}\n`;
        }
      }

      if (includeSessions) {
        for (const s of sessions) {
          csvContent += `Session,${s.id},IP: ${s.ip_address} | UA: ${s.user_agent},${s.last_active_at || s.created_at}\n`;
        }
      }

      const blob = new Blob([csvContent], { type: 'text/csv' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = filename;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    }

    setDownloaded(true);
    setTimeout(() => {
      setDownloaded(false);
      onClose();
    }, 1200);
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-sm animate-in fade-in duration-150"
      onClick={onClose}
    >
      <div
        className="w-full max-w-lg bg-card border border-border rounded-xl shadow-2xl overflow-hidden animate-in zoom-in-95 duration-150"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-border bg-muted/20">
          <div className="flex items-center gap-3">
            <div className="p-2 rounded-lg bg-primary/10 text-primary">
              <Download className="w-5 h-5" />
            </div>
            <div>
              <h2 className="text-base font-semibold text-foreground">Export Telemetry & Security Report</h2>
              <p className="text-xs text-muted-foreground">Download comprehensive operational audit snapshot</p>
            </div>
          </div>
          <button
            onClick={onClose}
            className="p-1 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Content */}
        <div className="p-6 space-y-5">
          {/* Format selection */}
          <div>
            <label className="block text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
              Export Format
            </label>
            <div className="grid grid-cols-2 gap-3">
              <button
                type="button"
                onClick={() => setFormat('json')}
                className={`flex items-center gap-3 p-3 rounded-lg border text-left transition-colors ${
                  format === 'json'
                    ? 'border-primary bg-primary/5 text-foreground ring-1 ring-primary'
                    : 'border-border bg-background text-muted-foreground hover:bg-muted hover:text-foreground'
                }`}
              >
                <FileJson className={`w-5 h-5 ${format === 'json' ? 'text-primary' : ''}`} />
                <div>
                  <div className="text-sm font-medium">JSON Format</div>
                  <div className="text-xs text-muted-foreground">Complete structured metadata</div>
                </div>
              </button>

              <button
                type="button"
                onClick={() => setFormat('csv')}
                className={`flex items-center gap-3 p-3 rounded-lg border text-left transition-colors ${
                  format === 'csv'
                    ? 'border-primary bg-primary/5 text-foreground ring-1 ring-primary'
                    : 'border-border bg-background text-muted-foreground hover:bg-muted hover:text-foreground'
                }`}
              >
                <FileSpreadsheet className={`w-5 h-5 ${format === 'csv' ? 'text-primary' : ''}`} />
                <div>
                  <div className="text-sm font-medium">CSV Format</div>
                  <div className="text-xs text-muted-foreground">Tabular spreadsheet view</div>
                </div>
              </button>
            </div>
          </div>

          {/* Scope checkboxes */}
          <div>
            <label className="block text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2">
              Report Sections to Include
            </label>
            <div className="space-y-2">
              <label className="flex items-center gap-3 p-2.5 rounded-lg border border-border/80 bg-background/60 hover:bg-muted/40 cursor-pointer transition-colors">
                <input
                  type="checkbox"
                  checked={includeStats}
                  onChange={(e) => setIncludeStats(e.target.checked)}
                  className="rounded border-border text-primary focus:ring-primary h-4 w-4"
                />
                <Activity className="w-4 h-4 text-muted-foreground" />
                <span className="text-sm font-medium text-foreground">KPI Metrics & Directory Velocity</span>
              </label>

              <label className="flex items-center gap-3 p-2.5 rounded-lg border border-border/80 bg-background/60 hover:bg-muted/40 cursor-pointer transition-colors">
                <input
                  type="checkbox"
                  checked={includePosture}
                  onChange={(e) => setIncludePosture(e.target.checked)}
                  className="rounded border-border text-primary focus:ring-primary h-4 w-4"
                />
                <Shield className="w-4 h-4 text-muted-foreground" />
                <span className="text-sm font-medium text-foreground">Security Posture & Threat Radar</span>
              </label>

              <label className="flex items-center gap-3 p-2.5 rounded-lg border border-border/80 bg-background/60 hover:bg-muted/40 cursor-pointer transition-colors">
                <input
                  type="checkbox"
                  checked={includeHealth}
                  onChange={(e) => setIncludeHealth(e.target.checked)}
                  className="rounded border-border text-primary focus:ring-primary h-4 w-4"
                />
                <HeartPulse className="w-4 h-4 text-muted-foreground" />
                <span className="text-sm font-medium text-foreground">Subsystem Probes & Latencies</span>
              </label>

              <label className="flex items-center gap-3 p-2.5 rounded-lg border border-border/80 bg-background/60 hover:bg-muted/40 cursor-pointer transition-colors">
                <input
                  type="checkbox"
                  checked={includeOperators}
                  onChange={(e) => setIncludeOperators(e.target.checked)}
                  className="rounded border-border text-primary focus:ring-primary h-4 w-4"
                />
                <Users className="w-4 h-4 text-muted-foreground" />
                <span className="text-sm font-medium text-foreground">Active Operators Roster</span>
              </label>

              <label className="flex items-center gap-3 p-2.5 rounded-lg border border-border/80 bg-background/60 hover:bg-muted/40 cursor-pointer transition-colors">
                <input
                  type="checkbox"
                  checked={includeSessions}
                  onChange={(e) => setIncludeSessions(e.target.checked)}
                  className="rounded border-border text-primary focus:ring-primary h-4 w-4"
                />
                <Activity className="w-4 h-4 text-muted-foreground" />
                <span className="text-sm font-medium text-foreground">Recent Live Sessions</span>
              </label>
            </div>
          </div>
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between px-6 py-4 bg-muted/40 border-t border-border">
          <span className="text-xs text-muted-foreground">
            Timestamped file download
          </span>
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 text-sm font-medium text-muted-foreground hover:text-foreground hover:bg-muted rounded-lg transition-colors"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={handleDownload}
              className="flex items-center gap-2 px-4 py-2 text-sm font-medium text-primary-foreground bg-primary hover:bg-primary/90 rounded-lg shadow-sm transition-colors"
            >
              {downloaded ? (
                <>
                  <CheckCircle className="w-4 h-4 text-emerald-400" />
                  <span>Report Generated!</span>
                </>
              ) : (
                <>
                  <Download className="w-4 h-4" />
                  <span>Download {format.toUpperCase()}</span>
                </>
              )}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
