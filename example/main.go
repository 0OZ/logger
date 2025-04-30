package main

import (
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/0Oz/logger"
)

func main() {
	// Create a basic logger
	log := logger.New()
	log.Info("Starting example application")

	// Create a pretty console logger for development
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

	// Demonstrate HTTP request logging (simulated)
	log.HTTPRequest("GET", "/api/users", 200, 123*time.Millisecond,
		"user_agent", "Mozilla/5.0",
		"user_id", "123456",
	)

	// Demonstrate formatted logging
	log.Infof("Server started on port %d", 8080)
	log.Warnf("Resource usage at %.2f%%", 85.75)

	// Demonstrate batch logging
	batchLogger := logger.NewBatchLogger(log, 10, 3*time.Second)
	for i := 0; i < 5; i++ {
		batchLogger.Add(logger.InfoLevel, "Batch log entry", map[string]interface{}{
			"index": i,
			"value": "test",
		})
	}
	batchLogger.Flush() // Force flush

	// Create file logger (if you want to see file output)
	fileLogger, err := logger.NewFileLogger("./logs/application.log", logger.InfoLevel)
	if err != nil {
		log.WithError(err).Error("Failed to create file logger")
	} else {
		fileLogger.Info("This message is written to a file")
	}

	// Create rotating file logger (note: rolling not fully implemented)
	rotLogger, err := logger.NewRollingFileLogger("./logs/rotating.log", logger.InfoLevel)
	if err != nil {
		log.WithError(err).Error("Failed to create rolling file logger")
	} else {
		rotLogger.Info("This message is written to a rolling log file (not fully implemented)")
	}

	// Demonstrate HTTP middleware
	http.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pong"))
	})

	// Only start HTTP server if explicitly enabled
	if len(os.Args) > 1 && os.Args[1] == "--server" {
		log.Info("Starting HTTP server on :8080")
		server := &http.Server{
			Addr:    ":8080",
			Handler: log.LogMiddleware(http.DefaultServeMux),
		}
		log.Fatal("Server failed", server.ListenAndServe())
	}

	log.Info("Example completed")
}
