import { useState } from 'react';
import { Outlet } from 'react-router-dom';
import { Menu } from 'lucide-react';
import { Sidebar } from './Sidebar';

export function Layout() {
  const [sidebarOpen, setSidebarOpen] = useState(false);

  return (
    <div className="min-h-screen bg-surface-50">
      <Sidebar isOpen={sidebarOpen} onClose={() => setSidebarOpen(false)} />
      
      <div className="lg:pl-64">
        {/* Mobile header */}
        <header className="lg:hidden sticky top-0 z-30 flex items-center h-16 px-4 bg-white border-b border-surface-200">
          <button
            onClick={() => setSidebarOpen(true)}
            className="p-2 -ml-2 text-surface-500 hover:text-surface-700"
          >
            <Menu className="w-6 h-6" />
          </button>
          <div className="ml-4 text-lg font-semibold text-surface-900">Aegion Admin</div>
        </header>

        {/* Main content */}
        <main className="p-4 lg:p-8">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
