package log

var Logger ILogger = nopLogger{}

type ILogger interface {
	Debug(a ...interface{})
	Debugf(format string, a ...interface{})
	Info(a ...interface{})
	Infof(format string, a ...interface{})
	Error(a ...interface{})
	Errorf(format string, a ...interface{})
	Fatal(a ...interface{})
	Fatalf(format string, a ...interface{})
}

type nopLogger struct{}

func (nopLogger) Debug(a ...interface{})                 {}
func (nopLogger) Debugf(format string, a ...interface{}) {}
func (nopLogger) Info(a ...interface{})                  {}
func (nopLogger) Infof(format string, a ...interface{})  {}
func (nopLogger) Error(a ...interface{})                 {}
func (nopLogger) Errorf(format string, a ...interface{}) {}
func (nopLogger) Fatal(a ...interface{})                 {}
func (nopLogger) Fatalf(format string, a ...interface{}) {}
