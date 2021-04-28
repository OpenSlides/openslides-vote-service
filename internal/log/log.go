package log

import "log"

var (
	debugLogger *log.Logger
	infoLogger  *log.Logger
)

// SetDebugLogger sets the debug logger. The default is no log at all.
//
// This function should only be started at the beginnen of the program before
// the Debug was called for the frist time.
func SetDebugLogger(l *log.Logger) {
	debugLogger = l
}

// SetInfoLogger sets the Logger for info messages. The default is log.Default()
//
// This function should only be started at the beginnen of the program before
// the Debug was called for the frist time.
func SetInfoLogger(l *log.Logger) {
	infoLogger = l
}

// Info prints output that is important for the user.
func Info(format string, a ...interface{}) {
	if infoLogger == nil {
		infoLogger = log.Default()
	}
	infoLogger.Printf(format, a...)
}

// Debug prints output that is important for development and debugging.
//
// If EnableDebug() was not called, this function is a noop.
func Debug(format string, a ...interface{}) {
	if debugLogger == nil {
		return
	}

	debugLogger.Printf(format, a...)
}

// IsDebug returns if debug output is enabled.
func IsDebug() bool {
	return debugLogger != nil
}
