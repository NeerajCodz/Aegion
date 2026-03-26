export interface Identity {
  id: string;
  email: string;
  display_name: string;
  avatar_url?: string;
  status: 'active' | 'suspended' | 'pending';
  mfa_enabled: boolean;
  created_at: string;
  updated_at: string;
  last_login_at?: string;
  metadata?: Record<string, unknown>;
}

export interface IdentitySession {
  id: string;
  identity_id: string;
  user_agent: string;
  ip_address: string;
  created_at: string;
  expires_at: string;
  last_active_at: string;
  is_current?: boolean;
}

export interface Operator {
  id: string;
  email: string;
  name: string;
  role: 'admin' | 'operator' | 'viewer';
  status: 'active' | 'inactive';
  created_at: string;
  last_login_at?: string;
}

export interface SystemSettings {
  session_lifetime_hours: number;
  mfa_required: boolean;
  password_min_length: number;
  max_login_attempts: number;
  lockout_duration_minutes: number;
  allowed_domains?: string[];
}

export interface PaginatedResponse<T> {
  data: T[];
  total: number;
  page: number;
  per_page: number;
  total_pages: number;
}

export interface ApiError {
  message: string;
  code: string;
  details?: Record<string, unknown>;
}

export interface DashboardStats {
  total_identities: number;
  active_sessions: number;
  identities_last_24h: number;
  mfa_adoption_rate: number;
}

export interface AuthState {
  operator: Operator | null;
  token: string | null;
  isAuthenticated: boolean;
}

export interface LoginCredentials {
  email: string;
  password: string;
}
