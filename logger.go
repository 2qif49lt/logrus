package logrus

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defmaxfilesize  = 1024 * 1024 * 10
	defmaxfilecount = 10
)

type FileHandler interface {
	DoFile(path string) error
}
type FileFunc func(string) error

func (f FileFunc) DoFile(path string) error {
	return f(path)
}

var DefaultFileFunc = func(path string) error {
	return os.Remove(path)
}

type Logger struct {
	// The logs are `io.Copy`'d to this in a mutex. It's common to set this to a
	// file, or leave it default which is `os.Stderr`. You can also set this to
	// something more adventorous, such as logging to Kafka.
	Out io.Writer
	// Hooks for the logger instance. These allow firing events based on logging
	// levels and log entries. For example, to send errors to an error tracking
	// service, log to StatsD or dump the core on fatal errors.
	Hooks LevelHooks
	// All log entries pass through the formatter before logged to Out. The
	// included formatters are `TextFormatter` and `JSONFormatter` for which
	// TextFormatter is the default. In development (when a TTY is attached) it
	// logs with colors, but to a file it wouldn't. You can easily implement your
	// own that implements the `Formatter` interface, see the `README` or included
	// formatters for examples.
	Formatter Formatter
	// The logging level the logger should log at. This is typically (and defaults
	// to) `logrus.Info`, which allows Info(), Warn(), Error() and Fatal() to be
	// logged. `logrus.Debug` is useful in
	Level Level
	// Used to sync writing to the log.
	mu sync.Mutex

	savespace bool     // 是否节省空间
	Fcount    int      // 文件个数
	Fmaxsize  int      // 最大文件大小
	file      []string // 文件列表
	folder    string
	name      string
	fh        FileHandler
}

// Creates a new logger. Configuration should be set by changing `Formatter`,
// `Out` and `Hooks` directly on the default logger instance. You can also just
// instantiate your own:
//
//    var log = &Logger{
//      Out: os.Stderr,
//      Formatter: new(JSONFormatter),
//      Hooks: make(LevelHooks),
//      Level: logrus.DebugLevel,
//    }
//
// It's recommended to make this a global instance called `log`.
func New() *Logger {
	return &Logger{
		Out:       os.Stderr,
		Formatter: new(TextFormatter),
		Hooks:     make(LevelHooks),
		Level:     InfoLevel,
	}
}

// 快速创建一个在当前执行程序所在目录里日志文件
func NewSSLog(folder string, name string, lvl Level) *Logger {
	l := &Logger{
		Out: nil,
		Formatter: &TextFormatter{
			DisableColors:   true,
			TimestampFormat: "2006-01-02 15:04:05",
		},
		Hooks:     make(LevelHooks),
		Level:     lvl % (DebugLevel + 1),
		savespace: true,
		Fcount:    defmaxfilecount,
		Fmaxsize:  defmaxfilesize,
		folder:    folder,
		name:      name,
		fh:        FileFunc(DefaultFileFunc),
	}

	wrter := l.createIo()
	if wrter == nil {
		return nil
	}
	l.Out = wrter
	return l
}
func getProcAbsDir() (string, error) {
	abs, err := filepath.Abs(os.Args[0])
	if err != nil {
		return "", nil
	}
	return filepath.Dir(abs), nil
}
func isPathExist(path string) bool {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false
	}
	return true
}
func getTimeStr() string {
	t := time.Now()
	year, month, day := t.Date()
	hour, minute, second := t.Clock()
	str := fmt.Sprintf("%04d%02d%02d%02d%02d%02d", year, month, day, hour, minute, second)
	return str
}
func isFile(w io.Writer) bool {
	f, ok := w.(*os.File)
	if ok == false {
		return false
	}

	fs, err := f.Stat()
	if err != nil {
		return false
	}

	return fs.Mode().IsRegular()
}

func (l *Logger) createIo() io.Writer {

	if l.savespace == false {
		return l.Out
	}
	procfolder, err := getProcAbsDir()
	if err != nil {
		return nil
	}

	timestr := getTimeStr()
	logfolder := filepath.Join(procfolder, l.folder, timestr)
	if len(l.file) > 0 {
		logfolder = filepath.Dir(l.file[0])
	}

	if isPathExist(logfolder) == false {
		err = os.MkdirAll(logfolder, os.ModePerm)
		if err != nil {
			fmt.Println("mkdirall return nil", err)
			return nil
		}
	}

	fileext := ""
	filename := l.name
	extdotindex := strings.LastIndex(l.name, ".")
	if extdotindex != -1 {
		fileext = l.name[extdotindex:]
		filename = l.name[:extdotindex]
	}

	logfilename := fmt.Sprintf("%s/%s-%s%s", logfolder, filename, timestr, fileext)

	file, err := os.OpenFile(logfilename, os.O_RDWR|os.O_CREATE|os.O_TRUNC, os.ModePerm)
	if err != nil {
		return nil
	}

	l.file = append(l.file, logfilename)
	if len(l.file) > l.Fcount {
		oldestfile := l.file[0]
		if l.fh != nil {
			err = l.fh.DoFile(oldestfile)
			if err != nil {
				fmt.Println("dofile return", err)
			} else {
				fmt.Println("dofile return succ")
			}
		}
		l.file = l.file[1:]
	}

	return file
}
func (l *Logger) SetFileHandler(handler FileHandler) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.fh = handler
}

func (l *Logger) SetFileFunc(handler func(string) error) {
	l.SetFileHandler(FileFunc(handler))
}

// Adds a field to the log entry, note that you it doesn't log until you call
// Debug, Print, Info, Warn, Fatal or Panic. It only creates a log entry.
// If you want multiple fields, use `WithFields`.
func (logger *Logger) WithField(key string, value interface{}) *Entry {
	return NewEntry(logger).WithField(key, value)
}

func (logger *Logger) WithTryJson(value interface{}) *Entry {
	return NewEntry(logger).WithTryJson(value)
}

// Adds a struct of fields to the log entry. All it does is call `WithField` for
// each `Field`.
func (logger *Logger) WithFields(fields Fields) *Entry {
	return NewEntry(logger).WithFields(fields)
}

// Add an error as single field to the log entry.  All it does is call
// `WithError` for the given `error`.
func (logger *Logger) WithError(err error) *Entry {
	return NewEntry(logger).WithError(err)
}

func (logger *Logger) Debugf(format string, args ...interface{}) {
	if logger.Level >= DebugLevel {
		NewEntry(logger).Debugf(format, args...)
	}
}

func (logger *Logger) Infof(format string, args ...interface{}) {
	if logger.Level >= InfoLevel {
		NewEntry(logger).Infof(format, args...)
	}
}

func (logger *Logger) Printf(format string, args ...interface{}) {
	NewEntry(logger).Printf(format, args...)
}

func (logger *Logger) Warnf(format string, args ...interface{}) {
	if logger.Level >= WarnLevel {
		NewEntry(logger).Warnf(format, args...)
	}
}

func (logger *Logger) Warningf(format string, args ...interface{}) {
	if logger.Level >= WarnLevel {
		NewEntry(logger).Warnf(format, args...)
	}
}

func (logger *Logger) Errorf(format string, args ...interface{}) {
	if logger.Level >= ErrorLevel {
		NewEntry(logger).Errorf(format, args...)
	}
}

func (logger *Logger) Fatalf(format string, args ...interface{}) {
	if logger.Level >= FatalLevel {
		NewEntry(logger).Fatalf(format, args...)
	}
	os.Exit(1)
}

func (logger *Logger) Panicf(format string, args ...interface{}) {
	if logger.Level >= PanicLevel {
		NewEntry(logger).Panicf(format, args...)
	}
}

func (logger *Logger) Debug(args ...interface{}) {
	if logger.Level >= DebugLevel {
		NewEntry(logger).Debug(args...)
	}
}

func (logger *Logger) Info(args ...interface{}) {
	if logger.Level >= InfoLevel {
		NewEntry(logger).Info(args...)
	}
}

func (logger *Logger) Print(args ...interface{}) {
	NewEntry(logger).Info(args...)
}

func (logger *Logger) Warn(args ...interface{}) {
	if logger.Level >= WarnLevel {
		NewEntry(logger).Warn(args...)
	}
}

func (logger *Logger) Warning(args ...interface{}) {
	if logger.Level >= WarnLevel {
		NewEntry(logger).Warn(args...)
	}
}

func (logger *Logger) Error(args ...interface{}) {
	if logger.Level >= ErrorLevel {
		NewEntry(logger).Error(args...)
	}
}

func (logger *Logger) Fatal(args ...interface{}) {
	if logger.Level >= FatalLevel {
		NewEntry(logger).Fatal(args...)
	}
	os.Exit(1)
}

func (logger *Logger) Panic(args ...interface{}) {
	if logger.Level >= PanicLevel {
		NewEntry(logger).Panic(args...)
	}
}

func (logger *Logger) Debugln(args ...interface{}) {
	if logger.Level >= DebugLevel {
		NewEntry(logger).Debugln(args...)
	}
}

func (logger *Logger) Infoln(args ...interface{}) {
	if logger.Level >= InfoLevel {
		NewEntry(logger).Infoln(args...)
	}
}

func (logger *Logger) Println(args ...interface{}) {
	NewEntry(logger).Println(args...)
}

func (logger *Logger) Warnln(args ...interface{}) {
	if logger.Level >= WarnLevel {
		NewEntry(logger).Warnln(args...)
	}
}

func (logger *Logger) Warningln(args ...interface{}) {
	if logger.Level >= WarnLevel {
		NewEntry(logger).Warnln(args...)
	}
}

func (logger *Logger) Errorln(args ...interface{}) {
	if logger.Level >= ErrorLevel {
		NewEntry(logger).Errorln(args...)
	}
}

func (logger *Logger) Fatalln(args ...interface{}) {
	if logger.Level >= FatalLevel {
		NewEntry(logger).Fatalln(args...)
	}
	os.Exit(1)
}

func (logger *Logger) Panicln(args ...interface{}) {
	if logger.Level >= PanicLevel {
		NewEntry(logger).Panicln(args...)
	}
}
