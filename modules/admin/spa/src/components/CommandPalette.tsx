import React, { useEffect, useState, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Search,
  LayoutDashboard,
  Users,
  Activity,
  Shield,
  KeyRound,
  Network,
  GitBranch,
  Scale,
  Settings,
  ChartColumn,
  Ban,
  Download,
  Terminal,
  ArrowRight,
  X,
  PlusCircle,
  RefreshCw,
} from 'lucide-react';
import { useAuth } from '../hooks/useAuth';
import { operatorHasPermission } from '../lib/permissions';

export interface CommandPaletteProps {
  isOpen: boolean;
  onClose: () => void;
  onOpenQuickBan?: (ip?: string) => void;
  onOpenExport?: () => void;
  onTriggerRefresh?: () => void;
}

interface CommandItem {
  id: string;
  title: string;
  subtitle?: string;
  category: 'Navigation' | 'Security Actions' | 'Directory Actions' | 'System & Config';
  icon: React.ComponentType<{ className?: string }>;
  shortcut?: string;
  permission?: string;
  action: () => void;
}

export function CommandPalette({
  isOpen,
  onClose,
  onOpenQuickBan,
  onOpenExport,
  onTriggerRefresh,
}: CommandPaletteProps) {
  const [query, setQuery] = useState('');
  const [selectedIndex, setSelectedIndex] = useState(0);
  const navigate = useNavigate();
  const { operator } = useAuth();

  // Keyboard shortcut listener for Cmd+K / Ctrl+K and '/'
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault();
        if (isOpen) {
          onClose();
        } else {
          // Open handled by parent if listening, but if triggered here we toggle
        }
      } else if (e.key === '/' && !isOpen) {
        const target = e.target as HTMLElement;
        const isInput = target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable;
        if (!isInput) {
          e.preventDefault();
        }
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, onClose]);

  // Reset query and selection when modal opens
  useEffect(() => {
    if (isOpen) {
      setQuery('');
      setSelectedIndex(0);
    }
  }, [isOpen]);

  const items: CommandItem[] = useMemo(() => [
    // Navigation
    {
      id: 'nav-dashboard',
      title: 'Dashboard Overview',
      subtitle: 'Real-time KPIs, signals, and security command center',
      category: 'Navigation',
      icon: LayoutDashboard,
      shortcut: 'G D',
      permission: 'audit:read',
      action: () => { navigate('/'); onClose(); },
    },
    {
      id: 'nav-activity',
      title: 'Activity Feed & Audit Log',
      subtitle: 'Real-time security event stream and operational telemetry',
      category: 'Navigation',
      icon: Activity,
      shortcut: 'G A',
      permission: 'audit:read',
      action: () => { navigate('/activity'); onClose(); },
    },
    {
      id: 'nav-identities',
      title: 'Identities Directory',
      subtitle: 'User accounts, credentials, MFA factors, and profiles',
      category: 'Navigation',
      icon: Users,
      shortcut: 'G I',
      permission: 'identities:read',
      action: () => { navigate('/identities'); onClose(); },
    },
    {
      id: 'nav-sessions',
      title: 'Active Sessions',
      subtitle: 'Live session tokens, active IP addresses, and revocations',
      category: 'Navigation',
      icon: Activity,
      shortcut: 'G S',
      permission: 'sessions:read',
      action: () => { navigate('/sessions'); onClose(); },
    },
    {
      id: 'nav-operators',
      title: 'Admin Operators',
      subtitle: 'Operator roster, access keys, and administrative profiles',
      category: 'Navigation',
      icon: Shield,
      shortcut: 'G O',
      permission: 'operators:read',
      action: () => { navigate('/operators'); onClose(); },
    },
    {
      id: 'nav-roles',
      title: 'Roles & Permissions',
      subtitle: 'RBAC role assignments and system permissions',
      category: 'Navigation',
      icon: KeyRound,
      shortcut: 'G R',
      permission: 'roles:read',
      action: () => { navigate('/roles'); onClose(); },
    },
    {
      id: 'nav-security',
      title: 'Security & IP Bans',
      subtitle: 'CIDR threat blocks and security enforcement controls',
      category: 'Navigation',
      icon: Shield,
      shortcut: 'G B',
      permission: 'security:read',
      action: () => { navigate('/security'); onClose(); },
    },
    {
      id: 'nav-oauth2',
      title: 'OAuth2 & OIDC Clients',
      subtitle: 'Client applications, redirect URIs, and token grants',
      category: 'Navigation',
      icon: KeyRound,
      permission: 'oauth2:clients:read',
      action: () => { navigate('/oauth2'); onClose(); },
    },
    {
      id: 'nav-integrations',
      title: 'SSO & Social Integrations',
      subtitle: 'SAML, OIDC, Social Providers, and Reverse Proxy routes',
      category: 'Navigation',
      icon: Network,
      permission: 'config:read',
      action: () => { navigate('/integrations'); onClose(); },
    },
    {
      id: 'nav-policy',
      title: 'Policy Engine (ABAC & ReBAC)',
      subtitle: 'Attribute and relationship-based access policy evaluation',
      category: 'Navigation',
      icon: Scale,
      permission: 'config:read',
      action: () => { navigate('/policy'); onClose(); },
    },
    {
      id: 'nav-scim',
      title: 'SCIM 2.0 Provisioning',
      subtitle: 'Directory sync tokens, attribute schemas, and mappings',
      category: 'Navigation',
      icon: GitBranch,
      permission: 'config:read',
      action: () => { navigate('/scim'); onClose(); },
    },
    {
      id: 'nav-analytics',
      title: 'DuckDB Analytics',
      subtitle: 'Deep query metrics, aggregate views, and sync pipelines',
      category: 'Navigation',
      icon: ChartColumn,
      permission: 'analytics:read',
      action: () => { navigate('/analytics'); onClose(); },
    },
    {
      id: 'nav-settings',
      title: 'System Settings',
      subtitle: 'Session lifetimes, MFA policy, password complexity rules',
      category: 'Navigation',
      icon: Settings,
      permission: 'config:read',
      action: () => { navigate('/settings'); onClose(); },
    },

    // Security Actions
    {
      id: 'action-quick-ban',
      title: 'Quick Ban IP Address / CIDR',
      subtitle: 'Instantly block a malicious client IP or subnet',
      category: 'Security Actions',
      icon: Ban,
      permission: 'security:create',
      action: () => {
        onClose();
        if (onOpenQuickBan) onOpenQuickBan();
      },
    },
    {
      id: 'action-export-report',
      title: 'Export Security & Telemetry Report',
      subtitle: 'Download structured JSON or CSV operational audit snapshot',
      category: 'Security Actions',
      icon: Download,
      action: () => {
        onClose();
        if (onOpenExport) onOpenExport();
      },
    },
    {
      id: 'action-refresh-telemetry',
      title: 'Force Refresh Dashboard Telemetry',
      subtitle: 'Trigger instant refetch of all metrics, probes, and timeseries',
      category: 'Security Actions',
      icon: RefreshCw,
      action: () => {
        onClose();
        if (onTriggerRefresh) onTriggerRefresh();
      },
    },

    // Directory Actions
    {
      id: 'action-create-operator',
      title: 'Invite / Create New Operator',
      subtitle: 'Grant administrative dashboard access to an identity',
      category: 'Directory Actions',
      icon: PlusCircle,
      permission: 'operators:create',
      action: () => { navigate('/operators'); onClose(); },
    },
    {
      id: 'action-create-oauth-client',
      title: 'Register OAuth2 Application',
      subtitle: 'Scaffold new client credentials and grant types',
      category: 'Directory Actions',
      icon: KeyRound,
      permission: 'oauth2:clients:manage',
      action: () => { navigate('/oauth2'); onClose(); },
    },
    {
      id: 'action-generate-scim-token',
      title: 'Generate SCIM 2.0 API Token',
      subtitle: 'Create provisioning token for Okta, Azure AD, or OneLogin',
      category: 'Directory Actions',
      icon: GitBranch,
      permission: 'config:update',
      action: () => { navigate('/scim'); onClose(); },
    },
    {
      id: 'action-simulate-policy',
      title: 'Simulate Access Policy Decision',
      subtitle: 'Dry-run ABAC/ReBAC rules against mock subject and resource',
      category: 'System & Config',
      icon: Terminal,
      permission: 'config:read',
      action: () => { navigate('/policy'); onClose(); },
    },
  ], [navigate, onClose, onOpenQuickBan, onOpenExport, onTriggerRefresh]);

  const filteredItems = useMemo(() => {
    const visible = items.filter(
      (item) => !item.permission || operatorHasPermission(operator, item.permission)
    );

    if (!query.trim()) return visible;

    const q = query.toLowerCase().trim();
    return visible.filter(
      (item) =>
        item.title.toLowerCase().includes(q) ||
        (item.subtitle && item.subtitle.toLowerCase().includes(q)) ||
        item.category.toLowerCase().includes(q)
    );
  }, [items, query, operator]);

  // Handle arrow navigation and enter key
  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setSelectedIndex((prev) => (prev + 1 < filteredItems.length ? prev + 1 : 0));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setSelectedIndex((prev) => (prev - 1 >= 0 ? prev - 1 : filteredItems.length - 1));
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (filteredItems[selectedIndex]) {
        filteredItems[selectedIndex].action();
      }
    } else if (e.key === 'Escape') {
      e.preventDefault();
      onClose();
    }
  };

  if (!isOpen) return null;

  // Group filtered items by category
  const categories = Array.from(new Set(filteredItems.map((item) => item.category)));

  let currentIndexTracker = 0;

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center pt-16 sm:pt-24 px-4 bg-black/60 backdrop-blur-sm animate-in fade-in duration-150"
      onClick={onClose}
    >
      <div
        className="w-full max-w-2xl bg-card border border-border rounded-xl shadow-2xl overflow-hidden flex flex-col max-h-[80vh] animate-in zoom-in-95 duration-150"
        onClick={(e) => e.stopPropagation()}
        onKeyDown={handleKeyDown}
      >
        {/* Search Input Bar */}
        <div className="flex items-center px-4 py-3.5 border-b border-border gap-3 bg-muted/20">
          <Search className="w-5 h-5 text-muted-foreground shrink-0" />
          <input
            type="text"
            autoFocus
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setSelectedIndex(0);
            }}
            placeholder="Search pages, commands, quick actions, or policies... (↑↓ to navigate, ↵ to select)"
            className="flex-1 bg-transparent text-foreground placeholder:text-muted-foreground outline-none text-sm sm:text-base"
          />
          {query && (
            <button
              onClick={() => setQuery('')}
              className="text-xs text-muted-foreground hover:text-foreground px-1.5 py-0.5 rounded bg-muted"
            >
              Clear
            </button>
          )}
          <button
            onClick={onClose}
            className="p-1 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted"
            aria-label="Close command palette"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* Results List */}
        <div className="overflow-y-auto p-2 divide-y divide-border/40 scrollbar-thin">
          {filteredItems.length === 0 ? (
            <div className="py-12 text-center text-muted-foreground">
              <Search className="w-8 h-8 mx-auto mb-2 opacity-40" />
              <p className="text-sm font-medium">No results found for &ldquo;{query}&rdquo;</p>
              <p className="text-xs text-muted-foreground mt-1">
                Try searching for &ldquo;Identities&rdquo;, &ldquo;IP Ban&rdquo;, &ldquo;SCIM&rdquo;, or &ldquo;Export&rdquo;
              </p>
            </div>
          ) : (
            categories.map((category) => {
              const categoryItems = filteredItems.filter((item) => item.category === category);
              return (
                <div key={category} className="py-1.5 first:pt-0 last:pb-0">
                  <div className="px-3 py-1.5 text-xs font-semibold text-muted-foreground tracking-wider uppercase">
                    {category}
                  </div>
                  <div className="space-y-0.5">
                    {categoryItems.map((item) => {
                      const itemIndex = currentIndexTracker++;
                      const isSelected = itemIndex === selectedIndex;
                      const Icon = item.icon;

                      return (
                        <button
                          key={item.id}
                          onClick={() => item.action()}
                          onMouseEnter={() => setSelectedIndex(itemIndex)}
                          className={`w-full flex items-center justify-between px-3 py-2.5 rounded-lg text-left text-sm transition-colors ${
                            isSelected
                              ? 'bg-primary text-primary-foreground font-medium'
                              : 'text-foreground hover:bg-muted/60'
                          }`}
                        >
                          <div className="flex items-center gap-3 min-w-0">
                            <div
                              className={`p-1.5 rounded-md ${
                                isSelected ? 'bg-primary-foreground/20 text-primary-foreground' : 'bg-muted text-muted-foreground'
                              }`}
                            >
                              <Icon className="w-4 h-4" />
                            </div>
                            <div className="truncate">
                              <div className="truncate font-medium">{item.title}</div>
                              {item.subtitle && (
                                <div
                                  className={`text-xs truncate ${
                                    isSelected ? 'text-primary-foreground/80' : 'text-muted-foreground'
                                  }`}
                                >
                                  {item.subtitle}
                                </div>
                              )}
                            </div>
                          </div>

                          <div className="flex items-center gap-2 shrink-0 ml-3">
                            {item.shortcut && (
                              <kbd
                                className={`text-[10px] font-mono px-1.5 py-0.5 rounded border ${
                                  isSelected
                                    ? 'bg-primary-foreground/20 border-primary-foreground/30 text-primary-foreground'
                                    : 'bg-muted border-border text-muted-foreground'
                                }`}
                              >
                                {item.shortcut}
                              </kbd>
                            )}
                            <ArrowRight
                              className={`w-3.5 h-3.5 opacity-0 transition-opacity ${
                                isSelected ? 'opacity-100' : ''
                              }`}
                            />
                          </div>
                        </button>
                      );
                    })}
                  </div>
                </div>
              );
            })
          )}
        </div>

        {/* Footer info bar */}
        <div className="px-4 py-2.5 bg-muted/40 border-t border-border flex items-center justify-between text-xs text-muted-foreground">
          <div className="flex items-center gap-3">
            <span className="flex items-center gap-1">
              <kbd className="px-1.5 py-0.5 rounded border border-border bg-card font-mono text-[10px]">↑</kbd>
              <kbd className="px-1.5 py-0.5 rounded border border-border bg-card font-mono text-[10px]">↓</kbd>
              Navigate
            </span>
            <span className="flex items-center gap-1">
              <kbd className="px-1.5 py-0.5 rounded border border-border bg-card font-mono text-[10px]">↵</kbd>
              Select
            </span>
            <span className="flex items-center gap-1">
              <kbd className="px-1.5 py-0.5 rounded border border-border bg-card font-mono text-[10px]">ESC</kbd>
              Close
            </span>
          </div>
          <span className="font-medium text-foreground/80">Aegion Security Command</span>
        </div>
      </div>
    </div>
  );
}
