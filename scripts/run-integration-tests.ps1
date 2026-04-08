# =============================================================================
# Aegion Integration Test Script - Comprehensive Validation
# Tests full stack deployment, module communication, and production readiness
# =============================================================================

$ErrorActionPreference = "Continue"
$script:TestsPassed = 0
$script:TestsFailed = 0

function Write-Section { param($Title) Write-Host "`n╔═══════════════════════════════════════════════════════════╗" -ForegroundColor Cyan; Write-Host "║  $Title" -ForegroundColor Cyan; Write-Host "╚═══════════════════════════════════════════════════════════╝" -ForegroundColor Cyan }
function Write-Test { param($Name) Write-Host "`n→ $Name" -ForegroundColor Yellow }
function Write-Pass { param($Message) Write-Host "  ✓ $Message" -ForegroundColor Green; $script:TestsPassed++ }
function Write-Fail { param($Message) Write-Host "  ✗ $Message" -ForegroundColor Red; $script:TestsFailed++ }
function Write-Info { param($Message) Write-Host "  ℹ $Message" -ForegroundColor Cyan }

# Cleanup function
function Cleanup {
    Write-Info "Cleaning up test environment..."
    docker compose -f deploy/docker-compose.yml down -v --remove-orphans 2>$null
    Remove-Item -Force -ErrorAction SilentlyContinue test-results.json
}

Write-Section "Aegion Integration Test Suite"
Write-Host "Started: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')" -ForegroundColor Cyan

# Pre-flight checks
Write-Section "Pre-Flight Checks"

Write-Test "Docker availability"
if (docker --version) {
    Write-Pass "Docker is installed and accessible"
} else {
    Write-Fail "Docker is not available"
    exit 1
}

Write-Test "Docker Compose availability"
if (docker compose version) {
    Write-Pass "Docker Compose is installed"
} else {
    Write-Fail "Docker Compose is not available"
    exit 1
}

Write-Test "Project structure"
if ((Test-Path "go.mod") -and (Test-Path "deploy/docker-compose.yml")) {
    Write-Pass "Project structure valid"
} else {
    Write-Fail "Must run from project root"
    exit 1
}

# Build phase
Write-Section "Build Phase"

Write-Test "Building Docker images"
$buildOutput = docker compose -f deploy/docker-compose.yml build --progress=plain 2>&1
if ($LASTEXITCODE -eq 0) {
    Write-Pass "All Docker images built successfully"
} else {
    Write-Fail "Docker build failed"
    Write-Host $buildOutput | Select-Object -Last 20
    exit 1
}

# Deployment phase
Write-Section "Deployment Phase"

Write-Test "Starting infrastructure services"
docker compose -f deploy/docker-compose.yml up -d postgres redis
Start-Sleep -Seconds 5

Write-Test "Waiting for PostgreSQL"
$timeout = 30
$ready = $false
for ($i = 1; $i -le $timeout; $i++) {
    $pgReady = docker compose -f deploy/docker-compose.yml exec -T postgres pg_isready -U aegion 2>&1
    if ($pgReady -match "accepting connections") {
        $ready = $true
        break
    }
    Start-Sleep -Seconds 1
}
if ($ready) {
    Write-Pass "PostgreSQL is ready"
} else {
    Write-Fail "PostgreSQL failed to start"
    docker compose -f deploy/docker-compose.yml logs postgres
    Cleanup
    exit 1
}

Write-Test "Waiting for Redis"
$timeout = 30
$ready = $false
for ($i = 1; $i -le $timeout; $i++) {
    $redisReady = docker compose -f deploy/docker-compose.yml exec -T redis redis-cli ping 2>&1
    if ($redisReady -match "PONG") {
        $ready = $true
        break
    }
    Start-Sleep -Seconds 1
}
if ($ready) {
    Write-Pass "Redis is ready"
} else {
    Write-Fail "Redis failed to start"
    docker compose -f deploy/docker-compose.yml logs redis
    Cleanup
    exit 1
}

Write-Test "Starting Aegion core"
docker compose -f deploy/docker-compose.yml up -d aegion
Start-Sleep -Seconds 10

Write-Test "Waiting for core service health"
$timeout = 60
$healthy = $false
for ($i = 1; $i -le $timeout; $i++) {
    try {
        $response = Invoke-WebRequest -Uri "http://localhost:8080/health" -Method Get -UseBasicParsing -TimeoutSec 2 -ErrorAction SilentlyContinue
        if ($response.StatusCode -eq 200) {
            $healthy = $true
            break
        }
    } catch {
        # Keep trying
    }
    Start-Sleep -Seconds 1
}
if ($healthy) {
    Write-Pass "Core service is healthy"
} else {
    Write-Fail "Core service failed to become healthy"
    docker compose -f deploy/docker-compose.yml logs aegion | Select-Object -Last 50
    Cleanup
    exit 1
}

# Core API tests
Write-Section "Core API Tests"

Write-Test "Health endpoint"
try {
    $response = Invoke-WebRequest -Uri "http://localhost:8080/health" -Method Get -UseBasicParsing
    if ($response.StatusCode -eq 200) {
        Write-Pass "Health endpoint returns 200"
    } else {
        Write-Fail "Health endpoint returned $($response.StatusCode)"
    }
} catch {
    Write-Fail "Health endpoint failed: $_"
}

Write-Test "Meta endpoint"
try {
    $response = Invoke-WebRequest -Uri "http://localhost:8080/.well-known/aegion/meta" -Method Get -UseBasicParsing
    if ($response.StatusCode -eq 200) {
        Write-Pass "Meta endpoint returns 200"
        $meta = $response.Content | ConvertFrom-Json
        Write-Info "Version: $($meta.version)"
    } else {
        Write-Fail "Meta endpoint returned $($response.StatusCode)"
    }
} catch {
    Write-Fail "Meta endpoint failed: $_"
}

Write-Test "OpenID configuration"
try {
    $response = Invoke-WebRequest -Uri "http://localhost:8080/.well-known/openid-configuration" -Method Get -UseBasicParsing
    if ($response.StatusCode -eq 200) {
        Write-Pass "OpenID configuration available"
    } else {
        Write-Fail "OpenID configuration returned $($response.StatusCode)"
    }
} catch {
    Write-Fail "OpenID configuration failed: $_"
}

Write-Test "JWKS endpoint"
try {
    $response = Invoke-WebRequest -Uri "http://localhost:8080/.well-known/jwks.json" -Method Get -UseBasicParsing
    if ($response.StatusCode -eq 200) {
        Write-Pass "JWKS endpoint returns keys"
    } else {
        Write-Fail "JWKS endpoint returned $($response.StatusCode)"
    }
} catch {
    Write-Fail "JWKS endpoint failed: $_"
}

# Module tests (if standalone modules are running)
Write-Section "Module Tests"

# Check if admin module is defined in docker-compose
$composeContent = Get-Content deploy/docker-compose.yml -Raw
if ($composeContent -match "module-admin:" -and $composeContent -notmatch "#.*module-admin:") {
    Write-Test "Starting Admin module"
    docker compose -f deploy/docker-compose.yml up -d module-admin
    Start-Sleep -Seconds 10
    
    try {
        $response = Invoke-WebRequest -Uri "http://localhost:9003/health" -Method Get -UseBasicParsing -TimeoutSec 5
        if ($response.StatusCode -eq 200) {
            Write-Pass "Admin module is healthy"
        } else {
            Write-Fail "Admin module returned $($response.StatusCode)"
        }
    } catch {
        Write-Fail "Admin module health check failed: $_"
    }
} else {
    Write-Info "Admin module is embedded in core (not standalone)"
}

# Check if OAuth2 module is defined
if ($composeContent -match "module-oauth2:" -and $composeContent -notmatch "#.*module-oauth2:") {
    Write-Test "Starting OAuth2 module"
    docker compose -f deploy/docker-compose.yml up -d module-oauth2
    Start-Sleep -Seconds 10
    
    try {
        $response = Invoke-WebRequest -Uri "http://localhost:9005/health" -Method Get -UseBasicParsing -TimeoutSec 5
        if ($response.StatusCode -eq 200) {
            Write-Pass "OAuth2 module is healthy"
        } else {
            Write-Fail "OAuth2 module returned $($response.StatusCode)"
        }
    } catch {
        Write-Fail "OAuth2 module health check failed: $_"
    }
} else {
    Write-Info "OAuth2 module not configured for standalone deployment"
}

# Security tests
Write-Section "Security Validation"

Write-Test "TLS redirect (should fail on HTTP)"
try {
    $response = Invoke-WebRequest -Uri "http://localhost:8080/" -Method Get -UseBasicParsing -MaximumRedirection 0 -ErrorAction SilentlyContinue
    # In production, this should redirect to HTTPS
    Write-Info "HTTP accessible (expected in development)"
} catch {
    Write-Info "HTTP properly blocked or redirected"
}

Write-Test "Non-root container user (core)"
$userId = docker compose -f deploy/docker-compose.yml exec -T aegion id -u 2>&1
if ($userId -match "1000" -or $userId -match "65532") {
    Write-Pass "Core runs as non-root user (UID: $userId)"
} else {
    Write-Fail "Core may be running as root (UID: $userId)"
}

Write-Test "Secrets not exposed in environment"
$env = docker compose -f deploy/docker-compose.yml exec -T aegion env 2>&1
$secretsExposed = $env | Select-String -Pattern "(POSTGRES_PASSWORD|SECRET.*=.{20,})" -NotMatch -Pattern "CHANGE|xxx"
if (-not $secretsExposed) {
    Write-Pass "No hardcoded secrets in environment"
} else {
    Write-Fail "Potential secrets exposed in environment"
}

# Performance tests
Write-Section "Performance Tests"

Write-Test "Response time measurement"
$measurements = @()
for ($i = 1; $i -le 10; $i++) {
    $start = Get-Date
    try {
        $response = Invoke-WebRequest -Uri "http://localhost:8080/health" -Method Get -UseBasicParsing -TimeoutSec 5
        $end = Get-Date
        $duration = ($end - $start).TotalMilliseconds
        $measurements += $duration
    } catch {
        Write-Fail "Request $i failed"
    }
}

if ($measurements.Count -gt 0) {
    $avg = ($measurements | Measure-Object -Average).Average
    $max = ($measurements | Measure-Object -Maximum).Maximum
    $min = ($measurements | Measure-Object -Minimum).Minimum
    
    Write-Pass "Average response time: $([math]::Round($avg, 2))ms (min: $([math]::Round($min, 2))ms, max: $([math]::Round($max, 2))ms)"
    
    if ($avg -lt 100) {
        Write-Pass "Performance: Excellent (< 100ms)"
    } elseif ($avg -lt 500) {
        Write-Pass "Performance: Good (< 500ms)"
    } else {
        Write-Info "Performance: Acceptable (> 500ms)"
    }
}

# Resource usage tests
Write-Section "Resource Usage"

Write-Test "Container resource consumption"
$stats = docker stats --no-stream --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}" 2>&1
Write-Info "Container stats:"
Write-Host $stats

# Logs inspection
Write-Section "Log Inspection"

Write-Test "Checking for errors in logs"
$logs = docker compose -f deploy/docker-compose.yml logs aegion 2>&1 | Select-Object -Last 100
$errors = $logs | Select-String -Pattern "ERROR|FATAL|panic"
if ($errors.Count -eq 0) {
    Write-Pass "No errors found in core logs"
} else {
    Write-Fail "Found $($errors.Count) error(s) in logs"
    $errors | Select-Object -First 5 | ForEach-Object { Write-Host "  $_" -ForegroundColor Red }
}

# Cleanup
Write-Section "Cleanup"
Cleanup
Write-Pass "Test environment cleaned up"

# Summary
Write-Section "Test Summary"
Write-Host ""
Write-Host "Tests Passed: $script:TestsPassed" -ForegroundColor Green
Write-Host "Tests Failed: $script:TestsFailed" -ForegroundColor Red
Write-Host "Total Tests: $($script:TestsPassed + $script:TestsFailed)" -ForegroundColor Cyan
Write-Host ""
Write-Host "Completed: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')" -ForegroundColor Cyan

if ($script:TestsFailed -eq 0) {
    Write-Host "`n✓ All integration tests passed!" -ForegroundColor Green
    exit 0
} else {
    Write-Host "`n✗ Some integration tests failed" -ForegroundColor Red
    exit 1
}
