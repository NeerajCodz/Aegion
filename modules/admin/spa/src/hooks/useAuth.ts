import { useState, useEffect, useCallback, createContext, useContext, type ReactNode } from 'react';
import type { AuthState, Operator, LoginCredentials } from '../types';
import { authApi } from '../api/operators';

interface AuthContextType extends AuthState {
  login: (credentials: LoginCredentials) => Promise<void>;
  logout: () => Promise<void>;
  isLoading: boolean;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({
    operator: null,
    token: null,
    isAuthenticated: false,
  });
  const [isLoading, setIsLoading] = useState(true);

  useEffect(() => {
    const token = localStorage.getItem('aegion_admin_token');
    const operatorStr = localStorage.getItem('aegion_admin_operator');
    
    if (token && operatorStr) {
      try {
        const operator = JSON.parse(operatorStr) as Operator;
        setState({
          operator,
          token,
          isAuthenticated: true,
        });
      } catch {
        localStorage.removeItem('aegion_admin_token');
        localStorage.removeItem('aegion_admin_operator');
      }
    }
    setIsLoading(false);
  }, []);

  const login = useCallback(async (credentials: LoginCredentials) => {
    const { token, operator } = await authApi.login(credentials);
    
    localStorage.setItem('aegion_admin_token', token);
    localStorage.setItem('aegion_admin_operator', JSON.stringify(operator));
    
    setState({
      operator,
      token,
      isAuthenticated: true,
    });
  }, []);

  const logout = useCallback(async () => {
    try {
      await authApi.logout();
    } catch {
      // Ignore logout errors
    }
    
    localStorage.removeItem('aegion_admin_token');
    localStorage.removeItem('aegion_admin_operator');
    
    setState({
      operator: null,
      token: null,
      isAuthenticated: false,
    });
  }, []);

  return (
    <AuthContext.Provider value={{ ...state, login, logout, isLoading }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthContextType {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return context;
}
