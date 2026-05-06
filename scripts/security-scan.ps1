# =============================================================================
# Aegion Security Scanning Script
# Runs comprehensive security scans on codebase and containers
# =============================================================================

param(
    [bool]$FailOnIssues = $true
)

$ErrorActionPreference = "Continue"

$script:IssuesFound = 0
$script:ScansRun = 0
$script:FailOnIssues = $FailOnIssues
$script:GosecVersion = if ($env:AEGION_GOSEC_VERSION) { $env:AEGION_GOSEC_VERSION } else { "v2.25.0" }
$script:GovulncheckVersion = if ($env:AEGION_GOVULNCHECK_VERSION) { $env:AEGION_GOVULNCHECK_VERSION } else { "v1.1.4" }

if ($env:AEGION_SECURITY_SCAN_FAIL_ON_ISSUES -and $env:AEGION_SECURITY_SCAN_FAIL_ON_ISSUES.ToLowerInvariant() -in @("false", "0", "no")) {
    $script:FailOnIssues = $false
}

function Write-Section { param($Title) Write-Host "`n=== $Title ===" -ForegroundColor Cyan }
function Write-Issue { param($Message) Write-Host "[WARN] $Message" -ForegroundColor Yellow; $script:IssuesFound++ }
function Write-Pass { param($Message) Write-Host "[PASS] $Message" -ForegroundColor Green }
function Write-Info { param($Message) Write-Host "[INFO] $Message" -ForegroundColor Blue }

# Check if a command exists
function Test-Command {
    param([string]$Command)
    $null -ne (Get-Command $Command -ErrorAction SilentlyContinue)
}

# Scan 1: Go Security Checker (gosec)
function Scan-GoSecurity {
    Write-Section "Go Security Scan (gosec)"
    $script:ScansRun++
    
    if (-not (Test-Command "gosec")) {
        Write-Info "gosec not installed. Installing..."
        try {
            go install "github.com/securego/gosec/v2/cmd/gosec@$($script:GosecVersion)"
        } catch {
            Write-Issue "Failed to install gosec: $_"
            return
        }
    }
    
    Write-Info "Running gosec..."
    $gosecOutput = gosec -fmt=json -out=security-gosec.json ./... 2>&1
    
    if (Test-Path "security-gosec.json") {
        $results = Get-Content "security-gosec.json" | ConvertFrom-Json
        $issueCount = $results.Issues.Count
        
        if ($issueCount -gt 0) {
            Write-Issue "Found $issueCount security issues in Go code"
            $results.Issues | ForEach-Object {
                Write-Host "  [$($_.severity)] $($_.rule): $($_.details)"
                Write-Host "    File: $($_.file):$($_.line)"
            }
        } else {
            Write-Pass "No security issues found in Go code"
        }
    }
}

# Scan 2: Dependency vulnerabilities (govulncheck)
function Scan-Dependencies {
    Write-Section "Dependency Vulnerability Scan (govulncheck)"
    $script:ScansRun++
    
    if (-not (Test-Command "govulncheck")) {
        Write-Info "govulncheck not installed. Installing..."
        try {
            go install "golang.org/x/vuln/cmd/govulncheck@$($script:GovulncheckVersion)"
        } catch {
            Write-Issue "Failed to install govulncheck: $_"
            return
        }
    }
    
    Write-Info "Running govulncheck..."
    $vulnOutput = govulncheck ./... 2>&1 | Out-String
    $govulnExitCode = $LASTEXITCODE

    if ($vulnOutput -match "No vulnerabilities found") {
        Write-Pass "No known vulnerabilities in dependencies"
    } elseif ($vulnOutput -match "Your code is affected by") {
        Write-Issue "Found vulnerabilities in dependencies"
        Write-Host $vulnOutput
    } elseif ($govulnExitCode -ne 0) {
        Write-Issue "govulncheck failed to run (exit code $govulnExitCode)"
        Write-Host $vulnOutput
    } else {
        Write-Pass "No known vulnerabilities in dependencies"
    }
}

# Scan 3: Docker image vulnerabilities (trivy)
function Scan-DockerImages {
    Write-Section "Docker Image Vulnerability Scan (trivy)"
    $script:ScansRun++
    
    if (-not (Test-Command "trivy")) {
        Write-Issue "trivy not installed. Please install from: https://github.com/aquasecurity/trivy"
        return
    }
    
    $images = @(
        "aegion/core:latest",
        "aegion/module-password:latest",
        "aegion/module-magic-link:latest",
        "aegion/module-admin:latest",
        "aegion/module-oauth2:latest",
        "aegion/module-policy:latest"
    )
    
    foreach ($image in $images) {
        Write-Info "Scanning $image..."
        $trivyOutput = trivy image --severity HIGH,CRITICAL --format json --output "security-trivy-$($image -replace '[\/:.]','-').json" $image 2>&1
        
        if (Test-Path "security-trivy-$($image -replace '[\/:.]','-').json") {
            $results = Get-Content "security-trivy-$($image -replace '[\/:.]','-').json" | ConvertFrom-Json
            $vulnCount = ($results.Results | ForEach-Object { $_.Vulnerabilities.Count } | Measure-Object -Sum).Sum
            
            if ($vulnCount -gt 0) {
                Write-Issue "$image has $vulnCount HIGH/CRITICAL vulnerabilities"
            } else {
                Write-Pass "$image has no HIGH/CRITICAL vulnerabilities"
            }
        }
    }
}

# Scan 4: Secret detection (gitleaks)
function Scan-Secrets {
    Write-Section "Secret Detection Scan (gitleaks)"
    $script:ScansRun++
    
    if (-not (Test-Command "gitleaks")) {
        Write-Info "gitleaks not installed. Skipping..."
        return
    }
    
    # Remove generated scanner artifacts so gitleaks doesn't flag prior scan output as secrets.
    @(
        "security-govulncheck.json",
        "security-gosec.json",
        "security-gosec-high.json",
        "security-trivy-fs.json",
        "security-trivy-image.json"
    ) | ForEach-Object {
        if (Test-Path $_) {
            Remove-Item $_ -Force -ErrorAction SilentlyContinue
        }
    }

    Write-Info "Running gitleaks..."
    $gitleaksResult = gitleaks detect --no-git --report-format json --report-path=security-gitleaks.json 2>&1
    $exitCode = $LASTEXITCODE
    
    if (Test-Path "security-gitleaks.json") {
        $results = Get-Content "security-gitleaks.json" | ConvertFrom-Json
        
        if ($results.Count -gt 0) {
            Write-Issue "Found $($results.Count) potential secrets in codebase"
            $results | ForEach-Object {
                Write-Host "  $($_.Description) in $($_.File):$($_.StartLine)"
            }
        } else {
            Write-Pass "No secrets detected in codebase"
        }
    } elseif ($exitCode -eq 0) {
        Write-Pass "No secrets detected in codebase"
    }
}

# Scan 5: Container security best practices (docker scout)
function Scan-ContainerBestPractices {
    Write-Section "Container Security Best Practices"
    $script:ScansRun++
    
    Write-Info "Checking Dockerfile best practices..."
    
    # Check all Dockerfiles
    $dockerfiles = Get-ChildItem -Recurse -Filter "Dockerfile*" | Where-Object { $_.Name -notmatch "\.bak$" }
    
    foreach ($dockerfile in $dockerfiles) {
        Write-Info "Checking $($dockerfile.FullName)..."
        if ($dockerfile.Name -eq "Dockerfile.base") {
            Write-Info "$($dockerfile.Name) is a build-base image; skipping runtime checks"
            continue
        }

        $lines = Get-Content $dockerfile.FullName
        if ($lines.Count -eq 0) {
            Write-Info "$($dockerfile.Name) is empty; skipping"
            continue
        }

        $fromIndices = @()
        for ($i = 0; $i -lt $lines.Count; $i++) {
            if ($lines[$i] -match "^\s*FROM\s+") {
                $fromIndices += $i
            }
        }
        if ($fromIndices.Count -eq 0) {
            Write-Info "$($dockerfile.Name) has no FROM directive; skipping"
            continue
        }

        # Evaluate only final runtime stage to avoid builder-stage false positives.
        $runtimeStart = $fromIndices[-1]
        $runtimeFrom = $lines[$runtimeStart]
        $runtimeContent = ($lines[$runtimeStart..($lines.Count - 1)] -join "`n")
        $dockerfileContent = ($lines -join "`n")

        $issues = @()

        $runtimeNonRootBase = $runtimeFrom -match "(?i):nonroot(\s|$)"
        $runtimeHasUser = $runtimeContent -match "(?m)^\s*USER\s+\S+"
        if (-not ($runtimeNonRootBase -or $runtimeHasUser)) {
            $issues += "No USER directive (may run as root)"
        }
        
        if ($runtimeContent -match "(?m)^\s*COPY\s+\.\s+\.") {
            $issues += "Copies entire context (should be selective)"
        }

        if ($runtimeContent -match "(?m)^\s*EXPOSE\s+22(\s|$)") {
            $issues += "Exposes SSH port (security risk)"
        }
        
        if ($dockerfileContent -match "(?m)^\s*ADD\s+https?://") {
            $issues += "Uses ADD with URL (prefer curl/wget in RUN)"
        }
        
        if ($issues.Count -gt 0) {
            Write-Issue "$($dockerfile.Name) has potential issues:"
            $issues | ForEach-Object { Write-Host "  - $_" }
        } else {
            Write-Pass "$($dockerfile.Name) follows best practices"
        }
    }
}

# Scan 6: Configuration security
function Scan-Configuration {
    Write-Section "Configuration Security Check"
    $script:ScansRun++
    
    Write-Info "Checking production/staging configuration targets..."

    # Focus on deployable targets to keep findings actionable.
    $configTargets = @(
        "configs/aegion.production.yaml",
        "configs/aegion.staging.yaml",
        "deploy/docker-compose.prod.yml"
    )
    
    foreach ($target in $configTargets) {
        if (-not (Test-Path $target)) {
            continue
        }
        $file = Get-Item $target
        $content = Get-Content $file.FullName -Raw
        
        if ($content -match "(password|passwd|pwd)\s*[:=]\s*(admin|password|123456|root)") {
            Write-Issue "$($file.Name) contains potential weak credentials"
        }
        
        if ($content -match "sslmode\s*[:=]\s*disable") {
            Write-Issue "$($file.Name) has SSL disabled for database"
        }
        
        if ($content -match "secure\s*[:=]\s*false") {
            Write-Issue "$($file.Name) has insecure cookies configured"
        }
    }
    
    Write-Pass "Configuration security check complete"
}

# Scan 7: TLS/SSL configuration
function Scan-TLSConfiguration {
    Write-Section "TLS/SSL Configuration Check"
    $script:ScansRun++
    
    Write-Info "Checking TLS configuration..."
    
    # Search for TLS configuration in Go files
    $goFiles = Get-ChildItem -Recurse -Filter "*.go"
    $tlsFound = $false
    $weakCiphers = $false
    
    foreach ($file in $goFiles) {
        $content = Get-Content $file.FullName -Raw
        
        if ($content -match "tls\.Config") {
            $tlsFound = $true
            
            if ($content -match "MinVersion.*tls\.VersionTLS1[01]") {
                $weakCiphers = $true
                Write-Issue "$($file.Name) allows TLS 1.0/1.1 (should be TLS 1.2+)"
            }
            
            if ($content -match "InsecureSkipVerify.*true") {
                Write-Issue "$($file.Name) skips certificate verification (insecure)"
            }
        }
    }
    
    if ($tlsFound -and -not $weakCiphers) {
        Write-Pass "TLS configuration uses secure versions"
    } elseif (-not $tlsFound) {
        Write-Info "No TLS configuration found in codebase"
    }
}

# Scan 8: Authentication implementation
function Scan-Authentication {
    Write-Section "Authentication Implementation Check"
    $script:ScansRun++
    
    Write-Info "Checking authentication patterns in non-test code..."
    
    $goFiles = Get-ChildItem -Recurse -Filter "*.go" | Where-Object { $_.Name -notmatch "_test\.go$" }
    
    foreach ($file in $goFiles) {
        $content = Get-Content $file.FullName

        # Check for explicit MD5 usage in production code.
        foreach ($line in $content) {
            if ($line -match "\bmd5\.(Sum|New)\b" -and $line -notmatch "#nosec\s+G401") {
                Write-Issue "$($file.Name) uses weak hashing algorithm (MD5)"
                break
            }
        }
    }
    
    Write-Pass "Authentication pattern check complete"
}

# Main execution
function Main {
    Write-Host "==================================================" -ForegroundColor Cyan
    Write-Host "  Aegion Security Scanning Suite" -ForegroundColor Cyan
    Write-Host "==================================================" -ForegroundColor Cyan
    
    # Ensure we're in the project root
    if (-not (Test-Path "go.mod")) {
        Write-Host "Error: Must run from project root directory" -ForegroundColor Red
        exit 1
    }
    
    # Run all scans
    Scan-GoSecurity
    Scan-Dependencies
    Scan-Configuration
    Scan-TLSConfiguration
    Scan-Authentication
    Scan-ContainerBestPractices
    Scan-Secrets
    # Scan-DockerImages  # Uncomment if Docker images are built
    
    # Summary
    Write-Section "Security Scan Summary"
    Write-Host "Scans run: $($script:ScansRun)" -ForegroundColor Cyan
    
    if ($script:IssuesFound -eq 0) {
        Write-Host "[PASS] No security issues found!" -ForegroundColor Green
        exit 0
    } else {
        Write-Host "[WARN] Found $($script:IssuesFound) potential security issues" -ForegroundColor Yellow
        if ($script:FailOnIssues) {
            Write-Host "Failing scan (set AEGION_SECURITY_SCAN_FAIL_ON_ISSUES=false to override)." -ForegroundColor Yellow
            exit 1
        }
        Write-Host "Continuing despite findings (AEGION_SECURITY_SCAN_FAIL_ON_ISSUES=false)." -ForegroundColor Yellow
        exit 0
    }
}

# Run main
Main
