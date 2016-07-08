// Package log implements an advanced logger with more easily-analyzed log output.
package log

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

const (
	logInfo int = iota
	logWarning
	logError
	logFatal

	defaultColor = "\x1b[0m"
	infoColor    = "\x1b[32m"
	warningColor = "\x1b[33m"
	errorColor   = "\x1b[31m"
)

var (
	logColor = map[int]string{
		logInfo:    infoColor,
		logWarning: warningColor,
		logError:   errorColor,
		logFatal:   errorColor,
	}

	logPrefix = map[int]string{
		logInfo:    "I",
		logWarning: "W",
		logError:   "E",
		logFatal:   "FATAL",
	}

	defaultLogger  *logger
	logBase        = "/var/log"
	defaultLogFile *os.File
	logFiles       []*os.File
)

type LogOptions struct {
	Verbosity int
	Colorful  bool
	LogDir    string
	Timestamp bool
}

// Init initializes the logging package.
func Init(opts *LogOptions) {
	var logWriters = []io.Writer{}
	_, exName := path.Split(os.Args[0])
	pid := os.Getpid()

	if opts.LogDir != "" {
		logBase = opts.LogDir
	}

	defaultLogName := fmt.Sprintf("%s/%d-%s-%d.log", logBase, time.Now().Unix(), exName, pid)
	defaultLogFile, err := os.Create(defaultLogName)
	if err == nil {
		logFiles = append(logFiles, defaultLogFile)
	}
	defaultLogger = NewLogger(true, opts.Colorful, opts.Timestamp, logWriters...).(*logger)
	defaultLogger.SetVerbosity(opts.Verbosity)
	// The default logger skips an extra stack frame when it logs to account for the package
	// convenience functions.
	defaultLogger.callerSkip++

	if err != nil {
		Warningf("unable to open default log file: %v", err)
	}
}

// Logger provides an interface to enhanced logging functionality.
type Logger interface {
	// Error formats an error message using the default formats for its operands and writes to the
	// error log destinations.
	Error(a ...interface{})
	// Errorf formats an error message according to a format specifier and writes to the error log
	// destinations.
	Errorf(format string, a ...interface{})

	// Fatal formats a fatal error message using the default formats for its operands, writes to the
	// error log destinations, and then panics.
	Fatal(a ...interface{})
	// Fatalf formats a fatal error message according to a format specifier, writes to the error log
	// destinations, and then panics.
	Fatalf(format string, a ...interface{})

	// Info formats an info message using the default formats for its operands and writes to the
	// info log destinations.
	Info(a ...interface{})
	// Infof formats an info message according to a format specifier and writes to the info log
	// destinations.
	Infof(format string, a ...interface{})

	// Warning formats a warning message using the default formats for its operands and writes to
	// the warning log destinations.
	Warning(a ...interface{})
	// Warningf formats according to a format specifier and writes to the warning log destinations.
	Warningf(format string, a ...interface{})

	// VError formats an error message using the default formats for its operands and writes to the
	// error log destinations if the logger verbosity is sufficiently high.
	VError(v int, a ...interface{})
	// VErrorf formats an error message according to a format specifier and writes to the error log
	// destinations if the logger verbosity is sufficiently high.
	VErrorf(v int, format string, a ...interface{})

	// VInfo formats an info message using the default formats for its operands and writes to the
	// info log destinations if the logger verbosity is sufficiently high.
	VInfo(v int, a ...interface{})
	// VInfof formats an info message according to a format specifier and writes to the info log
	// destinations if the logger verbosity is sufficiently high.
	VInfof(v int, format string, a ...interface{})

	// VWarning formats a warning message using the default formats for its operands and writes to
	// the warning log destinations if the logger verbosity is sufficiently high.
	VWarning(v int, a ...interface{})
	// VWarningf formats according to a format specifier and writes to the warning log destinations
	// if the logger verbosity is sufficiently high.
	VWarningf(v int, format string, a ...interface{})

	// SetVerbosity sets the output verbosity level. Output that is logged at a verbosity level >v
	// will not be output to the logs.
	SetVerbosity(v int)

	// SetDefaultVerbosity sets the default level of verbosity for outgoing logging messages from
	// this point forward. Note that this affects all future function calls until the next call of
	// SetDefaultVerbosity.
	SetDefaultVerbosity(v int)
}

// logger implements the Logger interface.
type logger struct {
	// Stores counts of log levels, mapping log levels to recorded counts. Using a map instead of
	// a slice gives us zero values for an unbounded set of log levels without having to iterate
	// make any special future alterations to the way log levels are counted.
	count map[int]int64

	// The mutex used to synchronize operations on the log object.
	mu sync.Mutex

	// callerSkip is used to determine how many stack frames to skip for logging. Usually this will
	// be 3, but the default logger will skip an extra frame to bypass the package-level convenience
	// functions.
	callerSkip int

	// verbosity required to output logging messages
	verbosity int

	// default verbosity level for logging calls
	defaultVerbosity int

	// determines whether logs should be written to stderr. stderr logs will be colorful if
	// colorful is set to true.
	logToStderr bool

	// determines whether to write colorful logs to stderr only. File logs will never be
	// written colorful.
	colorful bool

	// determines whether or not the logger will write out a timestamp.
	timestamp bool

	// writer to which file logs will be written.
	writer io.Writer
}

// NewLogger returns a new Logger that logs to the specified files..
func NewLogger(logToStderr bool, colorful bool, timestamp bool, logFiles ...io.Writer) Logger {
	l := &logger{
		count:       map[int]int64{},
		callerSkip:  3,
		logToStderr: logToStderr,
		colorful:    colorful,
		writer:      io.MultiWriter(logFiles...),
		timestamp:   timestamp,
	}
	return l
}

// write takes the log level and a logging string produced by log or logf and writes the log
// message, updating the count for that log level.
func (l *logger) write(logLevel int, s, file string, line int, callerOK bool) {
	var color string
	file = filepath.Base(file)
	if !callerOK {
		file, line = "unknown file", 0
	}

	var prefix string
	if logLevel == logFatal {
		prefix = fmt.Sprintf("[%s]", logPrefix[logFatal])
	} else {
		prefix = fmt.Sprintf("[%s%04d]", logPrefix[logLevel], l.count[logLevel])
	}

	if l.timestamp {
		prefix = fmt.Sprintf("%s %s", time.Now().String(), prefix)
	}
	s = fmt.Sprintf("%s %s:%d: %s", prefix, file, line, s)

	if l.logToStderr {
		if l.colorful {
			color = logColor[logLevel]
		}
		fmt.Fprintln(os.Stderr, color+s+defaultColor)
	}

	if logLevel == logFatal {
		go func() {
			time.Sleep(time.Second / 2)
			panic(fmt.Errorf("timeout waiting for fatal log to write to disk. Log message follows:\n%s", s))
		}()
		fmt.Fprintln(l.writer, s)
		// Fatal logs are a little different from everything else because we panic at the end.
		panic(s)
	}

	fmt.Fprintln(l.writer, s)

	l.count[logLevel]++
}

// log is used to print a log message using the default format interfaces (Info, Error, Warning)
func (l *logger) log(verbosity int, logLevel int, a ...interface{}) {
	_, file, line, ok := runtime.Caller(l.callerSkip - 1)
	l.mu.Lock()
	defer l.mu.Unlock()
	if verbosity > l.verbosity {
		return
	}

	s := fmt.Sprint(a...)
	l.write(logLevel, s, file, line, ok)
}

// logf is used to print a log message using the format string interfaces (Infof, Errof, Warningf)
func (l *logger) logf(verbosity int, logLevel int, format string, a ...interface{}) {
	_, file, line, ok := runtime.Caller(l.callerSkip - 1)
	l.mu.Lock()
	defer l.mu.Unlock()
	if verbosity > l.verbosity {
		return
	}

	s := fmt.Sprintf(format, a...)
	l.write(logLevel, s, file, line, ok)
}

// Info implements the Logger interface.
func (l *logger) Info(a ...interface{}) {
	l.log(l.defaultVerbosity, logInfo, a...)
}

// Warning implements the Logger interface.
func (l *logger) Warning(a ...interface{}) {
	l.log(l.defaultVerbosity, logWarning, a...)
}

// Error implements the Logger interface.
func (l *logger) Error(a ...interface{}) {
	l.log(l.defaultVerbosity, logError, a...)
}

// Fatal implements the Logger interface.
func (l *logger) Fatal(a ...interface{}) {
	// Verbosity level is 0 because we always log fatal messages.
	l.log(0, logFatal, a...)
}

// Infof implements the Logger interface.
func (l *logger) Infof(format string, a ...interface{}) {
	l.logf(l.defaultVerbosity, logInfo, format, a...)
}

// Warningf implements the Logger interface.
func (l *logger) Warningf(format string, a ...interface{}) {
	l.logf(l.defaultVerbosity, logWarning, format, a...)
}

// Errorf implements the Logger interface.
func (l *logger) Errorf(format string, a ...interface{}) {
	l.logf(l.defaultVerbosity, logError, format, a...)
}

// Fatalf implements the Logger interface.
func (l *logger) Fatalf(format string, a ...interface{}) {
	// Verbosity level is 0 because we always log fatal messages.
	l.logf(0, logFatal, format, a...)
}

// SetDefaultVerbosity implements the Logger interface.
func (l *logger) SetDefaultVerbosity(v int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.defaultVerbosity = v
}

// SetVerbosity implements the Logger interface.
func (l *logger) SetVerbosity(v int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.verbosity = v
}

// VInfo implements the Logger interface.
func (l *logger) VInfo(verbosity int, a ...interface{}) {
	l.log(verbosity, logInfo, a...)
}

// VWarning implements the Logger interface.
func (l *logger) VWarning(verbosity int, a ...interface{}) {
	l.log(verbosity, logWarning, a...)
}

// VError implements the Logger interface.
func (l *logger) VError(verbosity int, a ...interface{}) {
	l.log(verbosity, logError, a...)
}

// VInfof implements the Logger interface.
func (l *logger) VInfof(verbosity int, format string, a ...interface{}) {
	l.logf(verbosity, logInfo, format, a...)
}

// VWarningf implements the Logger interface.
func (l *logger) VWarningf(verbosity int, format string, a ...interface{}) {
	l.logf(verbosity, logWarning, format, a...)
}

// VErrorf implements the Logger interface.
func (l *logger) VErrorf(verbosity int, format string, a ...interface{}) {
	l.logf(verbosity, logError, format, a...)
}

// Default logger convenience functions

// Info is a convenience method that calls defaultLogger.Info(a..)
func Info(a ...interface{}) {
	defaultLogger.Info(a...)
}

// Warning is a convenience method that calls defaultLogger.Warning(a..)
func Warning(a ...interface{}) {
	defaultLogger.Warning(a...)
}

// Error is a convenience method that calls defaultLogger.Error(a..)
func Error(a ...interface{}) {
	defaultLogger.Error(a...)
}

// Fatal is a convenience method that calls defaultLogger.Fatal(a...)
func Fatal(a ...interface{}) {
	defaultLogger.Fatal(a...)
}

// Infof is a convenience method that calls defaultLogger.Infof(format, a..)
func Infof(format string, a ...interface{}) {
	defaultLogger.Infof(format, a...)
}

// Warningf is a convenience method that calls defaultLogger.Warningf(format, a..)
func Warningf(format string, a ...interface{}) {
	defaultLogger.Warningf(format, a...)
}

// Errorf is a convenience method that calls defaultLogger.Errorf(format, a..)
func Errorf(format string, a ...interface{}) {
	defaultLogger.Errorf(format, a...)
}

// Fatalf is a convenience method that calls defaultLogger.Fatalf(format, a..)
func Fatalf(format string, a ...interface{}) {
	defaultLogger.Fatalf(format, a...)
}

// VInfo is a convenience method that calls defaultLogger.VInfo(verbosity, a...)
func VInfo(verbosity int, a ...interface{}) {
	defaultLogger.VInfo(verbosity, a...)
}

// VWarning is a convenience method that calls defaultLogger.VVWarning(verbosity, a...)
func VWarning(verbosity int, a ...interface{}) {
	defaultLogger.VWarning(verbosity, a...)
}

// VError is a convenience method that calls defaultLogger.VVError(verbosity, a...)
func VError(verbosity int, a ...interface{}) {
	defaultLogger.VError(verbosity, a...)
}

// VInfof is a convenience method that calls defaultLogger.VVInfof(verbosity, format, a...)
func VInfof(verbosity int, format string, a ...interface{}) {
	defaultLogger.VInfof(verbosity, format, a...)
}

// VWarningf is a convenience method that calls defaultLogger.VVWarningf(verbosity, format, a...)
func VWarningf(verbosity int, format string, a ...interface{}) {
	defaultLogger.VWarningf(verbosity, format, a...)
}

// VErrorf is a convenience method that calls defaultLogger.VVErrorf(verbosity, format, a...)
func VErrorf(verbosity int, format string, a ...interface{}) {
	defaultLogger.VErrorf(verbosity, format, a...)
}
