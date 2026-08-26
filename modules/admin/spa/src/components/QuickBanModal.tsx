import { useState, useEffect } from 'react';
import { ShieldAlert, X, Ban, Clock, CheckCircle, AlertCircle } from 'lucide-react';
import { securityAdminApi } from '../api/security';

export interface QuickBanModalProps {
  isOpen: boolean;
  onClose: () => void;
  initialIP?: string;
  onSuccess?: () => void;
}

const DURATION_OPTIONS = [
  { label: '1 Hour', value: 1 * 60 * 60 * 1000 },
  { label: '24 Hours', value: 24 * 60 * 60 * 1000 },
  { label: '7 Days', value: 7 * 24 * 60 * 60 * 1000 },
  { label: '30 Days', value: 30 * 24 * 60 * 60 * 1000 },
  { label: 'Permanent', value: 0 },
];

const REASON_PRESETS = [
  'Suspicious login attempts / brute force',
  'Automated credential stuffing detected',
  'Known malicious scanner or bot traffic',
  'High volume session anomaly',
  'Manual administrative security block',
];

export function QuickBanModal({
  isOpen,
  onClose,
  initialIP = '',
  onSuccess,
}: QuickBanModalProps) {
  const [cidr, setCidr] = useState(initialIP);
  const [reason, setReason] = useState(REASON_PRESETS[0]);
  const [durationMs, setDurationMs] = useState(24 * 60 * 60 * 1000); // 24h default
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);

  useEffect(() => {
    if (isOpen) {
      setCidr(initialIP);
      setReason(REASON_PRESETS[0]);
      setDurationMs(24 * 60 * 60 * 1000);
      setError(null);
      setSuccess(false);
    }
  }, [isOpen, initialIP]);

  if (!isOpen) return null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const cleanCidr = cidr.trim();
    if (!cleanCidr) {
      setError('IP address or CIDR range is required');
      return;
    }

    setIsSubmitting(true);
    setError(null);

    try {
      let expiresAt: string | undefined = undefined;
      if (durationMs > 0) {
        expiresAt = new Date(Date.now() + durationMs).toISOString();
      }

      await securityAdminApi.upsertIPBan({
        cidr: cleanCidr,
        reason: reason.trim(),
        expires_at: expiresAt,
      });

      setSuccess(true);
      setTimeout(() => {
        if (onSuccess) onSuccess();
        onClose();
      }, 1000);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Failed to block IP address';
      setError(msg);
    } finally {
      setIsSubmitting(false);
    }
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
        <div className="flex items-center justify-between px-6 py-4 border-b border-border bg-destructive/5">
          <div className="flex items-center gap-3">
            <div className="p-2 rounded-lg bg-destructive/10 text-destructive">
              <ShieldAlert className="w-5 h-5" />
            </div>
            <div>
              <h2 className="text-base font-semibold text-foreground">Block IP Address / Subnet</h2>
              <p className="text-xs text-muted-foreground">Add CIDR block to security threat enforcement list</p>
            </div>
          </div>
          <button
            onClick={onClose}
            className="p-1 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Content & Form */}
        <form onSubmit={handleSubmit} className="p-6 space-y-4">
          {error && (
            <div className="p-3 text-sm text-destructive bg-destructive/10 border border-destructive/20 rounded-lg flex items-center gap-2">
              <AlertCircle className="w-4 h-4 shrink-0" />
              <span>{error}</span>
            </div>
          )}

          {success && (
            <div className="p-3 text-sm text-emerald-500 bg-emerald-500/10 border border-emerald-500/20 rounded-lg flex items-center gap-2">
              <CheckCircle className="w-4 h-4 shrink-0" />
              <span>IP address successfully blocked</span>
            </div>
          )}

          <div>
            <label className="block text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-1.5">
              IP Address or CIDR Range
            </label>
            <input
              type="text"
              required
              value={cidr}
              onChange={(e) => setCidr(e.target.value)}
              placeholder="e.g. 192.168.1.100 or 10.0.0.0/24"
              className="w-full px-3.5 py-2.5 rounded-lg border border-border bg-background text-foreground text-sm font-mono focus:outline-none focus:ring-2 focus:ring-destructive/50"
            />
          </div>

          <div>
            <label className="block text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-1.5">
              Reason for Block
            </label>
            <input
              type="text"
              required
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder="Enter reason..."
              className="w-full px-3.5 py-2.5 rounded-lg border border-border bg-background text-foreground text-sm focus:outline-none focus:ring-2 focus:ring-destructive/50 mb-2"
            />
            <div className="flex flex-wrap gap-1.5">
              {REASON_PRESETS.map((preset) => (
                <button
                  type="button"
                  key={preset}
                  onClick={() => setReason(preset)}
                  className={`text-[11px] px-2.5 py-1 rounded-md border transition-colors ${
                    reason === preset
                      ? 'bg-destructive/10 border-destructive/30 text-destructive font-medium'
                      : 'bg-muted/40 border-border/60 text-muted-foreground hover:text-foreground hover:bg-muted'
                  }`}
                >
                  {preset}
                </button>
              ))}
            </div>
          </div>

          <div>
            <label className="block text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-1.5">
              Ban Duration
            </label>
            <div className="grid grid-cols-5 gap-2">
              {DURATION_OPTIONS.map((opt) => (
                <button
                  type="button"
                  key={opt.label}
                  onClick={() => setDurationMs(opt.value)}
                  className={`py-2 px-2 text-xs font-medium rounded-lg border text-center transition-colors flex flex-col items-center gap-1 ${
                    durationMs === opt.value
                      ? 'bg-primary text-primary-foreground border-primary font-semibold'
                      : 'bg-background border-border text-foreground hover:bg-muted'
                  }`}
                >
                  <Clock className="w-3.5 h-3.5" />
                  <span>{opt.label}</span>
                </button>
              ))}
            </div>
          </div>

          {/* Action buttons */}
          <div className="flex items-center justify-end gap-3 pt-3 border-t border-border">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 text-sm font-medium text-muted-foreground hover:text-foreground hover:bg-muted rounded-lg transition-colors"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={isSubmitting || success}
              className="flex items-center gap-2 px-4 py-2 text-sm font-medium text-destructive-foreground bg-destructive hover:bg-destructive/90 rounded-lg shadow-sm transition-colors disabled:opacity-50"
            >
              <Ban className="w-4 h-4" />
              <span>{isSubmitting ? 'Enforcing Block...' : 'Block IP Address'}</span>
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
