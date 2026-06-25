package log

import "github.com/crewjam/rfc5424"

type nilLogger struct{}

func (n nilLogger) Errorf(s string, i ...interface{}) error                    { return nil }
func (n nilLogger) Warnf(s string, i ...interface{}) error                     { return nil }
func (n nilLogger) Infof(s string, i ...interface{}) error                     { return nil }
func (n nilLogger) ErrorfWithDepth(x int, s string, i ...interface{}) error    { return nil }
func (n nilLogger) WarnfWithDepth(x int, s string, i ...interface{}) error     { return nil }
func (n nilLogger) InfofWithDepth(x int, s string, i ...interface{}) error     { return nil }
func (n nilLogger) Error(a string, i ...rfc5424.SDParam) error                 { return nil }
func (n nilLogger) Warn(a string, i ...rfc5424.SDParam) error                  { return nil }
func (n nilLogger) Info(a string, i ...rfc5424.SDParam) error                  { return nil }
func (n nilLogger) ErrorWithDepth(a int, b string, c ...rfc5424.SDParam) error { return nil }
func (n nilLogger) WarnWithDepth(a int, b string, c ...rfc5424.SDParam) error  { return nil }
func (n nilLogger) InfoWithDepth(a int, b string, c ...rfc5424.SDParam) error  { return nil }
func (n nilLogger) Hostname() string                                           { return `` }
func (n nilLogger) Appname() string                                            { return `` }

func NoLogger() IngestLogger {
	return &nilLogger{}
}
