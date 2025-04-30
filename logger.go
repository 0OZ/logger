package logger

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// LogLevel represents logging levels
type LogLevel string

// Log levels
const (
	DebugLevel LogLevel = "debug"
	InfoLevel  LogLevel = "info"
	WarnLevel  LogLevel = "warn"
	ErrorLevel LogLevel = "error"
	FatalLevel LogLevel = "fatal"
	PanicLevel LogLevel = "panic"
)

// TimeFormat defines standard time formats for logging
type TimeFormat string

// Available time formats
const (
	TimeFormatISO8601   TimeFormat = "iso8601"   // 2006-01-02T15:04:05.000Z0700
	TimeFormatUnix      TimeFormat = "unix"      // Unix timestamp
	TimeFormatUnixMilli TimeFormat = "unixmilli" // Unix timestamp with milliseconds
	TimeFormatRFC3339   TimeFormat = "rfc3339"   // RFC3339 format
	TimeFormatSimple    TimeFormat = "simple"    // 2006-01-02 15:04:05
	TimeFormatKitchen   TimeFormat = "kitchen"   // 3:04:05PM
)

// OutputFormat defines the log output format
type OutputFormat string

// Available output formats
const (
	FormatJSON    OutputFormat = "json"
	FormatConsole OutputFormat = "console"
	FormatPretty  OutputFormat = "pretty"  // Colored, human-friendly output
	FormatCompact OutputFormat = "compact" // Minimal output format
)

// Config contains logger configuration options
type Config struct {
	Level              LogLevel
	Format             OutputFormat
	TimeFormat         TimeFormat
	Output             string // stdout, stderr, or file path
	Development        bool
	AddCallerInfo      bool
	CallerSkip         int      // How many levels of stack to skip when capturing caller
	StackTrace         bool     // Include stack traces for errors
	ServiceName        string   // Name of service for metadata
	Version            string   // Version of software for metadata
	Environment        string   // Environment (production, staging, etc)
	SamplingEnabled    bool     // Enable log sampling to reduce volume
	SamplingInitial    int      // Initial sampling allowance
	SamplingThereafter int      // Sampling rate after initial allowance
	ContextualFields   []string // Additional contextual fields to always include
	RedactFields       []string // Fields to redact from logs (e.g. "password", "token")
}

// Logger wraps zap logger with additional functionality
// Logger provides structured, leveled logging with context, redaction, and output configuration.
type Logger struct {
	*zap.SugaredLogger
	config Config
	fields map[string]interface{}
	level  zap.AtomicLevel
}

// Default config values
var defaultConfig = Config{
	Level:              InfoLevel,
	Format:             FormatJSON,
	TimeFormat:         TimeFormatISO8601,
	Output:             "stdout",
	Development:        false,
	AddCallerInfo:      true,
	CallerSkip:         0,
	StackTrace:         true,
	ServiceName:        "app",
	Version:            "unknown",
	Environment:        "development",
	SamplingEnabled:    false,
	SamplingInitial:    100,
	SamplingThereafter: 100,
	ContextualFields:   []string{},
	RedactFields:       []string{"password", "secret", "token", "key", "auth"},
}

// New creates a new logger with default configuration
func New() *Logger {
	return NewWithConfig(defaultConfig)
}

// TimeEncoder returns an encoding function for timestamps based on the format
func TimeEncoder(format TimeFormat) zapcore.TimeEncoder {
	switch format {
	case TimeFormatISO8601:
		return zapcore.ISO8601TimeEncoder
	case TimeFormatUnix:
		return zapcore.EpochTimeEncoder
	case TimeFormatUnixMilli:
		return zapcore.EpochMillisTimeEncoder
	case TimeFormatRFC3339:
		return func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
			enc.AppendString(t.Format(time.RFC3339))
		}
	case TimeFormatSimple:
		return func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
			enc.AppendString(t.Format("2006-01-02 15:04:05"))
		}
	case TimeFormatKitchen:
		return func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
			enc.AppendString(t.Format(time.Kitchen))
		}
	default:
		return zapcore.ISO8601TimeEncoder
	}
}

// NewWithConfig creates a new logger with the specified configuration
func NewWithConfig(config Config) *Logger {
	level := getZapLevel(config.Level)
	atomicLevel := zap.NewAtomicLevelAt(level)

	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "message",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     TimeEncoder(config.TimeFormat),
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	configureEncoder(&encoderConfig, config.Format)
	output := configureOutput(config.Output)
	encoder := createEncoder(encoderConfig, config.Format)
	core := createCore(encoder, output, atomicLevel, config)

	opts := createZapOptions(config)
	initialFields := createInitialFields(config)
	zapLogger := zap.New(core, opts...)

	zapFields := make([]zap.Field, 0, len(initialFields))
	for k, v := range initialFields {
		zapFields = append(zapFields, zap.Any(k, v))
	}
	sugar := zapLogger.With(zapFields...).Sugar()

	return &Logger{
		SugaredLogger: sugar,
		config:        config,
		fields:        initialFields,
		level:         atomicLevel,
	}
}

func configureEncoder(encoderConfig *zapcore.EncoderConfig, format OutputFormat) {
	switch format {
	case FormatConsole:
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		encoderConfig.EncodeCaller = func(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) {
			_, file := filepath.Split(caller.File)
			enc.AppendString(fmt.Sprintf("%s:%d", file, caller.Line))
		}
	case FormatPretty:
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		encoderConfig.EncodeCaller = func(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) {
			dir, file := filepath.Split(caller.File)
			parentDir := filepath.Base(dir)
			enc.AppendString(fmt.Sprintf("%s/%s:%d", parentDir, file, caller.Line))
		}
		encoderConfig.ConsoleSeparator = " | "
	case FormatCompact:
		encoderConfig.TimeKey = ""
		encoderConfig.LevelKey = "l"
		encoderConfig.MessageKey = "m"
		encoderConfig.CallerKey = "c"
		encoderConfig.EncodeLevel = func(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
			switch l {
			case zapcore.DebugLevel:
				enc.AppendString("D")
			case zapcore.InfoLevel:
				enc.AppendString("I")
			case zapcore.WarnLevel:
				enc.AppendString("W")
			case zapcore.ErrorLevel:
				enc.AppendString("E")
			case zapcore.FatalLevel:
				enc.AppendString("F")
			case zapcore.PanicLevel:
				enc.AppendString("P")
			}
		}
	}
}

// configureOutput sets up the log output destination, supporting stdout, stderr, and rolling file logs.
func configureOutput(outputPath string) zapcore.WriteSyncer {
	switch strings.ToLower(outputPath) {
	case "stdout":
		return zapcore.AddSync(os.Stdout)
	case "stderr":
		return zapcore.AddSync(os.Stderr)
	default:
		dir := filepath.Dir(outputPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create log directory %s: %v\n", dir, err)
		}
		// Use lumberjack for rolling logs
		ljLogger := &lumberjack.Logger{
			Filename:   outputPath,
			MaxSize:    100, // megabytes
			MaxBackups: 7,
			MaxAge:     30,   // days
			Compress:   true, // compress old logs
		}
		return zapcore.AddSync(ljLogger)
	}
}

func createEncoder(encoderConfig zapcore.EncoderConfig, format OutputFormat) zapcore.Encoder {
	switch format {
	case FormatJSON:
		return zapcore.NewJSONEncoder(encoderConfig)
	case FormatConsole, FormatPretty, FormatCompact:
		return zapcore.NewConsoleEncoder(encoderConfig)
	default:
		return zapcore.NewJSONEncoder(encoderConfig)
	}
}

func createCore(encoder zapcore.Encoder, output zapcore.WriteSyncer, level zap.AtomicLevel, config Config) zapcore.Core {
	if config.SamplingEnabled {
		return zapcore.NewSamplerWithOptions(
			zapcore.NewCore(encoder, output, level),
			time.Second,
			config.SamplingInitial,
			config.SamplingThereafter,
		)
	}
	return zapcore.NewCore(encoder, output, level)
}

func createZapOptions(config Config) []zap.Option {
	opts := []zap.Option{}
	if config.AddCallerInfo {
		opts = append(opts, zap.AddCaller())
		if config.CallerSkip != 0 {
			opts = append(opts, zap.AddCallerSkip(config.CallerSkip))
		}
	}
	if config.Development {
		opts = append(opts, zap.Development())
	}
	if config.StackTrace {
		opts = append(opts, zap.AddStacktrace(zapcore.ErrorLevel))
	}
	return opts
}

func createInitialFields(config Config) map[string]interface{} {
	fields := map[string]interface{}{
		"service": config.ServiceName,
	}

	if config.Environment != "" {
		fields["env"] = config.Environment
	}

	if config.Version != "" {
		fields["version"] = config.Version
	}

	hostname, err := os.Hostname()
	if err == nil && hostname != "" {
		fields["host"] = hostname
	}

	return fields
}

// fieldsToArgs converts a fields map to a slice of alternating keys and values
func fieldsToArgs(fields map[string]interface{}) []interface{} {
	args := make([]interface{}, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	return args
}

// getZapLevel converts our LogLevel to zap's level
func getZapLevel(level LogLevel) zapcore.Level {
	switch strings.ToLower(string(level)) {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	case "fatal":
		return zapcore.FatalLevel
	case "panic":
		return zapcore.PanicLevel
	default:
		return zapcore.InfoLevel
	}
}

// WithField returns a logger with a field added to it
func (l *Logger) WithField(key string, value interface{}) *Logger {
	if l.shouldRedact(key) {
		value = "[REDACTED]"
	}

	newFields := make(map[string]interface{}, len(l.fields)+1)
	for k, v := range l.fields {
		newFields[k] = v
	}
	newFields[key] = value

	return &Logger{
		SugaredLogger: l.SugaredLogger.With(key, value),
		config:        l.config,
		fields:        newFields,
		level:         l.level,
	}
}

// shouldRedact checks if a field should be redacted
func (l *Logger) shouldRedact(key string) bool {
	lowerKey := strings.ToLower(key)
	for _, f := range l.config.RedactFields {
		if strings.Contains(lowerKey, strings.ToLower(f)) {
			return true
		}
	}
	return false
}

// WithFields returns a logger with multiple fields added to it
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	newFields := make(map[string]interface{}, len(l.fields)+len(fields))
	for k, v := range l.fields {
		newFields[k] = v
	}

	processedFields := make(map[string]interface{}, len(fields))
	for k, v := range fields {
		if l.shouldRedact(k) {
			processedFields[k] = "[REDACTED]"
		} else {
			processedFields[k] = v
		}
		newFields[k] = processedFields[k]
	}

	return &Logger{
		SugaredLogger: l.SugaredLogger.With(fieldsToArgs(processedFields)...),
		config:        l.config,
		fields:        newFields,
		level:         l.level,
	}
}

// WithError returns a logger with an error field and additional error context
func (l *Logger) WithError(err error) *Logger {
	if err == nil {
		return l
	}

	errorFields := map[string]interface{}{
		"error":      err.Error(),
		"error_type": fmt.Sprintf("%T", err),
	}

	return l.WithFields(errorFields)
}

// WithContext enriches the logger with contextual information from the call site
func (l *Logger) WithContext() *Logger {
	_, file, line, ok := runtime.Caller(1)
	if !ok {
		return l
	}

	fields := map[string]interface{}{
		"file": file,
		"line": line,
	}

	if pc, _, _, ok := runtime.Caller(1); ok {
		if fn := runtime.FuncForPC(pc); fn != nil {
			fullName := fn.Name()
			if lastSlash := strings.LastIndexByte(fullName, '/'); lastSlash != -1 {
				if lastDot := strings.LastIndexByte(fullName[lastSlash:], '.'); lastDot != -1 {
					fields["function"] = fullName[lastSlash+lastDot+1:]
					fields["package"] = fullName[:lastSlash+lastDot]
				}
			} else {
				fields["function"] = fullName
			}
		}
	}

	return l.WithFields(fields)
}

// SetLevel changes the logging level dynamically
func (l *Logger) SetLevel(level LogLevel) {
	l.level.SetLevel(getZapLevel(level))
	l.Info("Log level changed to " + string(level))
}

// GetLevel returns the current logging level
func (l *Logger) GetLevel() LogLevel {
	zapLevel := l.level.Level()

	switch zapLevel {
	case zapcore.DebugLevel:
		return DebugLevel
	case zapcore.InfoLevel:
		return InfoLevel
	case zapcore.WarnLevel:
		return WarnLevel
	case zapcore.ErrorLevel:
		return ErrorLevel
	case zapcore.FatalLevel:
		return FatalLevel
	case zapcore.PanicLevel:
		return PanicLevel
	default:
		return InfoLevel
	}
}

// Debug logs a debug message with optional key-value pairs
func (l *Logger) Debug(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Debugw(msg, keysAndValues...)
}

// Info logs an info message with optional key-value pairs
func (l *Logger) Info(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Infow(msg, keysAndValues...)
}

// Warn logs a warning message with optional key-value pairs
func (l *Logger) Warn(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Warnw(msg, keysAndValues...)
}

// Error logs an error message with optional key-value pairs
func (l *Logger) Error(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Errorw(msg, keysAndValues...)
}

// Fatal logs a fatal message with optional error and then exits
func (l *Logger) Fatal(msg string, err error) {
	if err != nil {
		l.SugaredLogger.Fatalw(msg, "error", err)
	} else {
		l.SugaredLogger.Fatalw(msg)
	}
}

// Panic logs a panic message with optional key-value pairs and then panics
func (l *Logger) Panic(msg string, keysAndValues ...interface{}) {
	l.SugaredLogger.Panicw(msg, keysAndValues...)
}

// HTTPRequest logs an HTTP request with detailed information
func (l *Logger) HTTPRequest(method, path string, status int, latency time.Duration, keysAndValues ...interface{}) {
	fields := make([]interface{}, 0, 8+len(keysAndValues))
	fields = append(fields,
		"method", method,
		"path", path,
		"status", status,
		"latency_ms", latency.Milliseconds(),
	)
	fields = append(fields, keysAndValues...)

	if status >= 500 {
		l.SugaredLogger.Errorw("HTTP Request", fields...)
	} else if status >= 400 {
		l.SugaredLogger.Warnw("HTTP Request", fields...)
	} else {
		l.SugaredLogger.Infow("HTTP Request", fields...)
	}
}

// TraceError logs an error with automatic stack trace and context
func (l *Logger) TraceError(msg string, err error) {
	if err == nil {
		return
	}

	_, file, line, _ := runtime.Caller(1)
	fields := []interface{}{
		"error", err.Error(),
		"error_type", fmt.Sprintf("%T", err),
		"file", file,
		"line", line,
	}

	l.SugaredLogger.Errorw(msg, fields...)
}

// GetConfig returns the logger's configuration
func (l *Logger) GetConfig() Config {
	return l.config
}

// NewTestLogger creates a logger suitable for testing with pretty output
func NewTestLogger() *Logger {
	config := defaultConfig
	config.Level = DebugLevel
	config.Format = FormatPretty
	config.TimeFormat = TimeFormatSimple
	config.Development = true
	config.ServiceName = "test"
	return NewWithConfig(config)
}

// NewPrettyConsoleLogger creates a colorful, human-friendly logger for development
func NewPrettyConsoleLogger() *Logger {
	config := defaultConfig
	config.Format = FormatPretty
	config.TimeFormat = TimeFormatSimple
	config.Development = true
	return NewWithConfig(config)
}

// NewProductionLogger creates a logger optimized for production use
func NewProductionLogger(serviceName, version, environment string) *Logger {
	config := defaultConfig
	config.Format = FormatJSON
	config.TimeFormat = TimeFormatISO8601
	config.Development = false
	config.SamplingEnabled = true
	config.ServiceName = serviceName
	config.Version = version
	config.Environment = environment
	return NewWithConfig(config)
}

// GetZapLogger returns the underlying zap.Logger
func (l *Logger) GetZapLogger() *zap.Logger {
	return l.SugaredLogger.Desugar()
}

// Debugf logs a formatted debug message
func (l *Logger) Debugf(format string, args ...interface{}) { l.SugaredLogger.Debugf(format, args...) }

// Infof logs a formatted info message
func (l *Logger) Infof(format string, args ...interface{}) { l.SugaredLogger.Infof(format, args...) }

// Warnf logs a formatted warning message
func (l *Logger) Warnf(format string, args ...interface{}) { l.SugaredLogger.Warnf(format, args...) }

// Errorf logs a formatted error message
func (l *Logger) Errorf(format string, args ...interface{}) { l.SugaredLogger.Errorf(format, args...) }

// Fatalf logs a formatted fatal message and exits
func (l *Logger) Fatalf(format string, args ...interface{}) { l.SugaredLogger.Fatalf(format, args...) }

// Panicf logs a formatted panic message and panics
func (l *Logger) Panicf(format string, args ...interface{}) { l.SugaredLogger.Panicf(format, args...) }

// Sync flushes any buffered log entries
func (l *Logger) Sync() error {
	return l.SugaredLogger.Sync()
}

// DefaultLogger returns a global instance of the logger with default configuration
var defaultLoggerInstance *Logger
var defaultLoggerOnce sync.Once

// DefaultLogger returns a global singleton logger instance with default configuration
func DefaultLogger() *Logger {
	defaultLoggerOnce.Do(func() {
		defaultLoggerInstance = New()
	})
	return defaultLoggerInstance
}

// Flush ensures all buffered logs are written
func Flush() {
	if defaultLoggerInstance != nil {
		_ = defaultLoggerInstance.Sync()
	}
}

// NewFileLogger creates a logger that writes to the specified file
func NewFileLogger(filePath string, level LogLevel) (*Logger, error) {
	config := defaultConfig
	config.Output = filePath
	config.Level = level

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory %s: %v", dir, err)
	}

	return NewWithConfig(config), nil
}

// NewRollingFileLogger creates a logger with file rotation capabilities using lumberjack
func NewRollingFileLogger(filePath string, level LogLevel) (*Logger, error) {
	// Rotation is handled automatically in configureOutput
	return NewFileLogger(filePath, level)
}

// ShutdownSignalHandler registers signal handlers to flush logs before shutdown
func (l *Logger) ShutdownSignalHandler() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-c
		l.Info("Received shutdown signal, flushing logs...")
		_ = l.Sync()
		os.Exit(0)
	}()
}

// responseWriter is a wrapper around http.ResponseWriter that captures the status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

// WriteHeader captures the status code
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
	rw.written = true
}

// Write captures that headers were implicitly written
func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.statusCode = http.StatusOK
		rw.written = true
	}
	return rw.ResponseWriter.Write(b)
}

// getClientIP extracts the client IP from a request
func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		ip := strings.TrimSpace(ips[0])
		if ip != "" {
			return ip
		}
	}

	if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
		return xrip
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// LogMiddleware returns an HTTP middleware that logs requests
func (l *Logger) LogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapper := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapper, r)
		duration := time.Since(start)

		l.HTTPRequest(
			r.Method,
			r.URL.Path,
			wrapper.statusCode,
			duration,
			"ip", getClientIP(r),
			"user_agent", r.UserAgent(),
			"referer", r.Referer(),
		)
	})
}

// BatchLogger provides batch logging capabilities
type BatchLogger struct {
	*Logger
	buffer    []map[string]interface{}
	bufferMu  sync.Mutex
	maxSize   int
	flushTime time.Duration
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewBatchLogger creates a new batch logger
func NewBatchLogger(logger *Logger, maxSize int, flushInterval time.Duration) *BatchLogger {
	ctx, cancel := context.WithCancel(context.Background())
	bl := &BatchLogger{
		Logger:    logger,
		buffer:    make([]map[string]interface{}, 0, maxSize),
		maxSize:   maxSize,
		flushTime: flushInterval,
		ctx:       ctx,
		cancel:    cancel,
	}

	go bl.periodicFlush()
	return bl
}

// Add adds a log entry to the batch
func (bl *BatchLogger) Add(level LogLevel, msg string, fields map[string]interface{}) {
	entry := map[string]interface{}{
		"level":     level,
		"message":   msg,
		"timestamp": time.Now(),
	}

	for k, v := range fields {
		entry[k] = v
	}

	bl.bufferMu.Lock()
	bl.buffer = append(bl.buffer, entry)

	if len(bl.buffer) >= bl.maxSize {
		bl.flushLocked()
	}
	bl.bufferMu.Unlock()
}

// periodicFlush runs a ticker to flush logs at regular intervals
func (bl *BatchLogger) periodicFlush() {
	ticker := time.NewTicker(bl.flushTime)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			bl.Flush()
		case <-bl.ctx.Done():
			bl.Flush()
			return
		}
	}
}

// flushLocked flushes the buffer (must be called with lock held)
func (bl *BatchLogger) flushLocked() {
	if len(bl.buffer) == 0 {
		return
	}

	for _, entry := range bl.buffer {
		level := entry["level"].(LogLevel)
		msg := entry["message"].(string)

		delete(entry, "level")
		delete(entry, "message")

		kvs := make([]interface{}, 0, len(entry)*2)
		for k, v := range entry {
			kvs = append(kvs, k, v)
		}

		switch level {
		case DebugLevel:
			bl.Debug(msg, kvs...)
		case InfoLevel:
			bl.Info(msg, kvs...)
		case WarnLevel:
			bl.Warn(msg, kvs...)
		case ErrorLevel:
			bl.Error(msg, kvs...)
		case FatalLevel:
			bl.Error("FATAL: "+msg, kvs...)
		case PanicLevel:
			bl.Error("PANIC: "+msg, kvs...)
		}
	}

	bl.buffer = bl.buffer[:0]
}

// Flush flushes the buffer
func (bl *BatchLogger) Flush() {
	bl.bufferMu.Lock()
	bl.flushLocked()
	bl.bufferMu.Unlock()
}

// Close stops the batch logger and flushes remaining logs
func (bl *BatchLogger) Close() {
	bl.cancel()
	bl.Flush()
}
