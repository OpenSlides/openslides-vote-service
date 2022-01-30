package log

import (
	"log"
	"sync"
)

var (
	loggerMu    sync.RWMutex
	debugLogger *log.Logger
	infoLogger  *log.Logger
)

// SetDebugLogger sets the debug logger. The default is no log at all.
//
// This function should only be started at the beginnen of the program before
// the Debug was called for the frist time.
func SetDebugLogger(l *log.Logger) {
	loggerMu.Lock()
	defer loggerMu.Unlock()
	debugLogger = l
}

// SetInfoLogger sets the Logger for info messages. The default is log.Default()
//
// This function should only be started at the beginnen of the program before
// the Debug was called for the frist time.
func SetInfoLogger(l *log.Logger) {
	loggerMu.Lock()
	defer loggerMu.Unlock()
	infoLogger = l
}

// Info prints output that is important for the user.
func Info(format string, a ...interface{}) {
	loggerMu.RLock()
	defer loggerMu.RUnlock()

	if infoLogger == nil {
		return
	}
	infoLogger.Printf(format, a...)
}

// Debug prints output that is important for development and debugging.
//
// If EnableDebug() was not called, this function is a noop.
func Debug(format string, a ...interface{}) {
	loggerMu.RLock()
	defer loggerMu.RUnlock()

	if debugLogger == nil {
		return
	}

	debugLogger.Printf(format, a...)
}

// IsDebug returns if debug output is enabled.
func IsDebug() bool {
	return debugLogger != nil
}
