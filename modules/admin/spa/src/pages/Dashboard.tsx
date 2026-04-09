import { useMemo } from "react"
import { Link } from "react-router-dom"
import { useQuery } from "@tanstack/react-query"
import {
  Activity,
  AlertCircle,
  ArrowRight,
  ExternalLink,
  Gauge,
  HeartPulse,
  Server,
  Settings,
  ShieldCheck,
  UserCog,
  UserPlus,
  Users,
} from "lucide-react"
import { Bar, BarChart, CartesianGrid, XAxis } from "recharts"

import { identitiesApi } from "../api/identities"
import { dashboardApi, operatorsApi } from "../api/operators"
import { sessionsApi } from "../api/sessions"
import { useAuth } from "../hooks/useAuth"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart"
import { Progress } from "@/components/ui/progress"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { healthStatusVariant, identityStatusVariant, operatorRoleVariant, operatorStatusVariant } from "@/lib/status"
import { operatorHasPermission } from "@/lib/permissions"

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
  if (userAgent.includes("Safari")) return "Safari"
  if (userAgent.includes("Edge")) return "Edge"
  return "Unknown client"
}

const chartConfig = {
  identities: {
    label: "Identities",
    color: "var(--chart-1)",
  },
  sessions: {
    label: "Sessions",
    color: "var(--chart-2)",
  },
  newUsers: {
    label: "New 24h",
    color: "var(--chart-3)",
  },
} satisfies ChartConfig

const getRiskBadgeVariant = (score: number): "success" | "warning" | "destructive" => {
  if (score <= 30) return "success"
  if (score <= 60) return "warning"
  return "destructive"
}

export function Dashboard() {
  const { operator } = useAuth()
  const canReadConfig = operatorHasPermission(operator, "config:read")

  const statsQuery = useQuery({
    queryKey: ["dashboard-stats"],
    queryFn: dashboardApi.getStats,
    staleTime: 60000,
    refetchInterval: 60000,
  })

  const configQuery = useQuery({
    queryKey: ["dashboard-config"],
    queryFn: dashboardApi.getConfig,
    enabled: canReadConfig,
    staleTime: 5 * 60 * 1000,
  })

  const healthQuery = useQuery({
    queryKey: ["dashboard-health"],
    queryFn: dashboardApi.getHealth,
    staleTime: 15000,
    refetchInterval: 30000,
  })

  const observabilityQuery = useQuery({
    queryKey: ["dashboard-observability"],
    queryFn: dashboardApi.getObservability,
    staleTime: 15000,
    refetchInterval: 30000,
  })

  const recentIdentitiesQuery = useQuery({
    queryKey: ["dashboard-recent-identities"],
    queryFn: () => identitiesApi.list({ page: 1, per_page: 6 }),
    staleTime: 30000,
    refetchInterval: 60000,
  })

  const recentSessionsQuery = useQuery({
    queryKey: ["dashboard-recent-sessions"],
    queryFn: () => sessionsApi.list({ page: 1, per_page: 6 }),
    staleTime: 30000,
    refetchInterval: 60000,
  })

  const operatorsQuery = useQuery({
    queryKey: ["dashboard-operators"],
    queryFn: () => operatorsApi.list(1, 50),
    staleTime: 60000,
    refetchInterval: 120000,
  })

  const stats = statsQuery.data
  const recentIdentities = recentIdentitiesQuery.data?.data ?? []
  const recentSessions = recentSessionsQuery.data?.data ?? []
  const operators = operatorsQuery.data?.data ?? []
  const healthChecks = healthQuery.data ?? []
  const observabilityChecks = observabilityQuery.data ?? []

  const totalIdentities = stats?.total_identities ?? 0
  const activeSessions = stats?.active_sessions ?? 0
  const identitiesLast24h = stats?.identities_last_24h ?? 0
  const mfaRate = stats?.mfa_adoption_rate ?? 0

  const chartData = useMemo(
    () => [
      {
        category: "Identity",
        identities: totalIdentities,
        sessions: activeSessions,
        newUsers: identitiesLast24h,
      },
      {
        category: "Target",
        identities: Math.max(totalIdentities, 100),
        sessions: Math.max(Math.floor(totalIdentities * 0.9), 50),
        newUsers: Math.max(Math.floor(totalIdentities * 0.05), 10),
      },
    ],
    [totalIdentities, activeSessions, identitiesLast24h]
  )

  const isLoading =
    statsQuery.isLoading ||
    recentIdentitiesQuery.isLoading ||
    recentSessionsQuery.isLoading ||
    operatorsQuery.isLoading

  const primaryError =
    statsQuery.error ||
    recentIdentitiesQuery.error ||
    recentSessionsQuery.error ||
    operatorsQuery.error

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-5">
          {Array.from({ length: 5 }).map((_, idx) => (
            <Card key={idx}>
              <CardHeader>
                <Skeleton className="h-4 w-24" />
              </CardHeader>
              <CardContent>
                <Skeleton className="h-7 w-20" />
              </CardContent>
            </Card>
          ))}
        </div>
        <Card>
          <CardContent className="pt-6">
            <Skeleton className="h-64 w-full" />
          </CardContent>
        </Card>
      </div>
    )
  }

  if (primaryError) {
    return (
      <Alert variant="destructive">
        <AlertCircle className="size-4" />
        <AlertTitle>Dashboard query failed</AlertTitle>
        <AlertDescription>
          One or more critical observability queries failed. Check API connectivity and retry.
        </AlertDescription>
      </Alert>
    )
  }

  const sessionPressure = totalIdentities > 0 ? activeSessions / totalIdentities : 0
  const riskScore = Math.min(100, Math.round((100 - mfaRate) * 0.55 + sessionPressure * 22))
  const securityLabel = riskScore <= 30 ? "Strong" : riskScore <= 60 ? "Moderate" : "Needs attention"

  const totalOperators = operatorsQuery.data?.total ?? operators.length
  const activeOperators = operators.filter((item) => item.status === "active").length
  const adminOperators = operators.filter((item) => item.role === "admin").length
  const healthyChecks = healthChecks.filter((item) => item.status === "healthy").length
  const unhealthyChecks = healthChecks.filter((item) => item.status !== "healthy").length
  const healthyObservabilityChecks = observabilityChecks.filter((item) => item.status === "healthy").length

  const observabilityRows = [
    {
      metric: "Identity Inventory",
      value: totalIdentities.toLocaleString(),
      trend: identitiesLast24h > 0 ? `+${identitiesLast24h} in 24h` : "No change",
    },
    {
      metric: "Session Pressure",
      value: sessionPressure.toFixed(2),
      trend: `${activeSessions.toLocaleString()} active sessions`,
    },
    {
      metric: "MFA Coverage",
      value: `${mfaRate.toFixed(1)}%`,
      trend: mfaRate >= 80 ? "Healthy" : "Needs adoption uplift",
    },
    {
      metric: "Operator Availability",
      value: `${activeOperators}/${totalOperators}`,
      trend: `${adminOperators} admins on duty`,
    },
    {
      metric: "Observability Stack",
      value:
        observabilityChecks.length > 0
          ? `${healthyObservabilityChecks}/${observabilityChecks.length}`
          : "Disabled",
      trend: observabilityQuery.error ? "Probe fetch failed" : "Grafana, Prometheus, Tempo, Loki",
    },
  ]

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-2">
        <h1 className="text-2xl font-semibold tracking-tight">Admin Observability Dashboard</h1>
        <p className="text-sm text-muted-foreground">
          Unified visibility for identities, sessions, operators, and module health. Refreshes automatically.
        </p>
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-5">
        <Card>
          <CardHeader className="pb-2">
            <CardDescription>Total identities</CardDescription>
            <CardTitle className="text-2xl">{totalIdentities.toLocaleString()}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2 text-muted-foreground">
              <Users className="size-4" />
              <span className="text-xs">Directory inventory</span>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardDescription>Active sessions</CardDescription>
            <CardTitle className="text-2xl">{activeSessions.toLocaleString()}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2 text-muted-foreground">
              <Activity className="size-4" />
              <span className="text-xs">Live traffic indicator</span>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardDescription>New identities (24h)</CardDescription>
            <CardTitle className="text-2xl">{identitiesLast24h.toLocaleString()}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2 text-muted-foreground">
              <UserPlus className="size-4" />
              <span className="text-xs">Acquisition pulse</span>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardDescription>Operator coverage</CardDescription>
            <CardTitle className="text-2xl">{activeOperators}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2 text-muted-foreground">
              <UserCog className="size-4" />
              <span className="text-xs">{totalOperators} total operators</span>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardDescription>Security risk score</CardDescription>
            <CardTitle className="text-2xl">{riskScore}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <Progress value={Math.max(0, 100 - riskScore)} />
            <Badge variant={getRiskBadgeVariant(riskScore)}>{securityLabel}</Badge>
          </CardContent>
        </Card>
      </div>

      {healthQuery.error && (
        <Alert variant="warning">
          <AlertCircle className="size-4" />
          <AlertTitle>Health telemetry degraded</AlertTitle>
          <AlertDescription>
            Module health probes are currently failing. Core dashboard metrics are still available.
          </AlertDescription>
        </Alert>
      )}

      <Tabs defaultValue="overview" className="w-full">
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="telemetry">Telemetry</TabsTrigger>
          <TabsTrigger value="health">Module Health</TabsTrigger>
          <TabsTrigger value="users">User Management</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="space-y-6">
          <div className="grid grid-cols-1 gap-6 xl:grid-cols-3">
            <Card className="xl:col-span-2">
              <CardHeader>
                <CardTitle>Platform Signal Mix</CardTitle>
                <CardDescription>Current load against baseline target values.</CardDescription>
              </CardHeader>
              <CardContent>
                <ChartContainer config={chartConfig} className="h-72 w-full">
                  <BarChart accessibilityLayer data={chartData}>
                    <CartesianGrid vertical={false} />
                    <XAxis dataKey="category" tickLine={false} tickMargin={10} axisLine={false} />
                    <ChartTooltip content={<ChartTooltipContent />} />
                    <Bar dataKey="identities" fill="var(--color-identities)" radius={8} />
                    <Bar dataKey="sessions" fill="var(--color-sessions)" radius={8} />
                    <Bar dataKey="newUsers" fill="var(--color-newUsers)" radius={8} />
                  </BarChart>
                </ChartContainer>
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>Security Posture</CardTitle>
                <CardDescription>
                  MFA adoption and session pressure are fused into one risk signal.
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="space-y-1">
                  <p className="text-xs text-muted-foreground">MFA adoption</p>
                  <p className="text-lg font-medium">{mfaRate.toFixed(1)}%</p>
                </div>
                <Separator />
                <div className="space-y-1">
                  <p className="text-xs text-muted-foreground">Session pressure</p>
                  <p className="text-lg font-medium">{sessionPressure.toFixed(2)} sessions/identity</p>
                </div>
                <Separator />
                <div className="space-y-2">
                  <p className="text-xs text-muted-foreground">Recommended actions</p>
                  <div className="grid gap-2">
                    <Button asChild variant="outline" className="justify-between">
                      <Link to="/sessions">
                        Review active sessions
                        <ArrowRight className="size-4" />
                      </Link>
                    </Button>
                    <Button asChild variant="outline" className="justify-between">
                      <Link to="/settings">
                        Tune policy thresholds
                        <Settings className="size-4" />
                      </Link>
                    </Button>
                  </div>
                </div>
              </CardContent>
            </Card>
          </div>

          <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle>Recent Identities</CardTitle>
                <CardDescription>Newest records entering the platform.</CardDescription>
              </CardHeader>
              <CardContent>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Identity</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead className="text-right">Created</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {recentIdentities.length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={3} className="text-center text-muted-foreground">
                          No recent signups to display.
                        </TableCell>
                      </TableRow>
                    ) : (
                      recentIdentities.map((identity) => (
                        <TableRow key={identity.id}>
                          <TableCell>
                            <div className="flex flex-col">
                              <span className="font-medium">{identity.display_name || identity.email}</span>
                              <span className="text-xs text-muted-foreground">{identity.email}</span>
                            </div>
                          </TableCell>
                          <TableCell>
                            <Badge variant={identityStatusVariant(identity.status)}>{identity.status}</Badge>
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

            <Card>
              <CardHeader>
                <CardTitle>Recent Sessions</CardTitle>
                <CardDescription>Realtime client and endpoint activity.</CardDescription>
              </CardHeader>
              <CardContent>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Client</TableHead>
                      <TableHead>IP</TableHead>
                      <TableHead className="text-right">Last Active</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {recentSessions.length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={3} className="text-center text-muted-foreground">
                          No active sessions to display.
                        </TableCell>
                      </TableRow>
                    ) : (
                      recentSessions.map((session) => (
                        <TableRow key={session.id}>
                          <TableCell>
                            <div className="flex items-center gap-2">
                              <UserCog className="size-4 text-muted-foreground" />
                              <span>{parseUserAgent(session.user_agent)}</span>
                            </div>
                          </TableCell>
                          <TableCell>{session.ip_address || "Unknown"}</TableCell>
                          <TableCell className="text-right text-muted-foreground">
                            {formatRelativeTime(session.last_active_at)}
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

        <TabsContent value="telemetry">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Gauge className="size-4" />
                Observability Metrics
              </CardTitle>
              <CardDescription>Primary operational signals and health interpretation.</CardDescription>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Metric</TableHead>
                    <TableHead>Value</TableHead>
                    <TableHead>Interpretation</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {observabilityRows.map((row) => (
                    <TableRow key={row.metric}>
                      <TableCell className="font-medium">{row.metric}</TableCell>
                      <TableCell>{row.value}</TableCell>
                      <TableCell className="text-muted-foreground">{row.trend}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>

              <Separator className="my-6" />

              <div className="space-y-3">
                <div className="flex items-center justify-between">
                  <p className="text-sm font-medium">Integrated Observability Stack</p>
                  {observabilityQuery.error && <Badge variant="warning">Probe fetch failed</Badge>}
                </div>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Service</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead>Latency</TableHead>
                      <TableHead>Endpoint</TableHead>
                      <TableHead className="text-right">Message</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {observabilityChecks.length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={5} className="text-center text-muted-foreground">
                          Observability probes are disabled or unavailable.
                        </TableCell>
                      </TableRow>
                    ) : (
                      observabilityChecks.map((check) => (
                        <TableRow key={check.key}>
                          <TableCell className="font-medium">{check.label}</TableCell>
                          <TableCell>
                            <Badge variant={healthStatusVariant(check.status)}>{check.status}</Badge>
                          </TableCell>
                          <TableCell>{check.response_time_ms} ms</TableCell>
                          <TableCell>
                            {check.url ? (
                              <a
                                href={check.url}
                                target="_blank"
                                rel="noreferrer"
                                className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                              >
                                Open
                                <ExternalLink className="size-3" />
                              </a>
                            ) : (
                              <span className="text-xs text-muted-foreground">—</span>
                            )}
                          </TableCell>
                          <TableCell className="max-w-48 truncate text-right text-muted-foreground">
                            {check.message}
                          </TableCell>
                        </TableRow>
                      ))
                    )}
                  </TableBody>
                </Table>
              </div>

              <div className="mt-6 rounded-lg border bg-muted/30 p-4">
                <div className="flex items-center gap-2 text-sm font-medium">
                  <ShieldCheck className="size-4" />
                  Security Summary
                </div>
                <p className="mt-2 text-sm text-muted-foreground">
                  Current risk score is <span className="font-medium text-foreground">{riskScore}</span>. Keep
                  MFA above 80% and session pressure below 1.5 to remain in a strong posture range.
                </p>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="health" className="space-y-6">
          <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
            <Card>
              <CardHeader className="pb-2">
                <CardDescription>Healthy checks</CardDescription>
                <CardTitle className="text-2xl">{healthyChecks}</CardTitle>
              </CardHeader>
              <CardContent className="text-xs text-muted-foreground">
                <div className="flex items-center gap-2">
                  <HeartPulse className="size-4" />
                  Automated service and readiness probes
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="pb-2">
                <CardDescription>Degraded checks</CardDescription>
                <CardTitle className="text-2xl">{unhealthyChecks}</CardTitle>
              </CardHeader>
              <CardContent className="text-xs text-muted-foreground">
                <div className="flex items-center gap-2">
                  <Server className="size-4" />
                  Includes timeout and non-200 responses
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="pb-2">
                <CardDescription>Admin base path</CardDescription>
                <CardTitle className="text-2xl">{configQuery.data?.base_path ?? "unknown"}</CardTitle>
              </CardHeader>
              <CardContent className="text-xs text-muted-foreground">
                <div className="flex items-center gap-2">
                  <Settings className="size-4" />
                  Path configuration from /api/admin/dashboard/config
                </div>
              </CardContent>
            </Card>
          </div>

          <Card>
            <CardHeader>
              <CardTitle>Module Health Matrix</CardTitle>
              <CardDescription>Realtime health probes from admin service endpoints.</CardDescription>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Check</TableHead>
                    <TableHead>Endpoint</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Latency</TableHead>
                    <TableHead className="text-right">Message</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {healthChecks.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={5} className="text-center text-muted-foreground">
                        No health probes available.
                      </TableCell>
                    </TableRow>
                  ) : (
                    healthChecks.map((check) => (
                      <TableRow key={check.key}>
                        <TableCell className="font-medium">{check.label}</TableCell>
                        <TableCell>{check.endpoint}</TableCell>
                        <TableCell>
                          <Badge variant={healthStatusVariant(check.status)}>{check.status}</Badge>
                        </TableCell>
                        <TableCell>{check.response_time_ms} ms</TableCell>
                        <TableCell className="max-w-48 truncate text-right text-muted-foreground">
                          {check.message}
                        </TableCell>
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="users" className="space-y-6">
          <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
            <Card>
              <CardHeader className="pb-2">
                <CardDescription>Operator roster</CardDescription>
                <CardTitle className="text-2xl">{totalOperators}</CardTitle>
              </CardHeader>
              <CardContent className="text-xs text-muted-foreground">
                Active: {activeOperators} • Admins: {adminOperators}
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="pb-2">
                <CardDescription>Identity to operator ratio</CardDescription>
                <CardTitle className="text-2xl">
                  {totalOperators > 0 ? (totalIdentities / totalOperators).toFixed(1) : "0.0"}
                </CardTitle>
              </CardHeader>
              <CardContent className="text-xs text-muted-foreground">
                Directory size pressure per operator
              </CardContent>
            </Card>
            <Card>
              <CardHeader className="pb-2">
                <CardDescription>Session load per operator</CardDescription>
                <CardTitle className="text-2xl">
                  {activeOperators > 0 ? (activeSessions / activeOperators).toFixed(1) : "0.0"}
                </CardTitle>
              </CardHeader>
              <CardContent className="text-xs text-muted-foreground">
                Concurrent sessions owned by active operators
              </CardContent>
            </Card>
          </div>

          <Card>
            <CardHeader>
              <CardTitle>User Management Controls</CardTitle>
              <CardDescription>Manage identities, sessions, and operator access controls.</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex flex-wrap gap-2">
                <Button asChild variant="outline">
                  <Link to="/identities">
                    <Users className="size-4" />
                    Open identities
                  </Link>
                </Button>
                <Button asChild variant="outline">
                  <Link to="/sessions">
                    <Activity className="size-4" />
                    Open sessions
                  </Link>
                </Button>
                <Button asChild variant="outline">
                  <Link to="/operators">
                    <UserCog className="size-4" />
                    Open operators
                  </Link>
                </Button>
              </div>

              <Separator />

              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Operator</TableHead>
                    <TableHead>Role</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead className="text-right">Last Login</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {operators.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={4} className="text-center text-muted-foreground">
                        No operators available.
                      </TableCell>
                    </TableRow>
                  ) : (
                    operators.slice(0, 8).map((operator) => (
                      <TableRow key={operator.id}>
                        <TableCell>
                          <div className="flex flex-col">
                            <span className="font-medium">{operator.name}</span>
                            <span className="text-xs text-muted-foreground">{operator.email}</span>
                          </div>
                        </TableCell>
                        <TableCell>
                          <Badge variant={operatorRoleVariant(operator.role)}>{operator.role}</Badge>
                        </TableCell>
                        <TableCell>
                          <Badge variant={operatorStatusVariant(operator.status)}>{operator.status}</Badge>
                        </TableCell>
                        <TableCell className="text-right text-muted-foreground">
                          {operator.last_login_at ? formatRelativeTime(operator.last_login_at) : "Never"}
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
    </div>
  )
}
