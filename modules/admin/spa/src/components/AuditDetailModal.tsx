import { useState } from 'react';
import {
  FileText,
  X,
  Copy,
  Check,
  Globe,
  Clock,
  Shield,
  Ban,
  Tag,
} from 'lucide-react';
import type { ActivityFeedItem } from '../api/activity';

export interface AuditDetailModalProps {
  item: ActivityFeedItem | null;
  isOpen: boolean;
  onClose: () => void;
  onQuickBanIP?: (ip: string) => void;
}

export function AuditDetailModal({
  item,
  isOpen,
  onClose,
  onQuickBanIP,
}: AuditDetailModalProps) {
  const [copied, setCopied] = useState(false);

  if (!isOpen || !item) return null;

  const jsonString = item.details
    ? JSON.stringify(item.details, null, 2)
    : '{}';

  const handleCopyJSON = async () => {
    try {
      await navigator.clipboard.writeText(jsonString);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Fallback
    }
  };

  const isFailure =
    item.action.toLowerCase().includes('fail') ||
    item.action.toLowerCase().includes('denied') ||
    item.action.toLowerCase().includes('reject');

  const isSuccess =
    item.action.toLowerCase().includes('success') ||
    item.action.toLowerCase().includes('create') ||
    item.action.toLowerCase().includes('login');

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-sm animate-in fade-in duration-150"
      onClick={onClose}
    >
      <div
        className="w-full max-w-2xl bg-card border border-border rounded-xl shadow-2xl overflow-hidden flex flex-col max-h-[85vh] animate-in zoom-in-95 duration-150"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-border bg-muted/20">
          <div className="flex items-center gap-3">
            <div
              className={`p-2 rounded-lg ${
                isFailure
                  ? 'bg-destructive/10 text-destructive'
                  : isSuccess
                  ? 'bg-emerald-500/10 text-emerald-500'
                  : 'bg-primary/10 text-primary'
              }`}
            >
              <FileText className="w-5 h-5" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h2 className="text-base font-semibold text-foreground font-mono">{item.action}</h2>
                <span
                  className={`text-[10px] font-semibold px-2 py-0.5 rounded-full border ${
                    isFailure
                      ? 'bg-destructive/10 border-destructive/30 text-destructive'
                      : isSuccess
                      ? 'bg-emerald-500/10 border-emerald-500/30 text-emerald-500'
                      : 'bg-muted border-border text-muted-foreground'
                  }`}
                >
                  {isFailure ? 'FAILURE / BLOCKED' : isSuccess ? 'SUCCESS' : 'INFO'}
                </span>
              </div>
              <p className="text-xs text-muted-foreground mt-0.5 font-mono">Event ID: {item.id}</p>
            </div>
          </div>
          <button
            onClick={onClose}
            className="p-1 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Body content */}
        <div className="p-6 overflow-y-auto space-y-5 scrollbar-thin">
          {/* Metadata Grid */}
          <div className="grid grid-cols-2 sm:grid-cols-3 gap-3">
            <div className="p-3 rounded-lg border border-border bg-muted/20">
              <div className="flex items-center gap-1.5 text-xs text-muted-foreground mb-1">
                <Clock className="w-3.5 h-3.5" />
                <span>Timestamp</span>
              </div>
              <div className="text-xs font-medium text-foreground">
                {new Date(item.created_at).toLocaleString()}
              </div>
              <div className="text-[10px] text-muted-foreground font-mono mt-0.5">
                {new Date(item.created_at).toISOString()}
              </div>
            </div>

            <div className="p-3 rounded-lg border border-border bg-muted/20">
              <div className="flex items-center gap-1.5 text-xs text-muted-foreground mb-1">
                <Globe className="w-3.5 h-3.5" />
                <span>Source IP</span>
              </div>
              <div className="text-xs font-mono font-medium text-foreground flex items-center justify-between">
                <span>{item.ip_address || '127.0.0.1'}</span>
                {item.ip_address && item.ip_address !== '127.0.0.1' && onQuickBanIP && (
                  <button
                    onClick={() => onQuickBanIP(item.ip_address)}
                    title="Quick ban this IP"
                    className="p-1 rounded text-destructive hover:bg-destructive/10"
                  >
                    <Ban className="w-3 h-3" />
                  </button>
                )}
              </div>
            </div>

            <div className="p-3 rounded-lg border border-border bg-muted/20">
              <div className="flex items-center gap-1.5 text-xs text-muted-foreground mb-1">
                <Tag className="w-3.5 h-3.5" />
                <span>Resource</span>
              </div>
              <div className="text-xs font-medium text-foreground capitalize">
                {item.resource_type || 'General'}
              </div>
              {item.resource_id && (
                <div className="text-[10px] text-muted-foreground font-mono truncate mt-0.5" title={item.resource_id}>
                  {item.resource_id}
                </div>
              )}
            </div>

            <div className="p-3 rounded-lg border border-border bg-muted/20 col-span-2 sm:col-span-3">
              <div className="flex items-center gap-1.5 text-xs text-muted-foreground mb-1">
                <Shield className="w-3.5 h-3.5" />
                <span>Operator / Actor Reference</span>
              </div>
              <div className="text-xs font-mono text-foreground">
                {item.operator_id ? `Operator ID: ${item.operator_id}` : 'System / Anonymous Actor'}
              </div>
            </div>
          </div>

          {/* JSON Payload Viewer */}
          <div>
            <div className="flex items-center justify-between mb-2">
              <span className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                Event Context & Payload
              </span>
              <button
                onClick={handleCopyJSON}
                className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground px-2.5 py-1 rounded-md border border-border bg-background hover:bg-muted transition-colors"
              >
                {copied ? (
                  <>
                    <Check className="w-3.5 h-3.5 text-emerald-500" />
                    <span className="text-emerald-500 font-medium">Copied JSON!</span>
                  </>
                ) : (
                  <>
                    <Copy className="w-3.5 h-3.5" />
                    <span>Copy JSON</span>
                  </>
                )}
              </button>
            </div>
            <pre className="p-4 rounded-lg bg-zinc-950 text-zinc-100 font-mono text-xs overflow-x-auto border border-border/80 shadow-inner max-h-64 leading-relaxed">
              {jsonString}
            </pre>
          </div>
        </div>

        {/* Footer */}
        <div className="px-6 py-3 bg-muted/40 border-t border-border flex items-center justify-between">
          <span className="text-xs text-muted-foreground">
            Aegion Audit Trail • Tamper-evident log record
          </span>
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm font-medium text-primary-foreground bg-primary hover:bg-primary/90 rounded-lg transition-colors"
          >
            Done
          </button>
        </div>
      </div>
    </div>
  );
}
