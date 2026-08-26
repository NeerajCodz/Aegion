import { useState, useMemo, useEffect } from "react"
import { Link } from "react-router-dom"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import {
  Activity,
  AlertTriangle,
  ArrowRight,
  Ban,
  Clock,
  Command,
  Download,
  Eye,
  HeartPulse,
  Info,
  Lock,
  Plus,
  Radio,
  RefreshCw,
  Search,
  ShieldAlert,
  ShieldCheck,
  Trash2,
  TrendingUp,
  UserCheck,
  UserCog,
  Users,
  XCircle,
} from "lucide-react"
import {
  Area,
  AreaChart,
  CartesianGrid,
  Cell,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts"

import { identitiesApi } from "../api/identities"
import { dashboardApi, operatorsApi } from "../api/operators"
import { sessionsApi } from "../api/sessions"
import { securityAdminApi } from "../api/security"
import { activityApi, type ActivityFeedItem } from "../api/activity"
import type { Operator } from "../types"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Progress } from "@/components/ui/progress"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { healthStatusVariant, identityStatusVariant, operatorRoleVariant, operatorStatusVariant } from "@/lib/status"

import { CommandPalette } from "../components/CommandPalette"
import { QuickBanModal } from "../components/QuickBanModal"
import { AuditDetailModal } from "../components/AuditDetailModal"
import { ExportSummaryModal } from "../components/ExportSummaryModal"

const AUTO_REFRESH_INTERVALS = [
  { label: "Off", value: 0 },
  { label: "10s", value: 10000 },
  { label: "30s", value: 30000 },
  { label: "1m", value: 60000 },
  { label: "5m", value: 300000 },
]

const TIME_RANGES = [
  { label: "24 Hours", value: "24h" },
  { label: "7 Days", value: "7d" },
  { label: "30 Days", value: "30d" },
  { label: "90 Days", value: "90d" },
]

const PIE_COLORS = [
  "#3b82f6", // Passkeys - Blue
  "#10b981", // Passwords - Emerald
  "#8b5cf6", // Social OIDC - Purple
  "#f59e0b", // Enterprise SSO - Amber
  "#06b6d4", // MFA TOTP - Cyan
  "#ec4899", // MFA WebAuthn - Pink
]

const formatRelativeTime = (iso: string): string => {
  const value = new Date(iso).getTime()
  if (Number.isNaN(value)) {
    return "Unknown"
  }

  const deltaSeconds = Math.floor((Date.now() - value) / 1000)
  if (deltaSeconds < 60) return "Just now"
  if (deltaSeconds < 3600) return `${Math.floor(deltaSeconds / 60)}m ago`
  if (deltaSeconds < 86400) return `${Math.floor(deltaSeconds / 3600)}h ago`
  return `${Math.floor(deltaSeconds / 86400)}d ago`
}

const parseUserAgent = (userAgent: string): string => {
  if (!userAgent) return "Unknown client"
  if (userAgent.includes("Chrome")) return "Chrome"
  if (userAgent.includes("Firefox")) return "Firefox"
  if (userAgent.includes("Safari") && !userAgent.includes("Chrome")) return "Safari"
  if (userAgent.includes("Edge")) return "Edge"
  if (userAgent.includes("Postman") || userAgent.includes("curl")) return "API Client"
  return "Browser"
}

export function Dashboard() {
  const queryClient = useQueryClient()

  // Interactive controls state
  const [timeRange, setTimeRange] = useState<string>("24h")
  const [refreshInterval, setRefreshInterval] = useState<number>(30000) // Default 30s
  const [activeTab, setActiveTab] = useState<string>("overview")

  // Modal dialog states
  const [isCommandPaletteOpen, setIsCommandPaletteOpen] = useState(false)
  const [isQuickBanOpen, setIsQuickBanOpen] = useState(false)
  const [quickBanInitialIP, setQuickBanInitialIP] = useState<string>("")
  const [isExportModalOpen, setIsExportModalOpen] = useState(false)
  const [selectedAuditItem, setSelectedAuditItem] = useState<ActivityFeedItem | null>(null)

  // Filters for Tab 3 Activity Feed
  const [activitySearchQuery, setActivitySearchQuery] = useState("")
  const [activityFilterType, setActivityFilterType] = useState<string>("all")

  // Session termination loading state
  const [terminatingSessionId, setTerminatingSessionId] = useState<string | null>(null)

  // Keyboard shortcut listener for Cmd+K
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault()
        setIsCommandPaletteOpen((prev) => !prev)
      }
    }
    window.addEventListener("keydown", handleKeyDown)
    return () => window.removeEventListener("keydown", handleKeyDown)
  }, [])

  // Queries
  const statsQuery = useQuery({
    queryKey: ["dashboard-stats"],
    queryFn: dashboardApi.getStats,
    refetchInterval: refreshInterval > 0 ? refreshInterval : false,
    staleTime: 10000,
  })

  const timeSeriesQuery = useQuery({
    queryKey: ["dashboard-timeseries", timeRange],
    queryFn: () => dashboardApi.getTimeSeries(timeRange),
    refetchInterval: refreshInterval > 0 ? refreshInterval : false,
    staleTime: 10000,
  })

  const authBreakdownQuery = useQuery({
    queryKey: ["dashboard-auth-breakdown"],
    queryFn: dashboardApi.getAuthBreakdown,
    refetchInterval: refreshInterval > 0 ? refreshInterval : false,
    staleTime: 15000,
  })

  const securityPostureQuery = useQuery({
    queryKey: ["dashboard-security-posture"],
    queryFn: dashboardApi.getSecurityPosture,
    refetchInterval: refreshInterval > 0 ? refreshInterval : false,
    staleTime: 15000,
  })

  const healthQuery = useQuery({
    queryKey: ["dashboard-health"],
    queryFn: dashboardApi.getHealth,
    refetchInterval: refreshInterval > 0 ? refreshInterval : false,
    staleTime: 15000,
  })

  const observabilityQuery = useQuery({
    queryKey: ["dashboard-observability"],
    queryFn: dashboardApi.getObservability,
    staleTime: 30000,
  })

  const recentIdentitiesQuery = useQuery({
    queryKey: ["dashboard-recent-identities"],
    queryFn: () => identitiesApi.list({ page: 1, per_page: 6 }),
    refetchInterval: refreshInterval > 0 ? refreshInterval : false,
    staleTime: 15000,
  })

  const sessionsQuery = useQuery({
    queryKey: ["dashboard-sessions"],
    queryFn: () => sessionsApi.list({ page: 1, per_page: 6 }),
    refetchInterval: refreshInterval > 0 ? refreshInterval : false,
    staleTime: 15000,
  })

  const operatorsQuery = useQuery({
    queryKey: ["dashboard-operators"],
    queryFn: () => operatorsApi.list(1, 10),
    staleTime: 30000,
  })

  const ipBansQuery = useQuery({
    queryKey: ["dashboard-ip-bans"],
    queryFn: securityAdminApi.listIPBans,
    refetchInterval: refreshInterval > 0 ? refreshInterval : false,
    staleTime: 15000,
  })

  const activityFeedQuery = useQuery({
    queryKey: ["dashboard-activity-feed"],
    queryFn: () => activityApi.list(1, 15),
    refetchInterval: refreshInterval > 0 ? refreshInterval : false,
    staleTime: 10000,
  })

  // Trigger manual refresh
  const handleManualRefresh = () => {
    queryClient.invalidateQueries({ queryKey: ["dashboard-stats"] })
    queryClient.invalidateQueries({ queryKey: ["dashboard-timeseries"] })
    queryClient.invalidateQueries({ queryKey: ["dashboard-auth-breakdown"] })
    queryClient.invalidateQueries({ queryKey: ["dashboard-security-posture"] })
    queryClient.invalidateQueries({ queryKey: ["dashboard-health"] })
    queryClient.invalidateQueries({ queryKey: ["dashboard-sessions"] })
    queryClient.invalidateQueries({ queryKey: ["dashboard-ip-bans"] })
    queryClient.invalidateQueries({ queryKey: ["dashboard-activity-feed"] })
  }

  // Handle session termination
  const handleTerminateSession = async (sessionId: string) => {
    try {
      setTerminatingSessionId(sessionId)
      await sessionsApi.revoke(sessionId)
      queryClient.invalidateQueries({ queryKey: ["dashboard-sessions"] })
      queryClient.invalidateQueries({ queryKey: ["dashboard-stats"] })
    } catch {
      // Ignored
    } finally {
      setTerminatingSessionId(null)
    }
  }

  // Handle IP unban
  const handleRemoveIPBan = async (banId: string) => {
    try {
      await securityAdminApi.deleteIPBan(banId)
      queryClient.invalidateQueries({ queryKey: ["dashboard-ip-bans"] })
      queryClient.invalidateQueries({ queryKey: ["dashboard-stats"] })
      queryClient.invalidateQueries({ queryKey: ["dashboard-security-posture"] })
    } catch {
      // Ignored
    }
  }

  // Calculated Metrics
  const stats = statsQuery.data
  const posture = securityPostureQuery.data
  const timeSeries = timeSeriesQuery.data?.points ?? []
  const authBreakdown = authBreakdownQuery.data
  const healthChecks = healthQuery.data ?? []
  const ipBans = ipBansQuery.data ?? []
  const activityItems = activityFeedQuery.data?.items ?? []
  const recentIdentities = recentIdentitiesQuery.data?.data ?? []
  const activeSessions = sessionsQuery.data?.data ?? []
  const operators = operatorsQuery.data?.data ?? []

  const isFetching =
    statsQuery.isFetching ||
    timeSeriesQuery.isFetching ||
    authBreakdownQuery.isFetching ||
    securityPostureQuery.isFetching

  // System status summary
  const hasDegradedProbe = healthChecks.some((p) => p.status === "degraded")
  const hasOfflineProbe = healthChecks.some((p) => p.status === "offline")
  const systemStatus = hasOfflineProbe ? "degraded" : hasDegradedProbe ? "degraded" : "healthy"

  // Pie chart data for credential distribution
  const pieChartData = useMemo(() => {
    if (!authBreakdown) return []
    const items = [
      { name: "Passkeys (WebAuthn)", value: authBreakdown.passkeys_count },
      { name: "Passwords (Argon2id)", value: authBreakdown.passwords_count },
      { name: "Social OIDC", value: authBreakdown.social_oidc_count },
      { name: "Enterprise SSO", value: authBreakdown.enterprise_sso_count },
      { name: "MFA TOTP", value: authBreakdown.mfa_totp_count },
      { name: "MFA Backup Codes", value: authBreakdown.mfa_backup_codes_count },
    ].filter((item) => item.value > 0)

    if (items.length === 0) {
      return [
        { name: "Standard Identity Auth", value: stats?.total_identities || 1 },
      ]
    }
    return items
  }, [authBreakdown, stats])

  // Filtered activity feed items
  const filteredActivityItems = useMemo(() => {
    return activityItems.filter((item) => {
      const matchesQuery =
        !activitySearchQuery ||
        item.action.toLowerCase().includes(activitySearchQuery.toLowerCase()) ||
        item.resource_type.toLowerCase().includes(activitySearchQuery.toLowerCase()) ||
        item.ip_address.includes(activitySearchQuery)

      if (!matchesQuery) return false

      if (activityFilterType === "auth") {
        return item.action.toLowerCase().includes("login") || item.action.toLowerCase().includes("auth")
      }
      if (activityFilterType === "security") {
        return item.action.toLowerCase().includes("ban") || item.action.toLowerCase().includes("security")
      }
      if (activityFilterType === "create") {
        return item.action.toLowerCase().includes("create")
      }
      if (activityFilterType === "delete") {
        return item.action.toLowerCase().includes("delete") || item.action.toLowerCase().includes("revoke")
      }
      return true
    })
  }, [activityItems, activitySearchQuery, activityFilterType])

  return (
    <div className="space-y-6 animate-in fade-in duration-200">
      {/* 1. Header & Interactive Control Bar */}
      <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between pb-2 border-b border-border">
        <div>
          <div className="flex items-center gap-3">
            <h1 className="text-2xl lg:text-3xl font-bold tracking-tight text-foreground">
              Security Control Center
            </h1>
            <div
              className={`flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-semibold border ${
                systemStatus === "healthy"
                  ? "bg-emerald-500/10 border-emerald-500/30 text-emerald-500"
                  : "bg-amber-500/10 border-amber-500/30 text-amber-500"
              }`}
            >
              <span className="relative flex h-2 w-2">
                <span
                  className={`animate-ping absolute inline-flex h-full w-full rounded-full opacity-75 ${
                    systemStatus === "healthy" ? "bg-emerald-400" : "bg-amber-400"
                  }`}
                />
                <span
                  className={`relative inline-flex rounded-full h-2 w-2 ${
                    systemStatus === "healthy" ? "bg-emerald-500" : "bg-amber-500"
                  }`}
                />
              </span>
              <span className="capitalize">{systemStatus === "healthy" ? "All Systems Operational" : "Telemetry Warning"}</span>
            </div>
          </div>
          <p className="text-sm text-muted-foreground mt-1">
            Real-time identity management, session velocity, threat radar, and gateway telemetry.
          </p>
        </div>

        {/* Action & Control Bar */}
        <div className="flex flex-wrap items-center gap-2.5">
          {/* Time Range Selector */}
          <div className="flex items-center rounded-lg border border-border bg-card p-0.5">
            {TIME_RANGES.map((r) => (
              <button
                key={r.value}
                onClick={() => setTimeRange(r.value)}
                className={`px-2.5 py-1 text-xs font-medium rounded-md transition-colors ${
                  timeRange === r.value
                    ? "bg-primary text-primary-foreground font-semibold shadow-xs"
                    : "text-muted-foreground hover:text-foreground hover:bg-muted/60"
                }`}
              >
                {r.label}
              </button>
            ))}
          </div>

          {/* Auto Refresh Dropdown / Toggle */}
          <div className="flex items-center gap-1.5 px-2 py-1 rounded-lg border border-border bg-card text-xs text-muted-foreground">
            <Radio className={`w-3.5 h-3.5 ${refreshInterval > 0 ? "text-emerald-500 animate-pulse" : "text-muted-foreground"}`} />
            <select
              value={refreshInterval}
              onChange={(e) => setRefreshInterval(Number(e.target.value))}
              className="bg-transparent text-foreground text-xs font-medium focus:outline-none cursor-pointer"
            >
              {AUTO_REFRESH_INTERVALS.map((int) => (
                <option key={int.value} value={int.value} className="bg-card text-foreground">
                  {int.label}
                </option>
              ))}
            </select>
          </div>

          {/* Command Palette Button */}
          <Button
            variant="outline"
            size="sm"
            onClick={() => setIsCommandPaletteOpen(true)}
            className="flex items-center gap-1.5 text-xs font-medium"
          >
            <Command className="w-3.5 h-3.5" />
            <span className="hidden sm:inline">Commands</span>
            <kbd className="text-[10px] font-mono px-1 py-0.2 rounded border bg-muted">⌘K</kbd>
          </Button>

          {/* Quick Ban Button */}
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              setQuickBanInitialIP("")
              setIsQuickBanOpen(true)
            }}
            className="flex items-center gap-1.5 text-xs text-destructive hover:text-destructive border-destructive/30 hover:bg-destructive/10"
          >
            <Ban className="w-3.5 h-3.5" />
            <span>Ban IP</span>
          </Button>

          {/* Export Report Button */}
          <Button
            variant="outline"
            size="sm"
            onClick={() => setIsExportModalOpen(true)}
            className="flex items-center gap-1.5 text-xs"
          >
            <Download className="w-3.5 h-3.5" />
            <span>Export</span>
          </Button>

          {/* Refresh Now Button */}
          <Button
            variant="ghost"
            size="icon"
            onClick={handleManualRefresh}
            title="Refresh now"
            className="h-8 w-8 text-muted-foreground hover:text-foreground"
          >
            <RefreshCw className={`w-3.5 h-3.5 ${isFetching ? "animate-spin text-primary" : ""}`} />
          </Button>
        </div>
      </div>

      {/* 2. Top Metric Ribbon (5 Dynamic Cards) */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-5">
        {/* Card 1: Directory Scale */}
        <Card className="border-border/80 shadow-xs relative overflow-hidden">
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardDescription className="text-xs font-semibold uppercase tracking-wider">Directory Scale</CardDescription>
            <div className="p-1.5 rounded-lg bg-blue-500/10 text-blue-500">
              <Users className="size-4" />
            </div>
          </CardHeader>
          <CardContent className="space-y-1.5">
            <div className="text-2xl font-bold text-foreground">
              {statsQuery.isLoading ? <Skeleton className="h-7 w-20" /> : (stats?.total_identities ?? 0).toLocaleString()}
            </div>
            <div className="flex items-center gap-2 text-xs text-muted-foreground">
              <Badge variant="outline" className="text-[10px] font-mono text-emerald-500 border-emerald-500/30 bg-emerald-500/5">
                +{stats?.identities_last_24h ?? 0} in 24h
              </Badge>
              <span>{stats?.identities_last_7d ? `+${stats.identities_last_7d} 7d` : ""}</span>
            </div>
          </CardContent>
        </Card>

        {/* Card 2: Live Session Pressure */}
        <Card className="border-border/80 shadow-xs relative overflow-hidden">
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardDescription className="text-xs font-semibold uppercase tracking-wider">Active Sessions</CardDescription>
            <div className="p-1.5 rounded-lg bg-emerald-500/10 text-emerald-500">
              <Activity className="size-4" />
            </div>
          </CardHeader>
          <CardContent className="space-y-1.5">
            <div className="text-2xl font-bold text-foreground">
              {statsQuery.isLoading ? <Skeleton className="h-7 w-16" /> : stats?.active_sessions ?? 0}
            </div>
            <div className="text-xs text-muted-foreground flex items-center justify-between">
              <span>Pressure ratio:</span>
              <span className="font-mono font-medium text-foreground">
                {posture?.session_pressure_ratio ? posture.session_pressure_ratio.toFixed(2) : "0.00"} / id
              </span>
            </div>
          </CardContent>
        </Card>

        {/* Card 3: Security Threat Radar */}
        <Card className="border-border/80 shadow-xs relative overflow-hidden">
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardDescription className="text-xs font-semibold uppercase tracking-wider">Security Posture</CardDescription>
            <div
              className={`p-1.5 rounded-lg ${
                (posture?.risk_score ?? 15) >= 60
                  ? "bg-destructive/10 text-destructive"
                  : (posture?.risk_score ?? 15) >= 30
                  ? "bg-amber-500/10 text-amber-500"
                  : "bg-emerald-500/10 text-emerald-500"
              }`}
            >
              <ShieldCheck className="size-4" />
            </div>
          </CardHeader>
          <CardContent className="space-y-1.5">
            <div className="flex items-center justify-between">
              <div className="text-2xl font-bold text-foreground">
                {securityPostureQuery.isLoading ? <Skeleton className="h-7 w-16" /> : `${posture?.risk_score ?? 15}/100`}
              </div>
              <Badge
                className={`text-[10px] uppercase font-semibold ${
                  (posture?.risk_score ?? 15) >= 60
                    ? "bg-destructive text-destructive-foreground"
                    : (posture?.risk_score ?? 15) >= 30
                    ? "bg-amber-500 text-amber-950"
                    : "bg-emerald-500 text-emerald-950"
                }`}
              >
                {posture?.risk_level ?? "Strong"}
              </Badge>
            </div>
            <Progress value={posture?.risk_score ?? 15} className="h-1.5" />
          </CardContent>
        </Card>

        {/* Card 4: MFA & Passkey Velocity */}
        <Card className="border-border/80 shadow-xs relative overflow-hidden">
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardDescription className="text-xs font-semibold uppercase tracking-wider">MFA & Passkeys</CardDescription>
            <div className="p-1.5 rounded-lg bg-purple-500/10 text-purple-500">
              <Lock className="size-4" />
            </div>
          </CardHeader>
          <CardContent className="space-y-1.5">
            <div className="flex items-center justify-between">
              <div className="text-2xl font-bold text-foreground">
                {statsQuery.isLoading ? <Skeleton className="h-7 w-16" /> : `${(stats?.mfa_adoption_rate ?? 0).toFixed(0)}%`}
              </div>
              <span className="text-xs font-medium text-purple-500">
                Passkeys: {stats?.passkey_adoption_rate ? `${stats.passkey_adoption_rate.toFixed(0)}%` : "0%"}
              </span>
            </div>
            <Progress value={stats?.mfa_adoption_rate ?? 0} className="h-1.5" />
          </CardContent>
        </Card>

        {/* Card 5: Governance & Operator Roster */}
        <Card className="border-border/80 shadow-xs relative overflow-hidden">
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardDescription className="text-xs font-semibold uppercase tracking-wider">Governance</CardDescription>
            <div className="p-1.5 rounded-lg bg-amber-500/10 text-amber-500">
              <UserCog className="size-4" />
            </div>
          </CardHeader>
          <CardContent className="space-y-1.5">
            <div className="text-2xl font-bold text-foreground">
              {statsQuery.isLoading ? <Skeleton className="h-7 w-16" /> : `${stats?.active_operators ?? 1} Admins`}
            </div>
            <div className="text-xs text-muted-foreground flex items-center justify-between">
              <span>Roles: {stats?.total_roles ?? 3}</span>
              <span>OAuth2: {stats?.total_oauth2_clients ?? 0}</span>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* 3. Five Purpose-Built Tabs */}
      <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full space-y-6">
        <div className="border-b border-border pb-1">
          <TabsList className="bg-muted/40 p-1 rounded-xl">
            <TabsTrigger value="overview" className="flex items-center gap-2 text-xs font-medium">
              <TrendingUp className="size-3.5" />
              <span>Overview & Signals</span>
            </TabsTrigger>
            <TabsTrigger value="security" className="flex items-center gap-2 text-xs font-medium">
              <ShieldAlert className="size-3.5" />
              <span>Threat Radar</span>
              {posture?.threat_indicators && posture.threat_indicators.length > 0 && (
                <Badge variant="destructive" className="text-[10px] px-1.5 py-0 h-4">
                  {posture.threat_indicators.length}
                </Badge>
              )}
            </TabsTrigger>
            <TabsTrigger value="activity" className="flex items-center gap-2 text-xs font-medium">
              <Activity className="size-3.5" />
              <span>Activity Feed</span>
            </TabsTrigger>
            <TabsTrigger value="telemetry" className="flex items-center gap-2 text-xs font-medium">
              <HeartPulse className="size-3.5" />
              <span>System Telemetry</span>
            </TabsTrigger>
            <TabsTrigger value="governance" className="flex items-center gap-2 text-xs font-medium">
              <UserCheck className="size-3.5" />
              <span>Governance</span>
            </TabsTrigger>
          </TabsList>
        </div>

        {/* TAB 1: OVERVIEW & SIGNAL MATRIX */}
        <TabsContent value="overview" className="space-y-6">
          {/* Charts Row */}
          <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
            {/* Velocity & Traffic Time Series Chart */}
            <Card className="lg:col-span-2 border-border/80">
              <CardHeader className="flex flex-row items-center justify-between pb-2">
                <div>
                  <CardTitle className="text-base font-semibold">Identity Velocity & Session Concurrency</CardTitle>
                  <CardDescription className="text-xs">
                    New identity creations and live sessions over the last {timeRange}
                  </CardDescription>
                </div>
                <div className="flex items-center gap-2">
                  <span className="flex items-center gap-1 text-[11px] text-muted-foreground">
                    <span className="w-2.5 h-2.5 rounded-sm bg-blue-500" /> Identities
                  </span>
                  <span className="flex items-center gap-1 text-[11px] text-muted-foreground">
                    <span className="w-2.5 h-2.5 rounded-sm bg-emerald-500" /> Sessions
                  </span>
                </div>
              </CardHeader>
              <CardContent className="pt-2">
                <div className="h-64 w-full">
                  {timeSeriesQuery.isLoading ? (
                    <div className="h-full flex items-center justify-center">
                      <Skeleton className="h-56 w-full" />
                    </div>
                  ) : timeSeries.length === 0 ? (
                    <div className="h-full flex items-center justify-center text-xs text-muted-foreground">
                      No time-series points recorded for this window.
                    </div>
                  ) : (
                    <ResponsiveContainer width="100%" height="100%">
                      <AreaChart data={timeSeries} margin={{ top: 10, right: 10, left: -20, bottom: 0 }}>
                        <defs>
                          <linearGradient id="identitiesGradient" x1="0" y1="0" x2="0" y2="1">
                            <stop offset="5%" stopColor="#3b82f6" stopOpacity={0.4} />
                            <stop offset="95%" stopColor="#3b82f6" stopOpacity={0.0} />
                          </linearGradient>
                          <linearGradient id="sessionsGradient" x1="0" y1="0" x2="0" y2="1">
                            <stop offset="5%" stopColor="#10b981" stopOpacity={0.4} />
                            <stop offset="95%" stopColor="#10b981" stopOpacity={0.0} />
                          </linearGradient>
                        </defs>
                        <CartesianGrid strokeDasharray="3 3" vertical={false} opacity={0.2} />
                        <XAxis
                          dataKey="timestamp"
                          tickLine={false}
                          axisLine={false}
                          tickMargin={8}
                          minTickGap={32}
                          tickFormatter={(val) => {
                            const date = new Date(val)
                            return timeRange === "24h"
                              ? date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
                              : date.toLocaleDateString([], { month: "short", day: "numeric" })
                          }}
                          className="text-[10px] fill-muted-foreground"
                        />
                        <YAxis tickLine={false} axisLine={false} tickMargin={8} className="text-[10px] fill-muted-foreground" />
                        <Tooltip
                          content={({ active, payload, label }) => {
                            if (active && payload && payload.length) {
                              const labelStr = typeof label === 'string' || typeof label === 'number' ? new Date(label).toLocaleString() : ''
                              return (
                                <div className="rounded-lg border border-border bg-card p-2.5 shadow-xl text-xs space-y-1">
                                  <div className="font-semibold text-foreground">
                                    {labelStr}
                                  </div>
                                  <div className="text-blue-500 flex items-center justify-between gap-3">
                                    <span>New Identities:</span>
                                    <span className="font-mono font-bold">{payload[0]?.value ?? 0}</span>
                                  </div>
                                  <div className="text-emerald-500 flex items-center justify-between gap-3">
                                    <span>Active Sessions:</span>
                                    <span className="font-mono font-bold">{payload[1]?.value ?? 0}</span>
                                  </div>
                                </div>
                              )
                            }
                            return null
                          }}
                        />
                        <Area
                          type="monotone"
                          dataKey="new_identities"
                          stroke="#3b82f6"
                          strokeWidth={2}
                          fillOpacity={1}
                          fill="url(#identitiesGradient)"
                        />
                        <Area
                          type="monotone"
                          dataKey="active_sessions"
                          stroke="#10b981"
                          strokeWidth={2}
                          fillOpacity={1}
                          fill="url(#sessionsGradient)"
                        />
                      </AreaChart>
                    </ResponsiveContainer>
                  )}
                </div>
              </CardContent>
            </Card>

            {/* Credential & Auth Protocol Mix Donut Chart */}
            <Card className="border-border/80">
              <CardHeader className="pb-2">
                <CardTitle className="text-base font-semibold">Credential Protocol Mix</CardTitle>
                <CardDescription className="text-xs">
                  Passkeys, Passwords, Social OIDC, and Enterprise SSO distribution
                </CardDescription>
              </CardHeader>
              <CardContent className="pt-2 flex flex-col items-center justify-center">
                <div className="h-44 w-full">
                  <ResponsiveContainer width="100%" height="100%">
                    <PieChart>
                      <Pie
                        data={pieChartData}
                        cx="50%"
                        cy="50%"
                        innerRadius={48}
                        outerRadius={70}
                        paddingAngle={3}
                        dataKey="value"
                      >
                        {pieChartData.map((_, index) => (
                          <Cell key={`cell-${index}`} fill={PIE_COLORS[index % PIE_COLORS.length]} />
                        ))}
                      </Pie>
                      <Tooltip
                        content={({ active, payload }) => {
                          if (active && payload && payload.length) {
                            return (
                              <div className="rounded-lg border border-border bg-card p-2 text-xs shadow-md">
                                <span className="font-semibold text-foreground">{payload[0].name}: </span>
                                <span className="font-mono">{payload[0].value}</span>
                              </div>
                            )
                          }
                          return null
                        }}
                      />
                    </PieChart>
                  </ResponsiveContainer>
                </div>

                {/* Legend list */}
                <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 w-full mt-2 text-xs">
                  {pieChartData.slice(0, 4).map((entry, idx) => (
                    <div key={entry.name} className="flex items-center gap-1.5 truncate">
                      <span className="w-2 h-2 rounded-full shrink-0" style={{ backgroundColor: PIE_COLORS[idx % PIE_COLORS.length] }} />
                      <span className="text-muted-foreground truncate">{entry.name}</span>
                      <span className="font-mono text-foreground font-semibold ml-auto">{entry.value}</span>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          </div>

          {/* Tables Row: Recent Identities and Live Sessions */}
          <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
            {/* Recent Identities Table */}
            <Card className="border-border/80">
              <CardHeader className="flex flex-row items-center justify-between pb-3">
                <div>
                  <CardTitle className="text-base font-semibold">Recent Identities</CardTitle>
                  <CardDescription className="text-xs">Latest registered users across directory</CardDescription>
                </div>
                <Button asChild variant="outline" size="sm" className="text-xs h-7">
                  <Link to="/identities">
                    <span>View all</span>
                    <ArrowRight className="size-3 ml-1" />
                  </Link>
                </Button>
              </CardHeader>
              <CardContent>
                <Table>
                  <TableHeader>
                    <TableRow className="text-xs">
                      <TableHead>User / Email</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead className="text-right">Registered</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {recentIdentities.length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={3} className="text-center text-xs text-muted-foreground py-6">
                          No recent identities recorded.
                        </TableCell>
                      </TableRow>
                    ) : (
                      recentIdentities.map((identity) => (
                        <TableRow key={identity.id} className="text-xs">
                          <TableCell>
                            <div className="flex flex-col">
                              <span className="font-medium text-foreground">{identity.display_name || identity.email}</span>
                              <span className="text-[11px] text-muted-foreground font-mono truncate max-w-44">
                                {identity.email}
                              </span>
                            </div>
                          </TableCell>
                          <TableCell>
                            <Badge variant={identityStatusVariant(identity.status)} className="text-[10px]">
                              {identity.status}
                            </Badge>
                          </TableCell>
                          <TableCell className="text-right text-muted-foreground">
                            {formatRelativeTime(identity.created_at)}
                          </TableCell>
                        </TableRow>
                      ))
                    )}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>

            {/* Live Active Sessions Table with Terminate & Quick Ban */}
            <Card className="border-border/80">
              <CardHeader className="flex flex-row items-center justify-between pb-3">
                <div>
                  <CardTitle className="text-base font-semibold">Live Active Sessions</CardTitle>
                  <CardDescription className="text-xs">Active sessions with 1-click revocation & IP threat blocking</CardDescription>
                </div>
                <Button asChild variant="outline" size="sm" className="text-xs h-7">
                  <Link to="/sessions">
                    <span>Manage</span>
                    <ArrowRight className="size-3 ml-1" />
                  </Link>
                </Button>
              </CardHeader>
              <CardContent>
                <Table>
                  <TableHeader>
                    <TableRow className="text-xs">
                      <TableHead>Client / IP</TableHead>
                      <TableHead>Last Active</TableHead>
                      <TableHead className="text-right">Actions</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {activeSessions.length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={3} className="text-center text-xs text-muted-foreground py-6">
                          No active sessions currently open.
                        </TableCell>
                      </TableRow>
                    ) : (
                      activeSessions.map((session) => (
                        <TableRow key={session.id} className="text-xs">
                          <TableCell>
                            <div className="flex flex-col">
                              <span className="font-medium text-foreground">{parseUserAgent(session.user_agent)}</span>
                              <span className="text-[11px] font-mono text-muted-foreground">{session.ip_address}</span>
                            </div>
                          </TableCell>
                          <TableCell className="text-muted-foreground">
                            {session.last_active_at ? formatRelativeTime(session.last_active_at) : formatRelativeTime(session.created_at)}
                          </TableCell>
                          <TableCell className="text-right">
                            <div className="flex items-center justify-end gap-1.5">
                              {session.ip_address && session.ip_address !== "127.0.0.1" && (
                                <Button
                                  variant="ghost"
                                  size="sm"
                                  onClick={() => {
                                    setQuickBanInitialIP(session.ip_address)
                                    setIsQuickBanOpen(true)
                                  }}
                                  title="Quick block IP"
                                  className="h-7 px-2 text-destructive hover:bg-destructive/10 text-[11px]"
                                >
                                  <Ban className="size-3" />
                                  <span className="hidden sm:inline ml-1">Ban</span>
                                </Button>
                              )}
                              <Button
                                variant="ghost"
                                size="sm"
                                disabled={terminatingSessionId === session.id}
                                onClick={() => handleTerminateSession(session.id)}
                                title="Revoke session"
                                className="h-7 px-2 text-muted-foreground hover:text-foreground hover:bg-muted text-[11px]"
                              >
                                <XCircle className="size-3" />
                                <span className="hidden sm:inline ml-1">Revoke</span>
                              </Button>
                            </div>
                          </TableCell>
                        </TableRow>
                      ))
                    )}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        {/* TAB 2: SECURITY & THREAT RADAR */}
        <TabsContent value="security" className="space-y-6">
          {/* Posture overview metrics */}
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <Card className="border-border/80">
              <CardHeader className="pb-2">
                <CardDescription className="text-xs">Failed Logins (24h)</CardDescription>
                <CardTitle className="text-2xl font-bold text-foreground">
                  {posture?.failed_logins_last_24h ?? 0}
                </CardTitle>
              </CardHeader>
              <CardContent className="text-xs text-muted-foreground">
                Authentication failure attempts logged
              </CardContent>
            </Card>

            <Card className="border-border/80">
              <CardHeader className="pb-2">
                <CardDescription className="text-xs">Active IP Bans</CardDescription>
                <CardTitle className="text-2xl font-bold text-foreground">
                  {posture?.active_ip_bans_count ?? ipBans.length}
                </CardTitle>
              </CardHeader>
              <CardContent className="text-xs text-muted-foreground">
                CIDRs blocked by security policies
              </CardContent>
            </Card>

            <Card className="border-border/80">
              <CardHeader className="pb-2">
                <CardDescription className="text-xs">Wildcard SCIM Tokens</CardDescription>
                <CardTitle className="text-2xl font-bold text-foreground">
                  {posture?.wildcard_scim_tokens ?? 0}
                </CardTitle>
              </CardHeader>
              <CardContent className="text-xs text-muted-foreground">
                Tokens with unrestricted directory sync
              </CardContent>
            </Card>

            <Card className="border-border/80">
              <CardHeader className="pb-2">
                <CardDescription className="text-xs">Unrotated OAuth Secrets</CardDescription>
                <CardTitle className="text-2xl font-bold text-foreground">
                  {posture?.unrotated_oauth_secrets ?? 0}
                </CardTitle>
              </CardHeader>
              <CardContent className="text-xs text-muted-foreground">
                Secrets older than 90 days
              </CardContent>
            </Card>
          </div>

          {/* Active Threat Indicators Alert Grid */}
          <Card className="border-border/80">
            <CardHeader className="pb-3">
              <CardTitle className="text-base font-semibold flex items-center gap-2">
                <ShieldAlert className="size-4 text-amber-500" />
                <span>Active Threat Indicators & Actionable Recommendations</span>
              </CardTitle>
              <CardDescription className="text-xs">
                Real-time security rule validations and posture guardrails
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              {!posture?.threat_indicators || posture.threat_indicators.length === 0 ? (
                <div className="p-4 rounded-lg bg-emerald-500/10 border border-emerald-500/20 text-emerald-500 flex items-center gap-3">
                  <ShieldCheck className="size-5 shrink-0" />
                  <div>
                    <div className="text-sm font-semibold">Zero Critical Security Threats Detected</div>
                    <div className="text-xs text-emerald-600/80">
                      MFA adoption, IP bans, and token scopes meet established security standards.
                    </div>
                  </div>
                </div>
              ) : (
                posture.threat_indicators.map((threat) => (
                  <div
                    key={threat.id}
                    className={`p-4 rounded-lg border flex flex-col sm:flex-row sm:items-center justify-between gap-3 ${
                      threat.severity === "critical"
                        ? "bg-destructive/10 border-destructive/30 text-destructive"
                        : threat.severity === "warning"
                        ? "bg-amber-500/10 border-amber-500/30 text-amber-500"
                        : "bg-blue-500/10 border-blue-500/30 text-blue-500"
                    }`}
                  >
                    <div className="flex items-start gap-3">
                      {threat.severity === "critical" ? (
                        <ShieldAlert className="size-5 shrink-0 mt-0.5" />
                      ) : threat.severity === "warning" ? (
                        <AlertTriangle className="size-5 shrink-0 mt-0.5" />
                      ) : (
                        <Info className="size-5 shrink-0 mt-0.5" />
                      )}
                      <div>
                        <div className="flex items-center gap-2">
                          <span className="text-sm font-semibold text-foreground">{threat.title}</span>
                          <Badge
                            className={`text-[9px] uppercase font-mono ${
                              threat.severity === "critical"
                                ? "bg-destructive text-destructive-foreground"
                                : threat.severity === "warning"
                                ? "bg-amber-500 text-amber-950"
                                : "bg-blue-500 text-blue-950"
                            }`}
                          >
                            {threat.severity}
                          </Badge>
                        </div>
                        <p className="text-xs text-muted-foreground mt-0.5">{threat.description}</p>
                      </div>
                    </div>

                    {threat.action_url && (
                      <Button asChild size="sm" variant="outline" className="text-xs shrink-0 self-end sm:self-auto">
                        <Link to={threat.action_url}>
                          <span>{threat.action_label || "Resolve"}</span>
                          <ArrowRight className="size-3 ml-1.5" />
                        </Link>
                      </Button>
                    )}
                  </div>
                ))
              )}
            </CardContent>
          </Card>

          {/* Active IP Bans Management Table */}
          <Card className="border-border/80">
            <CardHeader className="flex flex-row items-center justify-between pb-3">
              <div>
                <CardTitle className="text-base font-semibold">Active IP Bans</CardTitle>
                <CardDescription className="text-xs">Blocked CIDR subnets and client addresses</CardDescription>
              </div>
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  setQuickBanInitialIP("")
                  setIsQuickBanOpen(true)
                }}
                className="text-xs h-7 gap-1.5 text-destructive border-destructive/30"
              >
                <Plus className="size-3" />
                <span>Add IP Ban</span>
              </Button>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow className="text-xs">
                    <TableHead>CIDR / IP Range</TableHead>
                    <TableHead>Reason</TableHead>
                    <TableHead>Created</TableHead>
                    <TableHead>Expires</TableHead>
                    <TableHead className="text-right">Action</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {ipBans.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={5} className="text-center text-xs text-muted-foreground py-6">
                        No active IP bans currently enforced.
                      </TableCell>
                    </TableRow>
                  ) : (
                    ipBans.map((ban) => (
                      <TableRow key={ban.id} className="text-xs">
                        <TableCell className="font-mono font-medium text-foreground">{ban.cidr}</TableCell>
                        <TableCell className="text-muted-foreground">{ban.reason}</TableCell>
                        <TableCell className="text-muted-foreground">{formatRelativeTime(ban.created_at)}</TableCell>
                        <TableCell className="text-muted-foreground">
                          {ban.expires_at ? formatRelativeTime(ban.expires_at) : "Permanent"}
                        </TableCell>
                        <TableCell className="text-right">
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleRemoveIPBan(ban.id)}
                            className="h-7 px-2 text-destructive hover:bg-destructive/10 text-xs"
                          >
                            <Trash2 className="size-3 mr-1" />
                            <span>Unban</span>
                          </Button>
                        </TableCell>
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </TabsContent>

        {/* TAB 3: REALTIME ACTIVITY FEED */}
        <TabsContent value="activity" className="space-y-6">
          <Card className="border-border/80">
            <CardHeader className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between pb-3">
              <div>
                <CardTitle className="text-base font-semibold">Security Activity Feed & Audit Trail</CardTitle>
                <CardDescription className="text-xs">Real-time immutable log of admin and user actions</CardDescription>
              </div>

              {/* Filters */}
              <div className="flex flex-wrap items-center gap-2">
                <div className="relative">
                  <Search className="size-3.5 absolute left-2.5 top-2.5 text-muted-foreground" />
                  <input
                    type="text"
                    value={activitySearchQuery}
                    onChange={(e) => setActivitySearchQuery(e.target.value)}
                    placeholder="Search logs by action, IP, resource..."
                    className="pl-8 pr-3 py-1 text-xs rounded-lg border border-border bg-background text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-primary w-48 sm:w-64"
                  />
                </div>

                <select
                  value={activityFilterType}
                  onChange={(e) => setActivityFilterType(e.target.value)}
                  className="px-2.5 py-1 text-xs rounded-lg border border-border bg-background text-foreground focus:outline-none cursor-pointer"
                >
                  <option value="all">All Actions</option>
                  <option value="auth">Auth & Logins</option>
                  <option value="security">Security & Bans</option>
                  <option value="create">Creations</option>
                  <option value="delete">Deletions & Revocations</option>
                </select>
              </div>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow className="text-xs">
                    <TableHead>Action</TableHead>
                    <TableHead>Resource</TableHead>
                    <TableHead>IP Address</TableHead>
                    <TableHead>Timestamp</TableHead>
                    <TableHead className="text-right">Inspect</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {filteredActivityItems.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={5} className="text-center text-xs text-muted-foreground py-8">
                        No activity events match your filter.
                      </TableCell>
                    </TableRow>
                  ) : (
                    filteredActivityItems.map((item) => {
                      const isFailure =
                        item.action.toLowerCase().includes("fail") ||
                        item.action.toLowerCase().includes("denied") ||
                        item.action.toLowerCase().includes("reject")
                      const isSuccess =
                        item.action.toLowerCase().includes("success") ||
                        item.action.toLowerCase().includes("create") ||
                        item.action.toLowerCase().includes("login")

                      return (
                        <TableRow
                          key={item.id}
                          onClick={() => setSelectedAuditItem(item)}
                          className="text-xs cursor-pointer hover:bg-muted/60 transition-colors"
                        >
                          <TableCell>
                            <div className="flex items-center gap-2">
                              <span
                                className={`w-2 h-2 rounded-full shrink-0 ${
                                  isFailure ? "bg-destructive" : isSuccess ? "bg-emerald-500" : "bg-blue-500"
                                }`}
                              />
                              <span className="font-mono font-medium text-foreground">{item.action}</span>
                            </div>
                          </TableCell>
                          <TableCell className="capitalize text-muted-foreground">
                            {item.resource_type || "general"}
                          </TableCell>
                          <TableCell className="font-mono text-muted-foreground">{item.ip_address || "127.0.0.1"}</TableCell>
                          <TableCell className="text-muted-foreground">{formatRelativeTime(item.created_at)}</TableCell>
                          <TableCell className="text-right">
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={(e) => {
                                e.stopPropagation()
                                setSelectedAuditItem(item)
                              }}
                              className="h-6 px-2 text-xs text-muted-foreground hover:text-foreground"
                            >
                              <Eye className="size-3 mr-1" />
                              <span>View JSON</span>
                            </Button>
                          </TableCell>
                        </TableRow>
                      )
                    })
                  )}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </TabsContent>

        {/* TAB 4: SYSTEM TELEMETRY & MODULE HEALTH */}
        <TabsContent value="telemetry" className="space-y-6">
          {/* Subsystem Health Grid */}
          <Card className="border-border/80">
            <CardHeader className="pb-3">
              <CardTitle className="text-base font-semibold">Subsystem Health Probes & Latencies</CardTitle>
              <CardDescription className="text-xs">
                Live heartbeat probes and endpoint response times
              </CardDescription>
            </CardHeader>
            <CardContent>
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
                {healthChecks.map((check) => (
                  <div key={check.key} className="p-4 rounded-xl border border-border bg-card/60 space-y-2">
                    <div className="flex items-center justify-between">
                      <span className="font-semibold text-sm text-foreground">{check.label}</span>
                      <Badge variant={healthStatusVariant(check.status)} className="text-[10px] uppercase">
                        {check.status}
                      </Badge>
                    </div>
                    <div className="text-xs font-mono text-muted-foreground truncate">{check.endpoint}</div>
                    <div className="flex items-center justify-between text-xs text-muted-foreground pt-1 border-t border-border/50">
                      <span className="flex items-center gap-1">
                        <Clock className="size-3" />
                        {check.response_time_ms} ms
                      </span>
                      <span className="truncate max-w-36 text-right">{check.message}</span>
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>

          {/* OpenTelemetry & Observability Stack */}
          {observabilityQuery.data && (
            <Card className="border-border/80">
              <CardHeader className="pb-3">
                <CardTitle className="text-base font-semibold">OpenTelemetry Pipeline Configuration</CardTitle>
                <CardDescription className="text-xs">
                  Distributed tracing, metrics collectors, and logging telemetry
                </CardDescription>
              </CardHeader>
              <CardContent>
                <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
                  <div className="p-3 rounded-lg border border-border bg-muted/20 space-y-1">
                    <div className="text-xs text-muted-foreground font-semibold">Traces Exporter (Tempo)</div>
                    <div className="text-sm font-mono text-foreground truncate">
                      {observabilityQuery.data.telemetry?.traces_endpoint || "http://localhost:4318/v1/traces"}
                    </div>
                    <Badge variant={observabilityQuery.data.telemetry?.traces_enabled ? "default" : "secondary"} className="text-[10px]">
                      {observabilityQuery.data.telemetry?.traces_enabled ? "Active Exporter" : "Disabled"}
                    </Badge>
                  </div>

                  <div className="p-3 rounded-lg border border-border bg-muted/20 space-y-1">
                    <div className="text-xs text-muted-foreground font-semibold">Metrics Exporter (Prometheus)</div>
                    <div className="text-sm font-mono text-foreground truncate">
                      {observabilityQuery.data.telemetry?.metrics_endpoint || "http://localhost:9090"}
                    </div>
                    <Badge variant={observabilityQuery.data.telemetry?.metrics_enabled ? "default" : "secondary"} className="text-[10px]">
                      {observabilityQuery.data.telemetry?.metrics_enabled ? "Active Exporter" : "Disabled"}
                    </Badge>
                  </div>

                  <div className="p-3 rounded-lg border border-border bg-muted/20 space-y-1">
                    <div className="text-xs text-muted-foreground font-semibold">Events Pipeline (Loza)</div>
                    <div className="text-sm font-mono text-foreground truncate">
                      {observabilityQuery.data.telemetry?.events_endpoint || "http://loza-collector:9308/events"}
                    </div>
                    <Badge variant={observabilityQuery.data.telemetry?.events_enabled ? "default" : "secondary"} className="text-[10px]">
                      {observabilityQuery.data.telemetry?.events_enabled ? "Active Pipeline" : "Disabled"}
                    </Badge>
                  </div>
                </div>
              </CardContent>
            </Card>
          )}
        </TabsContent>

        {/* TAB 5: GOVERNANCE & ACCESS CONTROLS */}
        <TabsContent value="governance" className="space-y-6">
          <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
            <Card className="border-border/80">
              <CardHeader className="pb-2">
                <CardDescription className="text-xs">Active Operators Roster</CardDescription>
                <CardTitle className="text-2xl font-bold text-foreground">{operators.length}</CardTitle>
              </CardHeader>
              <CardContent className="text-xs text-muted-foreground">
                Administrators with access keys
              </CardContent>
            </Card>

            <Card className="border-border/80">
              <CardHeader className="pb-2">
                <CardDescription className="text-xs">Directory Load Per Admin</CardDescription>
                <CardTitle className="text-2xl font-bold text-foreground">
                  {operators.length > 0 ? ((stats?.total_identities ?? 0) / operators.length).toFixed(1) : "0.0"}
                </CardTitle>
              </CardHeader>
              <CardContent className="text-xs text-muted-foreground">
                Identities managed per operator
              </CardContent>
            </Card>

            <Card className="border-border/80">
              <CardHeader className="pb-2">
                <CardDescription className="text-xs">Session Capacity Factor</CardDescription>
                <CardTitle className="text-2xl font-bold text-foreground">
                  {operators.length > 0 ? ((stats?.active_sessions ?? 0) / operators.length).toFixed(1) : "0.0"}
                </CardTitle>
              </CardHeader>
              <CardContent className="text-xs text-muted-foreground">
                Concurrent sessions per operator
              </CardContent>
            </Card>
          </div>

          <Card className="border-border/80">
            <CardHeader className="flex flex-row items-center justify-between pb-3">
              <div>
                <CardTitle className="text-base font-semibold">Operator Roster & RBAC Roles</CardTitle>
                <CardDescription className="text-xs">Administrative accounts and permission tiers</CardDescription>
              </div>
              <Button asChild variant="outline" size="sm" className="text-xs h-7">
                <Link to="/operators">
                  <span>Manage Operators</span>
                  <ArrowRight className="size-3 ml-1" />
                </Link>
              </Button>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow className="text-xs">
                    <TableHead>Operator</TableHead>
                    <TableHead>Role</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead className="text-right">Last Login</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {operators.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={4} className="text-center text-xs text-muted-foreground py-6">
                        No operators available.
                      </TableCell>
                    </TableRow>
                  ) : (
                    operators.map((op: Operator) => (
                      <TableRow key={op.id} className="text-xs">
                        <TableCell>
                          <div className="flex flex-col">
                            <span className="font-medium text-foreground">{op.name || op.email}</span>
                            <span className="text-[11px] text-muted-foreground font-mono">{op.email}</span>
                          </div>
                        </TableCell>
                        <TableCell>
                          <Badge variant={operatorRoleVariant(op.role)} className="text-[10px]">
                            {op.role}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          <Badge variant={operatorStatusVariant(op.status)} className="text-[10px]">
                            {op.status}
                          </Badge>
                        </TableCell>
                        <TableCell className="text-right text-muted-foreground">
                          {op.last_login_at ? formatRelativeTime(op.last_login_at) : "Never"}
                        </TableCell>
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      {/* Modals & Dialogs */}
      <CommandPalette
        isOpen={isCommandPaletteOpen}
        onClose={() => setIsCommandPaletteOpen(false)}
        onOpenQuickBan={(ip) => {
          setQuickBanInitialIP(ip || "")
          setIsQuickBanOpen(true)
        }}
        onOpenExport={() => setIsExportModalOpen(true)}
        onTriggerRefresh={handleManualRefresh}
      />

      <QuickBanModal
        isOpen={isQuickBanOpen}
        onClose={() => setIsQuickBanOpen(false)}
        initialIP={quickBanInitialIP}
        onSuccess={() => {
          queryClient.invalidateQueries({ queryKey: ["dashboard-ip-bans"] })
          queryClient.invalidateQueries({ queryKey: ["dashboard-stats"] })
          queryClient.invalidateQueries({ queryKey: ["dashboard-security-posture"] })
        }}
      />

      <AuditDetailModal
        item={selectedAuditItem}
        isOpen={!!selectedAuditItem}
        onClose={() => setSelectedAuditItem(null)}
        onQuickBanIP={(ip) => {
          setSelectedAuditItem(null)
          setQuickBanInitialIP(ip)
          setIsQuickBanOpen(true)
        }}
      />

      <ExportSummaryModal
        isOpen={isExportModalOpen}
        onClose={() => setIsExportModalOpen(false)}
        stats={stats}
        securityPosture={posture}
        healthProbes={healthChecks}
        operators={operators}
        sessions={activeSessions}
      />
    </div>
  )
}
