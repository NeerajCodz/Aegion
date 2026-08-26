import { useState, useEffect } from 'react';
import { Outlet } from 'react-router-dom';
import {
  Menu,
  Search,
  Ban,
  Download,
} from 'lucide-react';
import { Sidebar } from './Sidebar';
import { CommandPalette } from './CommandPalette';
import { QuickBanModal } from './QuickBanModal';
import { ExportSummaryModal } from './ExportSummaryModal';
import { useAuth } from '../hooks/useAuth';

export function Layout() {
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [commandPaletteOpen, setCommandPaletteOpen] = useState(false);
  const [quickBanOpen, setQuickBanOpen] = useState(false);
  const [exportModalOpen, setExportModalOpen] = useState(false);
  const { operator } = useAuth();

  // Keyboard shortcut listener for Cmd+K / Ctrl+K
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault();
        setCommandPaletteOpen((prev) => !prev);
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, []);

  return (
    <div className="min-h-screen bg-background">
      <Sidebar isOpen={sidebarOpen} onClose={() => setSidebarOpen(false)} />

      <div className="lg:pl-64 flex flex-col min-h-screen">
        {/* Top Header Bar (Desktop & Mobile) */}
        <header className="sticky top-0 z-30 flex items-center justify-between h-16 px-4 lg:px-8 bg-card/90 backdrop-blur-md border-b border-border">
          {/* Mobile hamburger & brand */}
          <div className="flex items-center gap-3 lg:hidden">
            <button
              onClick={() => setSidebarOpen(true)}
              className="p-2 -ml-2 text-muted-foreground hover:text-foreground rounded-lg hover:bg-muted"
              aria-label="Open sidebar"
            >
              <Menu className="w-5 h-5" />
            </button>
            <div className="text-base font-bold text-foreground">Aegion</div>
          </div>

          {/* Desktop Search / Command Bar Trigger */}
          <div className="hidden lg:flex items-center gap-3 flex-1 max-w-md">
            <button
              onClick={() => setCommandPaletteOpen(true)}
              className="w-full flex items-center justify-between px-3 py-1.5 rounded-lg border border-border/80 bg-background/80 hover:bg-muted text-xs text-muted-foreground hover:text-foreground transition-colors shadow-xs"
            >
              <div className="flex items-center gap-2">
                <Search className="w-3.5 h-3.5" />
                <span>Search pages, commands, or jump to...</span>
              </div>
              <kbd className="font-mono text-[10px] px-1.5 py-0.5 rounded border border-border bg-card">⌘K</kbd>
            </button>
          </div>

          {/* Right Controls & User Pill */}
          <div className="flex items-center gap-2 sm:gap-3 ml-auto">
            {/* System Status Pill */}
            <div className="hidden sm:flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-semibold bg-emerald-500/10 border border-emerald-500/30 text-emerald-500">
              <span className="relative flex h-2 w-2">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
                <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500" />
              </span>
              <span>Operational</span>
            </div>

            {/* Mobile search trigger */}
            <button
              onClick={() => setCommandPaletteOpen(true)}
              className="lg:hidden p-2 text-muted-foreground hover:text-foreground rounded-lg hover:bg-muted"
              title="Search commands (⌘K)"
            >
              <Search className="w-4 h-4" />
            </button>

            {/* Quick action buttons */}
            <button
              onClick={() => setQuickBanOpen(true)}
              className="hidden md:flex items-center gap-1.5 px-2.5 py-1.5 text-xs font-medium text-destructive hover:text-destructive border border-destructive/30 hover:bg-destructive/10 rounded-lg transition-colors"
              title="Quick ban IP address"
            >
              <Ban className="w-3.5 h-3.5" />
              <span>Ban IP</span>
            </button>

            <button
              onClick={() => setExportModalOpen(true)}
              className="hidden md:flex items-center gap-1.5 px-2.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground border border-border hover:bg-muted rounded-lg transition-colors"
              title="Export report"
            >
              <Download className="w-3.5 h-3.5" />
              <span>Export</span>
            </button>

            <div className="h-4 w-px bg-border hidden sm:block mx-1" />

            {/* Operator profile badge */}
            <div className="flex items-center gap-2 pl-1">
              <div className="w-8 h-8 rounded-full bg-primary/10 border border-primary/20 text-primary flex items-center justify-center font-bold text-xs">
                {operator?.name?.charAt(0).toUpperCase() || 'A'}
              </div>
              <div className="hidden xl:flex flex-col text-left">
                <span className="text-xs font-semibold text-foreground truncate max-w-28">
                  {operator?.name || 'Administrator'}
                </span>
                <span className="text-[10px] text-muted-foreground capitalize">
                  {operator?.role || 'Admin'}
                </span>
              </div>
            </div>
          </div>
        </header>

        {/* Main Content Area */}
        <main className="flex-1 p-4 lg:p-8 max-w-7xl w-full mx-auto">
          <Outlet />
        </main>
      </div>

      {/* Global Modals Mounted at Root */}
      <CommandPalette
        isOpen={commandPaletteOpen}
        onClose={() => setCommandPaletteOpen(false)}
        onOpenQuickBan={() => setQuickBanOpen(true)}
        onOpenExport={() => setExportModalOpen(true)}
      />

      <QuickBanModal
        isOpen={quickBanOpen}
        onClose={() => setQuickBanOpen(false)}
      />

      <ExportSummaryModal
        isOpen={exportModalOpen}
        onClose={() => setExportModalOpen(false)}
      />
    </div>
  );
}
