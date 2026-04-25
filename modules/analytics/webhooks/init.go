package webhooks

// Package webhooks provides webhook management for analytics events.
//
// Features:
// - Webhook registration and management
// - Event filtering with glob patterns and custom filters
// - HTTP delivery with retries and exponential backoff
// - HMAC-SHA256 signature generation
// - Dead letter queue for failed deliveries
// - Delivery history and event replay

// This package exports:
// - Manager: Orchestrates webhook operations
// - Store: Persists webhooks and delivery history
// - Dispatcher: Sends HTTP webhooks
// - Matcher: Matches events against webhook filters
// - RetryPolicy: Handles retry logic
// - Signature: Generates HMAC signatures

// Usage:
// 1. Create a Store with a database
// 2. Create a Manager with the Store
// 3. Call manager.Start() to begin processing deliveries
// 4. Call manager.RegisterWebhook() to create webhooks
// 5. Call manager.PublishEvent() or manager.DispatchEvent() when events occur
// 6. Call manager.Stop() for graceful shutdown
