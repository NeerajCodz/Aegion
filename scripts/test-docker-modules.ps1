# =============================================================================
# Aegion Module Docker Testing Script (PowerShell)
# Tests each module individually in Docker to verify modular approach
# =============================================================================

$ErrorActionPreference = "Stop"

# Test results tracking
$script:TestsPassed = 0
$script:TestsFailed = 0
$script:TestsSkipped = 0

# Colors
function Write-Success { param($Message) Write-Host "✓ $Message" -ForegroundColor Green; $script:TestsPassed++ }
function Write-Failure { param($Message) Write-Host "✗ $Message" -ForegroundColor Red; $script:TestsFailed++ }
function Write-Warning-Custom { param($Message) Write-Host "⚠ $Message" -ForegroundColor Yellow; $script:TestsSkipped++ }
function Write-Info { param($Message) Write-Host "[$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')] $Message" -ForegroundColor Cyan }

# Cleanup function
function Cleanup {
    Write-Info "Cleaning up containers and networks..."
    try {
        docker compose -f deploy\docker-compose.yml down -v --remove-orphans 2>$null
    } catch {
        # Ignore cleanup errors
    }
}

# Register cleanup on exit
$null = Register-EngineEvent -SourceIdentifier PowerShell.Exiting -Action { Cleanup }

# Test HTTP endpoint
function Test-HttpEndpoint {
    param(
        [string]$Url,
        [int]$ExpectedStatus = 200,
        [int]$MaxRetries = 30,
        [int]$RetryDelay = 2
    )
    
    Write-Info "Testing HTTP endpoint: $Url (expecting $ExpectedStatus)"
    
    for ($i = 1; $i -le $MaxRetries; $i++) {
        try {
            $response = Invoke-WebRequest -Uri $Url -Method Get -UseBasicParsing -TimeoutSec 5 -ErrorAction SilentlyContinue
            if ($response.StatusCode -eq $ExpectedStatus) {
                Write-Success "HTTP $Url returned $($response.StatusCode)"
                return $true
            }
        } catch {
            $statusCode = $_.Exception.Response.StatusCode.value__
        }
        
        if ($i -lt $MaxRetries) {
            Write-Host "  Retry $i/$MaxRetries..."
            Start-Sleep -Seconds $RetryDelay
        }
    }
    
    Write-Failure "HTTP $Url failed (expected $ExpectedStatus)"
    return $false
}

# Test container health
function Test-ContainerHealth {
    param(
        [string]$Container,
        [int]$MaxRetries = 30,
        [int]$RetryDelay = 2
    )
    
    Write-Info "Checking health of container: $Container"
    
    for ($i = 1; $i -le $MaxRetries; $i++) {
        try {
            $health = docker inspect --format='{{.State.Health.Status}}' $Container 2>$null
            
            if ($health -eq "healthy") {
                Write-Success "Container $Container is healthy"
                return $true
            } elseif ($health -eq "starting") {
                Write-Host "  Container starting... ($i/$MaxRetries)"
            } else {
                Write-Failure "Container $Container is $health"
                return $false
            }
        } catch {
            Write-Host "  Waiting for container... ($i/$MaxRetries)"
        }
        
        Start-Sleep -Seconds $RetryDelay
    }
    
    Write-Failure "Container $Container failed to become healthy"
    return $false
}

# Test non-root user
function Test-NonRootUser {
    param([string]$Container)
    
    Write-Info "Checking if $Container runs as non-root"
    
    try {
        $uid = docker exec $Container id -u 2>$null
        if ($uid -ne "0") {
            Write-Success "Container $Container runs as non-root user (UID: $uid)"
            return $true
        } else {
            Write-Failure "Container $Container runs as root!"
            return $false
        }
    } catch {
        Write-Failure "Could not check user for $Container"
        return $false
    }
}

# Test secrets not exposed
function Test-NoSecretsInEnv {
    param([string]$Container)
    
    Write-Info "Checking for hardcoded secrets in $Container"
    
    try {
        $env = docker exec $Container env 2>$null | Select-String -Pattern "(PASSWORD|SECRET|KEY|TOKEN)" | Select-String -NotMatch -Pattern "(CHANGE|xxx)"
        
        if ($env) {
            Write-Warning-Custom "Found environment variables with secret-like names in $Container"
            $env | ForEach-Object {
                $name = ($_ -split '=', 2)[0]
                if ($name) { Write-Host "  ${name}=<redacted>" }
            }
            return $false
        }
        
        Write-Success "No obvious secrets exposed in $Container environment"
        return $true
    } catch {
        Write-Warning-Custom "Could not check environment for $Container"
        return $false
    }
}

# Main test execution
function Main {
    Write-Info "Starting Aegion Docker Module Testing"
    Write-Info "======================================"
    
    # Ensure we're in the project root
    if (-not (Test-Path "go.mod") -or -not (Test-Path "deploy")) {
        Write-Failure "Must run from project root directory"
        exit 1
    }
    
    # Create .env file if it doesn't exist
    if (-not (Test-Path "deploy\.env")) {
        Write-Info "Creating .env file from template"
        Copy-Item "deploy\.env.example" "deploy\.env"
        
        # Generate secure secrets
        Write-Info "Generating secure secrets..."
        $cookieSecret = -join ((1..32) | ForEach-Object { [char](Get-Random -Minimum 33 -Maximum 126) })
        $cipherSecret = -join ((1..32) | ForEach-Object { [char](Get-Random -Minimum 33 -Maximum 126) })
        $internalSecret = -join ((1..32) | ForEach-Object { [char](Get-Random -Minimum 33 -Maximum 126) })
        
        # Update .env file
        $content = Get-Content "deploy\.env" -Raw
        $content = $content -replace "dev-cookie-secret-change-me-32chars!", $cookieSecret
        $content = $content -replace "dev-cipher-secret-change-me-32chars!", $cipherSecret
        $content = $content -replace "dev-internal-secret-change-me-32ch!", $internalSecret
        Set-Content "deploy\.env" -Value $content -NoNewline
    }
    
    # Build all Docker images first
    Write-Info "Building Docker images..."
    $buildResult = docker compose -f deploy\docker-compose.yml build 2>&1
    if ($LASTEXITCODE -ne 0) {
        Write-Failure "Failed to build Docker images"
        Write-Host $buildResult
        exit 1
    }
    Write-Success "All Docker images built successfully"
    
    # Start infrastructure (postgres, redis)
    Write-Info "Starting infrastructure services..."
    docker compose -f deploy\docker-compose.yml up -d postgres redis
    
    # Wait for infrastructure to be healthy
    Test-ContainerHealth "aegion-postgres-1"
    Test-ContainerHealth "aegion-redis-1"
    
    # Test 1: Core service
    Write-Host ""
    Write-Info "TEST 1: Core Service"
    Write-Info "===================="
    docker compose -f deploy\docker-compose.yml up -d aegion
    Test-ContainerHealth "aegion-aegion-1"
    Test-HttpEndpoint "http://localhost:8080/health"
    Test-HttpEndpoint "http://localhost:8080/.well-known/aegion/meta"
    Test-NonRootUser "aegion-aegion-1"
    
    # Test 2: Password module
    Write-Host ""
    Write-Info "TEST 2: Password Module"
    Write-Info "======================="
    docker compose -f deploy\docker-compose.yml up -d module-password
    Test-ContainerHealth "aegion-module-password-1"
    Test-HttpEndpoint "http://localhost:9001/health"
    Test-NonRootUser "aegion-module-password-1"
    
    # Test 3: Magic Link module
    Write-Host ""
    Write-Info "TEST 3: Magic Link Module"
    Write-Info "========================="
    docker compose -f deploy\docker-compose.yml up -d module-magic-link
    Test-ContainerHealth "aegion-module-magic-link-1"
    Test-HttpEndpoint "http://localhost:9002/health"
    Test-NonRootUser "aegion-module-magic-link-1"
    
    # Test 4: Admin module
    Write-Host ""
    Write-Info "TEST 4: Admin Module"
    Write-Info "===================="
    docker compose -f deploy\docker-compose.yml up -d module-admin
    Test-ContainerHealth "aegion-module-admin-1"
    Test-HttpEndpoint "http://localhost:9003/health"
    Test-NonRootUser "aegion-module-admin-1"
    
    # Test 5: Security hardening
    Write-Host ""
    Write-Info "TEST 5: Security Hardening"
    Write-Info "=========================="
    Test-NoSecretsInEnv "aegion-aegion-1"
    
    # Show container logs for debugging
    Write-Host ""
    Write-Info "Container Logs (last 20 lines each):"
    Write-Info "====================================="
    $containers = @("aegion-aegion-1", "aegion-module-password-1", "aegion-module-magic-link-1", "aegion-module-admin-1")
    foreach ($container in $containers) {
        $exists = docker ps --format '{{.Names}}' | Select-String -Pattern $container
        if ($exists) {
            Write-Info "--- $container ---"
            docker logs --tail 20 $container 2>&1
        }
    }
    
    # Summary
    Write-Host ""
    Write-Info "Test Summary"
    Write-Info "============"
    Write-Success "Tests passed: $($script:TestsPassed)"
    Write-Failure "Tests failed: $($script:TestsFailed)"
    Write-Warning-Custom "Tests skipped: $($script:TestsSkipped)"
    
    # Cleanup
    Cleanup
    
    if ($script:TestsFailed -gt 0) {
        Write-Failure "Some tests failed!"
        exit 1
    } else {
        Write-Success "All tests passed!"
        exit 0
    }
}

# Run main function
try {
    Main
} catch {
    Write-Failure "Unexpected error: $_"
    Cleanup
    exit 1
}
