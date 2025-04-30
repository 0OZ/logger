# go-logger

A flexible, high-performance, and developer-friendly logging library for Go, built on top of [uber-go/zap](https://github.com/uber-go/zap). Supports multiple output formats, contextual fields, error tracing, batch logging, HTTP middleware, and more.

## Features

- Multiple log levels: Debug, Info, Warn, Error, Fatal, Panic
- Output formats: JSON, Console, Pretty (colorful), Compact
- Customizable time formats
- Contextual and structured logging
- Field redaction for sensitive data
- Batch logging
- HTTP request logging and middleware
- File and (placeholder) rolling file logging
- Dynamic log level changes
- Production and development presets
- Built-in error tracing and stack traces

## Installation

```
go get github.com/0Oz/go-logger
```

## Basic Usage

```go
package main

import (
    "github.com/0Oz/go-logger"
)

func main() {
    log := logger.New()
    log.Info("Hello, logger!")
}
```

## Example Usage

```go
log := logger.New()
log.Info("Starting example application")

devLogger := logger.NewPrettyConsoleLogger()
devLogger.Debug("This is a debug message with pretty formatting")

// Log with fields
log.Info("User logged in",
    "user_id", "123456",
    "email", "user@example.com",
    "login_time", time.Now(),
)

// Using WithFields
userLogger := log.WithFields(map[string]interface{}{
    "user_id": "123456",
    "session": "abcdef1234567890",
})
userLogger.Info("User performed an action")

// Log different levels
log.Debug("Debug message - might not show depending on level")
log.Info("Info message - general information")
log.Warn("Warning message - something to pay attention to")
log.Error("Error message - something went wrong", "error_code", 500)

// Log with error
err := errors.New("something failed")
log.WithError(err).Error("Operation failed")

// HTTP request logging
log.HTTPRequest("GET", "/api/users", 200, 123*time.Millisecond,
    "user_agent", "Mozilla/5.0",
    "user_id", "123456",
)

// Formatted logging
log.Infof("Server started on port %d", 8080)
log.Warnf("Resource usage at %.2f%%", 85.75)

// Batch logging
batchLogger := logger.NewBatchLogger(log, 10, 3*time.Second)
for i := 0; i < 5; i++ {
    batchLogger.Add(logger.InfoLevel, "Batch log entry", map[string]interface{}{
        "index": i,
        "value": "test",
    })
}
batchLogger.Flush() // Force flush

// File logger
fileLogger, err := logger.NewFileLogger("./logs/application.log", logger.InfoLevel)
if err != nil {
    log.WithError(err).Error("Failed to create file logger")
} else {
    fileLogger.Info("This message is written to a file")
}

// HTTP middleware
http.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte("pong"))
})

// Start HTTP server with logging middleware
server := &http.Server{
    Addr:    ":8080",
    Handler: log.LogMiddleware(http.DefaultServeMux),
}
log.Fatal("Server failed", server.ListenAndServe())
```

## Configuration

You can customize the logger using the `Config` struct:

```go
type Config struct {
    Level              LogLevel    // debug, info, warn, error, fatal, panic
    Format             OutputFormat // json, console, pretty, compact
    TimeFormat         TimeFormat   // iso8601, unix, unixmilli, rfc3339, simple, kitchen
    Output             string       // stdout, stderr, or file path
    Development        bool
    AddCallerInfo      bool
    CallerSkip         int
    StackTrace         bool
    ServiceName        string
    Version            string
    Environment        string
    SamplingEnabled    bool
    SamplingInitial    int
    SamplingThereafter int
    ContextualFields   []string
    RedactFields       []string // e.g. ["password", "token"]
}
```

Create a logger with custom config:

```go
cfg := logger.Config{
    Level:      logger.DebugLevel,
    Format:     logger.FormatPretty,
    Output:     "stdout",
    ServiceName: "my-service",
    // ... other options ...
}
log := logger.NewWithConfig(cfg)
```

## Advanced Features

- **BatchLogger**: Buffer logs and flush in batches for performance.
- **HTTP Middleware**: Log HTTP requests with latency, status, and user agent.
- **Field Redaction**: Automatically redact sensitive fields (e.g., passwords, tokens).
- **Dynamic Level**: Change log level at runtime with `SetLevel`.
- **Production/Development Presets**: Use `NewProductionLogger` or `NewPrettyConsoleLogger` for quick setup.
- **Error Tracing**: Use `WithError` and `TraceError` for rich error context.

## Dependencies

- [go.uber.org/zap](https://github.com/uber-go/zap)
- [go.uber.org/multierr](https://github.com/uber-go/multierr)

## Directory Structure

- `logger.go` — Main logger implementation
- `example/` — Example usage and demo app
- `logs/` — Default log output directory (gitignored)

---

*Feel free to open issues or PRs to improve this logger!* 