# Observability System – Developer Guide

This document defines how to **implement, operate, and debug a production-grade observability pipeline** for a distributed Go service using OpenTelemetry (OTEL).

---

## Overview

The system consists of:

* Go-based HTTP service (e.g., Echo)
* Embedded OpenTelemetry SDK
* OpenTelemetry Collector (sidecar)
* Remote observability backend (Tempo, Prometheus, Loki, etc.)

---

## Architecture

### Data Flow

```
Application (OTEL SDK)
        │
        │ OTLP (gRPC :4317)
        ▼
OpenTelemetry Collector (sidecar)
        │
        ├── Traces → Tempo
        ├── Metrics → Prometheus (remote write)
        └── Logs → Loki
```

---

### Key Principles

* Application sends telemetry only to **localhost Collector**
* Collector handles:

  * Authentication
  * Batching & retries
  * Resource enrichment
  * Cardinality control
* No credentials in application code
* Sidecar pattern (one Collector per service instance)

---

## Environment Behavior

### Local Development

* Telemetry is optional (can be disabled)
* Default: no external dependencies required

### Production

* Telemetry enabled by default
* Collector sidecar is mandatory
* Backend connectivity required

---

## Resource Configuration

All signals must share a consistent OTEL Resource.

### Required Attributes

* `service.name`
* `service.version`
* `deployment.environment`
* `service.instance.id`

---

## Tracing

### Automatic Instrumentation

Enable:

* HTTP server spans (per request)
* Middleware tracing
* Database / external calls (if supported)

---

### Custom Spans

Use spans for business operations.

```go
ctx, span := tracer.Start(ctx, "create_user")
defer span.End()
```

---

### Guidelines

* Use meaningful span names (low cardinality)
* Add attributes instead of encoding data in span names
* Propagate context across all boundaries

---

## Metrics

### Required Metrics

* Request latency (histogram)
* Request count (counter)
* Error count (counter)
* Runtime metrics (GC, memory, goroutines)

---

### Example

```go
requestCounter.Add(ctx, 1,
    metric.WithAttributes(attribute.String("route", "/users")),
)
```

---

### Guidelines

* Use **low-cardinality labels only**
* Prefer histograms for latency
* Avoid dynamic values (user IDs, request IDs)

---

## Logging

### Requirements

* Structured logging only
* Logs must include:

  * `trace_id`
  * `span_id`
  * `request_id`

---

### Example

```go
logger.InfoContext(ctx, "user created",
    "user_id", user.ID,
)
```

---

### Rules

* Never log sensitive data
* Always log with context
* Ensure logs correlate with traces

---

## Correlation

All signals must be correlated:

* Logs ↔ Traces via `trace_id` and `span_id`
* Metrics ↔ Traces via exemplars (when supported)

---

## Cardinality Control

### Strategy

| Signal  | High Cardinality |
| ------- | ---------------- |
| Traces  | Allowed          |
| Logs    | Allowed          |
| Metrics | Restricted       |

---

### Rules

* Keep high-cardinality data out of metric labels
* Use Collector processors to drop or transform labels

---

## Collector Responsibilities

The sidecar Collector is responsible for:

* Authentication with backend
* Batching and retry logic
* Memory limiting
* Resource detection (cloud/container)
* Attribute filtering for metrics

---

## Deployment Model

* One Collector sidecar per service instance
* OTLP push model (no scraping)
* Resilient to network failures

---

## Failure Handling

System must degrade gracefully:

* If Collector is unavailable → app continues without telemetry
* If backend is unavailable → Collector buffers/retries

---

## Debugging Guide

### Verify Telemetry Locally

* Check app is sending to `localhost:4317`
* Enable debug exporter in Collector

---

### Missing Traces

* Verify context propagation
* Check sampling configuration
* Inspect Collector logs

---

### Broken Trace Chains

* Ensure context is passed across goroutines and services
* Validate HTTP headers (`traceparent`)

---

### Metrics Issues

* Check label cardinality
* Verify exporter configuration

---

### Logs Not Correlated

* Ensure logger extracts trace/span from context

---

### Collector Issues

* Inspect Collector logs
* Validate exporters and endpoints

---

## Best Practices

* Keep span names low cardinality
* Use attributes for detail
* Avoid sensitive data in logs
* Prefer histograms over averages
* Maintain consistent service identity

---

## Workflow Summary

```bash
# 1. Instrument service (traces, metrics, logs)

# 2. Run Collector sidecar

# 3. Verify local telemetry

# 4. Deploy with Collector in production

# 5. Use dashboards + traces for debugging
```

---

## Key Principles

* Separation of concerns (app vs Collector)
* Push-based telemetry
* Correlated signals
* Controlled cardinality
* Graceful degradation

---

If needed, next steps:

* Provide OTEL Go SDK setup code
* Provide Collector config (otel-collector.yaml)
* Add Grafana dashboards & alerting rules
* Add sampling strategies (tail/head-based)
