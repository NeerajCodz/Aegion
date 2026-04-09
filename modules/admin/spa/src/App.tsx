import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AuthProvider, useAuth } from './hooks/useAuth';
import { Layout } from './components/Layout';
import { Dashboard } from './pages/Dashboard';
import { Identities } from './pages/Identities';
import { IdentityDetail } from './pages/IdentityDetail';
import { Sessions } from './pages/Sessions';
import { Operators } from './pages/Operators';
import { Roles } from './pages/Roles';
import { Settings } from './pages/Settings';
import { Login } from './pages/Login';
import { operatorHasPermission } from './lib/permissions';
import type { Operator } from './types';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
});

const permissionRoutes: Array<{ path: string; permission: string }> = [
  { path: '/', permission: 'audit:read' },
  { path: '/identities', permission: 'identities:read' },
  { path: '/sessions', permission: 'sessions:read' },
  { path: '/operators', permission: 'operators:read' },
  { path: '/roles', permission: 'roles:read' },
  { path: '/settings', permission: 'config:read' },
];

function fallbackRoute(operator: Operator | null): string {
  if (!operator) {
    return '/login';
  }
  for (const route of permissionRoutes) {
    if (operatorHasPermission(operator, route.permission)) {
      return route.path;
    }
  }
  return '/login';
}

function ProtectedRoute({
  children,
  permission,
}: {
  children: React.ReactNode;
  permission?: string;
}) {
  const { isAuthenticated, isLoading, operator } = useAuth();

  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-background">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-foreground"></div>
      </div>
    );
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />;
  }

  if (permission && !operatorHasPermission(operator, permission)) {
    return <Navigate to={fallbackRoute(operator)} replace />;
  }

  return <>{children}</>;
}

function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route
        path="/"
        element={
          <ProtectedRoute>
            <Layout />
          </ProtectedRoute>
        }
      >
        <Route
          index
          element={
            <ProtectedRoute permission="audit:read">
              <Dashboard />
            </ProtectedRoute>
          }
        />
        <Route
          path="identities"
          element={
            <ProtectedRoute permission="identities:read">
              <Identities />
            </ProtectedRoute>
          }
        />
        <Route
          path="identities/:id"
          element={
            <ProtectedRoute permission="identities:read">
              <IdentityDetail />
            </ProtectedRoute>
          }
        />
        <Route
          path="sessions"
          element={
            <ProtectedRoute permission="sessions:read">
              <Sessions />
            </ProtectedRoute>
          }
        />
        <Route
          path="operators"
          element={
            <ProtectedRoute permission="operators:read">
              <Operators />
            </ProtectedRoute>
          }
        />
        <Route
          path="roles"
          element={
            <ProtectedRoute permission="roles:read">
              <Roles />
            </ProtectedRoute>
          }
        />
        <Route
          path="settings"
          element={
            <ProtectedRoute permission="config:read">
              <Settings />
            </ProtectedRoute>
          }
        />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <AuthProvider>
          <AppRoutes />
        </AuthProvider>
      </BrowserRouter>
    </QueryClientProvider>
  );
}

export default App;
