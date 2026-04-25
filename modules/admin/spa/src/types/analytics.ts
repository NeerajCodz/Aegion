// Analytics configuration types
export type StorageBackend = 'local' | 's3' | 'iceberg' | 'k8s';
export type SyncStrategy = 'real-time' | 'batch' | 'async' | 'hybrid';
export type HealthState = 'healthy' | 'degraded' | 'offline';
export type WebhookEventType = 'all' | 'audit' | 'authentication' | 'system' | 'custom';

// Storage Configuration
export interface S3Config {
  bucket: string;
  region: string;
  endpoint?: string;
  access_key_id?: string;
  secret_access_key?: string;
  enable_encryption: boolean;
  encryption_type?: 'sse-s3' | 'sse-kms';
  kms_key_id?: string;
}

export interface IcebergConfig {
  warehouse_path: string;
  catalog_type: 'hive' | 'glue' | 'nessie';
  catalog_uri: string;
  use_partitioning: boolean;
  partition_columns?: string[];
}

export interface K8sConfig {
  namespace: string;
  pvc_name: string;
  storage_class?: string;
  access_mode: 'ReadWriteOnce' | 'ReadWriteMany';
  size_gb: number;
}

export interface LocalConfig {
  path: string;
  max_size_gb: number;
}

export interface StorageBackendConfig {
  backend: StorageBackend;
  s3_config?: S3Config;
  iceberg_config?: IcebergConfig;
  k8s_config?: K8sConfig;
  local_config?: LocalConfig;
  current_usage_bytes?: number;
  estimated_monthly_cost_usd?: number;
}

// Sync Configuration
export interface RealTimeSyncConfig {
  enabled: boolean;
  batch_size: number;
  flush_interval_ms: number;
  enable_compression: boolean;
}

export interface BatchSyncConfig {
  enabled: boolean;
  schedule: string; // cron expression
  start_time?: string; // HH:mm:ss
  batch_window_hours?: number;
  include_tables?: string[];
  exclude_tables?: string[];
}

export interface AsyncSyncConfig {
  enabled: boolean;
  broker_type: 'kafka' | 'rabbitmq' | 'pubsub';
  broker_url: string;
  topic_name: string;
  partitions?: number;
  consumer_group?: string;
  enable_dlq?: boolean;
}

export interface HybridFallbackConfig {
  enable_hybrid: boolean;
  primary_strategy: SyncStrategy;
  fallback_strategy?: SyncStrategy;
  fallback_threshold_seconds?: number;
  retry_count?: number;
}

export interface SyncStrategyConfig {
  active_strategies: SyncStrategy[];
  real_time?: RealTimeSyncConfig;
  batch?: BatchSyncConfig;
  async?: AsyncSyncConfig;
  hybrid?: HybridFallbackConfig;
  last_sync_at?: string;
  next_sync_at?: string;
  sync_lag_seconds?: number;
}

// Retention Configuration
export interface TierConfig {
  ttl_days: number;
  storage_backend: StorageBackend;
  compression_enabled: boolean;
  compression_type?: 'gzip' | 'snappy' | 'lz4';
}

export interface CategoryRetentionOverride {
  category: string; // e.g., 'audit_events', 'authentication', 'system'
  hot_ttl_days?: number;
  warm_ttl_days?: number;
  cold_ttl_days?: number;
  storage_backend?: StorageBackend;
}

export interface RetentionPolicy {
  hot_tier: TierConfig;
  warm_tier: TierConfig;
  cold_tier: TierConfig;
  category_overrides?: CategoryRetentionOverride[];
  estimated_storage_cost_monthly_usd?: number;
  estimated_monthly_cost_breakdown?: Record<string, number>;
}

// Webhook Configuration
export interface WebhookFilter {
  event_types: WebhookEventType[];
  categories?: string[];
  custom_filter_expression?: string;
  exclude_patterns?: string[];
}

export interface WebhookConfig {
  id?: string;
  name: string;
  url: string;
  description?: string;
  filter: WebhookFilter;
  enabled: boolean;
  rate_limit_per_minute?: number;
  retry_policy?: {
    max_retries: number;
    backoff_ms: number;
  };
  headers?: Record<string, string>;
  created_at?: string;
  updated_at?: string;
  last_triggered_at?: string;
  status?: 'active' | 'failing' | 'disabled';
}

export interface WebhookDeliveryRecord {
  id: string;
  webhook_id: string;
  event_id: string;
  attempt_number: number;
  status_code: number;
  response_time_ms: number;
  delivered_at: string;
  retry_at?: string;
  error_message?: string;
}

// Dashboard Configuration
export interface DashboardComponent {
  id: string;
  type: 'chart' | 'gauge' | 'table' | 'stat' | 'text' | 'custom';
  title: string;
  position: { x: number; y: number };
  size: { width: number; height: number };
  query_id?: string;
  config: Record<string, unknown>;
  refreshInterval?: number; // in seconds
}

export interface DashboardDefinition {
  id?: string;
  name: string;
  description?: string;
  components: DashboardComponent[];
  is_public: boolean;
  owner_id?: string;
  shared_with?: string[];
  tags?: string[];
  created_at?: string;
  updated_at?: string;
  favorite?: boolean;
}

// Event Types
export interface AnalyticsEvent {
  id: string;
  timestamp: string;
  event_type: string;
  category: string;
  source: string;
  user_id?: string;
  actor_type?: string;
  actor_id?: string;
  action: string;
  resource_type?: string;
  resource_id?: string;
  status: 'success' | 'failure';
  details: Record<string, unknown>;
  metadata?: Record<string, unknown>;
}

// Reports
export interface ScheduledReport {
  id?: string;
  name: string;
  description?: string;
  template: string;
  schedule: string; // cron expression
  email_recipients: string[];
  enabled: boolean;
  created_at?: string;
  updated_at?: string;
  last_sent_at?: string;
  next_send_at?: string;
}

// System Metrics
export interface ApiLatencyMetric {
  timestamp: string;
  p50_ms: number;
  p95_ms: number;
  p99_ms: number;
}

export interface ErrorRateMetric {
  timestamp: string;
  error_rate: number; // percentage
  error_count: number;
  total_requests: number;
}

export interface StorageTierMetric {
  tier: 'hot' | 'warm' | 'cold';
  usage_bytes: number;
  capacity_bytes: number;
}

export interface HealthStatus {
  overall_status: HealthState;
  components: {
    database: HealthState;
    storage: HealthState;
    sync_engine: HealthState;
    webhooks: HealthState;
    api: HealthState;
  };
  metrics?: {
    api_latency?: ApiLatencyMetric[];
    error_rate?: ErrorRateMetric[];
    storage_tiers?: StorageTierMetric[];
    webhook_delivery_success_rate?: number;
    sync_lag_seconds?: number;
    cpu_usage_percent?: number;
    memory_usage_percent?: number;
    disk_usage_percent?: number;
  };
  last_check_at: string;
  details?: Record<string, unknown>;
}

// API Response Types
export interface AnalyticsStats {
  total_events: number;
  events_today: number;
  events_this_month: number;
  unique_users: number;
  top_event_types: Array<{ type: string; count: number }>;
  generated_at: string;
}

export interface PaginatedAnalyticsResponse<T> {
  data: T[];
  pagination: {
    page: number;
    page_size: number;
    total: number;
    has_next: boolean;
    total_pages: number;
  };
  meta: {
    query_time_ms: number;
    cached_result: boolean;
  };
}

// Configuration validation results
export interface ValidationResult {
  valid: boolean;
  errors: Record<string, string[]>;
  warnings?: Record<string, string[]>;
}

// User preferences for analytics
export interface AnalyticsPreferences {
  favorite_dashboards: string[];
  recent_dashboards: string[];
  last_accessed_dashboard?: string;
  refresh_interval_seconds: number;
  auto_refresh_enabled: boolean;
  timezone?: string;
  date_format?: string;
}
