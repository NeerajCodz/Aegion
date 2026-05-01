#!/usr/bin/env bash
# =============================================================================
# Aegion Module Docker Testing Script
# Tests each module individually in Docker to verify modular approach
# =============================================================================

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test results tracking
TESTS_PASSED=0
TESTS_FAILED=0
TESTS_SKIPPED=0

# Cleanup function
cleanup() {
    echo -e "${BLUE}Cleaning up containers and networks...${NC}"
    docker compose -f deploy/docker-compose.yml down -v --remove-orphans 2>/dev/null || true
}

trap cleanup EXIT

# Log function
log() {
    echo -e "${BLUE}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $*"
}

success() {
    echo -e "${GREEN}✓${NC} $*"
    ((TESTS_PASSED++))
}

error() {
    echo -e "${RED}✗${NC} $*"
    ((TESTS_FAILED++))
}

warning() {
    echo -e "${YELLOW}⚠${NC} $*"
    ((TESTS_SKIPPED++))
}

# Test if a URL returns expected status code
test_http() {
    local url=$1
    local expected_status=${2:-200}
    local max_retries=${3:-30}
    local retry_delay=${4:-2}
    
    log "Testing HTTP endpoint: $url (expecting $expected_status)"
    
    for i in $(seq 1 $max_retries); do
        if response=$(curl -s -w "%{http_code}" -o /dev/null "$url" 2>&1); then
            if [ "$response" = "$expected_status" ]; then
                success "HTTP $url returned $response"
                return 0
            fi
        fi
        
        if [ $i -lt $max_retries ]; then
            echo "  Retry $i/$max_retries (got: $response)..."
            sleep $retry_delay
        fi
    done
    
    error "HTTP $url failed (expected $expected_status, got $response)"
    return 1
}

# Test if a container is healthy
test_container_health() {
    local container=$1
    local max_retries=${2:-30}
    local retry_delay=${3:-2}
    
    log "Checking health of container: $container"
    
    for i in $(seq 1 $max_retries); do
        if health=$(docker inspect --format='{{.State.Health.Status}}' "$container" 2>/dev/null); then
            if [ "$health" = "healthy" ]; then
                success "Container $container is healthy"
                return 0
            elif [ "$health" = "starting" ]; then
                echo "  Container starting... ($i/$max_retries)"
            else
                error "Container $container is $health"
                return 1
            fi
        else
            echo "  Waiting for container... ($i/$max_retries)"
        fi
        
        sleep $retry_delay
    done
    
    error "Container $container failed to become healthy"
    return 1
}

# Test gRPC health check
test_grpc_health() {
    local host=$1
    local port=$2
    
    log "Testing gRPC health: $host:$port"
    
    # Use grpc_health_probe if available, otherwise check port
    if command -v grpc_health_probe &> /dev/null; then
        if grpc_health_probe -addr="$host:$port" 2>&1; then
            success "gRPC $host:$port is healthy"
            return 0
        fi
    else
        # Fallback: check if port is open
        if timeout 5 bash -c "echo > /dev/tcp/$host/$port" 2>/dev/null; then
            success "gRPC port $host:$port is open"
            return 0
        fi
    fi
    
    error "gRPC $host:$port is not responding"
    return 1
}

# Security check: verify environment variables don't contain secrets
test_no_secrets_in_env() {
    local container=$1
    
    log "Checking for hardcoded secrets in $container"
    
    if env_vars=$(docker exec "$container" env 2>/dev/null | grep -E "(PASSWORD|SECRET|KEY|TOKEN)" | grep -v "CHANGE" | grep -v "xxx" || true); then
        if [ -n "$env_vars" ]; then
            warning "Found environment variables with secret-like names in $container"
            echo "$env_vars" | while read -r line; do
                echo "  $line"
            done
            return 1
        fi
    fi
    
    success "No obvious secrets exposed in $container environment"
    return 0
}

# Security check: verify container runs as non-root
test_nonroot_user() {
    local container=$1
    
    log "Checking if $container runs as non-root"
    
    if user=$(docker exec "$container" id -u 2>/dev/null); then
        if [ "$user" != "0" ]; then
            success "Container $container runs as non-root user (UID: $user)"
            return 0
        else
            error "Container $container runs as root!"
            return 1
        fi
    fi
    
    error "Could not check user for $container"
    return 1
}

# Security check: verify TLS is configured for production
test_tls_configuration() {
    log "Checking TLS configuration"
    
    # Check if TLS certificates are mounted/configured
    if [ -n "${TLS_CERT_FILE:-}" ] && [ -n "${TLS_KEY_FILE:-}" ]; then
        success "TLS configuration present"
        return 0
    else
        warning "TLS not configured (acceptable for development)"
        return 0
    fi
}

# Test module isolation: ensure modules can't access each other's data directly
test_module_isolation() {
    local module1=$1
    local module2=$2
    
    log "Testing isolation between $module1 and $module2"
    
    # Check network isolation (modules should only communicate via defined interfaces)
    if docker exec "$module1" ping -c 1 -W 1 "$module2" &>/dev/null; then
        success "Module $module1 can reach $module2 (expected for same network)"
    else
        error "Module $module1 cannot reach $module2"
        return 1
    fi

    if ! test_pid_namespace_isolation "$module1" "$module2"; then
        return 1
    fi

    if ! test_filesystem_isolation "$module1" "$module2"; then
        return 1
    fi

    return 0
}

test_pid_namespace_isolation() {
    local module1=$1
    local module2=$2

    local pid_mode_1
    local pid_mode_2
    pid_mode_1=$(docker inspect --format='{{.HostConfig.PidMode}}' "$module1" 2>/dev/null || true)
    pid_mode_2=$(docker inspect --format='{{.HostConfig.PidMode}}' "$module2" 2>/dev/null || true)

    if [[ "$pid_mode_1" == host || "$pid_mode_2" == host ]]; then
        error "Host PID namespace sharing detected between $module1 and $module2"
        return 1
    fi
    if [[ "$pid_mode_1" == container:* || "$pid_mode_2" == container:* ]]; then
        error "Container PID namespace sharing detected between $module1 and $module2"
        return 1
    fi

    success "PID namespaces for $module1 and $module2 are isolated"
    return 0
}

test_filesystem_isolation() {
    local module1=$1
    local module2=$2

    local mounts1
    local mounts2
    mounts1=$(docker inspect --format='{{range .Mounts}}{{printf "%s|%s|%t\n" .Source .Destination .RW}}{{end}}' "$module1" 2>/dev/null || true)
    mounts2=$(docker inspect --format='{{range .Mounts}}{{printf "%s|%s|%t\n" .Source .Destination .RW}}{{end}}' "$module2" 2>/dev/null || true)

    local shared=0
    while IFS='|' read -r src1 dest1 rw1; do
        [ -n "${src1:-}" ] || continue
        while IFS='|' read -r src2 dest2 rw2; do
            [ -n "${src2:-}" ] || continue
            if [[ "$src1" == "$src2" && "$rw1" == "true" && "$rw2" == "true" ]]; then
                error "Writable shared mount detected between $module1 ($dest1) and $module2 ($dest2): $src1"
                shared=1
            fi
        done <<< "$mounts2"
    done <<< "$mounts1"

    if [ "$shared" -ne 0 ]; then
        return 1
    fi

    success "No shared writable mounts detected between $module1 and $module2"
    return 0
}

# Main test execution
main() {
    log "Starting Aegion Docker Module Testing"
    log "======================================"
    
    # Ensure we're in the project root
    if [ ! -f "go.mod" ] || [ ! -d "deploy" ]; then
        error "Must run from project root directory"
        exit 1
    fi
    
    # Create .env file if it doesn't exist
    if [ ! -f "deploy/.env" ]; then
        log "Creating .env file from template"
        cp deploy/.env.example deploy/.env
        
        # Generate secure secrets
        log "Generating secure secrets..."
        export AEGION_SECRETS_COOKIE=$(openssl rand -base64 32 | head -c 32)
        export AEGION_SECRETS_CIPHER=$(openssl rand -base64 32 | head -c 32)
        export AEGION_SECRETS_INTERNAL=$(openssl rand -base64 32 | head -c 32)
        
        # Update .env file
        sed -i "s/dev-cookie-secret-change-me-32chars!/$AEGION_SECRETS_COOKIE/" deploy/.env
        sed -i "s/dev-cipher-secret-change-me-32chars!/$AEGION_SECRETS_CIPHER/" deploy/.env
        sed -i "s/dev-internal-secret-change-me-32ch!/$AEGION_SECRETS_INTERNAL/" deploy/.env
    fi
    
    # Build all Docker images first
    log "Building Docker images..."
    if ! docker compose -f deploy/docker-compose.yml build; then
        error "Failed to build Docker images"
        exit 1
    fi
    success "All Docker images built successfully"
    
    # Start infrastructure (postgres, redis)
    log "Starting infrastructure services..."
    docker compose -f deploy/docker-compose.yml up -d postgres redis
    
    # Wait for infrastructure to be healthy
    test_container_health "aegion-postgres-1" || exit 1
    test_container_health "aegion-redis-1" || exit 1
    
    # Test 1: Core service
    log ""
    log "TEST 1: Core Service"
    log "===================="
    docker compose -f deploy/docker-compose.yml up -d aegion
    test_container_health "aegion-aegion-1" || true
    test_http "http://localhost:8080/health" 200 || true
    test_http "http://localhost:8080/.well-known/aegion/meta" 200 || true
    test_nonroot_user "aegion-aegion-1" || true
    
    # Test 2: Password module
    log ""
    log "TEST 2: Password Module"
    log "======================="
    docker compose -f deploy/docker-compose.yml up -d module-password
    test_container_health "aegion-module-password-1" || true
    test_http "http://localhost:9001/health" 200 || true
    test_nonroot_user "aegion-module-password-1" || true
    
    # Test 3: Magic Link module
    log ""
    log "TEST 3: Magic Link Module"
    log "========================="
    docker compose -f deploy/docker-compose.yml up -d module-magic-link
    test_container_health "aegion-module-magic-link-1" || true
    test_http "http://localhost:9002/health" 200 || true
    test_nonroot_user "aegion-module-magic-link-1" || true
    
    # Test 4: Admin module
    log ""
    log "TEST 4: Admin Module"
    log "===================="
    docker compose -f deploy/docker-compose.yml up -d module-admin
    test_container_health "aegion-module-admin-1" || true
    test_http "http://localhost:9003/health" 200 || true
    test_nonroot_user "aegion-module-admin-1" || true
    
    # Test 5: Inter-module communication
    log ""
    log "TEST 5: Inter-Module Communication"
    log "==================================="
    test_module_isolation "aegion-module-password-1" "aegion-module-magic-link-1" || true
    
    # Test 6: Security hardening
    log ""
    log "TEST 6: Security Hardening"
    log "=========================="
    test_no_secrets_in_env "aegion-aegion-1" || true
    test_tls_configuration || true
    
    # Test 7: End-to-end workflow
    log ""
    log "TEST 7: End-to-End Workflow"
    log "==========================="
    # Test registration flow (if implemented)
    test_http "http://localhost:8080/self-service/registration/browser" 200 || true
    # Test login flow
    test_http "http://localhost:8080/self-service/login/browser" 200 || true
    
    # Show container logs for debugging
    log ""
    log "Container Logs (last 20 lines each):"
    log "====================================="
    for container in aegion-aegion-1 aegion-module-password-1 aegion-module-magic-link-1 aegion-module-admin-1; do
        if docker ps --format '{{.Names}}' | grep -q "$container"; then
            log "--- $container ---"
            docker logs --tail 20 "$container" 2>&1 || true
        fi
    done
    
    # Summary
    log ""
    log "Test Summary"
    log "============"
    success "Tests passed: $TESTS_PASSED"
    error "Tests failed: $TESTS_FAILED"
    warning "Tests skipped: $TESTS_SKIPPED"
    
    if [ $TESTS_FAILED -gt 0 ]; then
        error "Some tests failed!"
        exit 1
    else
        success "All tests passed!"
        exit 0
    fi
}

# Run main function
main "$@"
