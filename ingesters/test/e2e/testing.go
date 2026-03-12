package e2e

import "testing"

func Fatal(t *testing.T, args ...interface{}) {
	mtx.RLock()
	defer mtx.RUnlock()
	SaveTestFiles(t, instance, None, []string{
		"/opt/gravwell/etc/gravwell.conf",
		"/opt/gravwell/log/info.log",
		"/opt/gravwell/log/warn.log",
		"/opt/gravwell/log/error.log",
	})
	t.Fatal(args...)
}

func Fatalf(t *testing.T, s string, args ...interface{}) {
	mtx.RLock()
	defer mtx.RUnlock()
	SaveTestFiles(t, instance, None, []string{
		"/opt/gravwell/etc/gravwell.conf",
		"/opt/gravwell/log/info.log",
		"/opt/gravwell/log/warn.log",
		"/opt/gravwell/log/error.log",
	})
	t.Fatalf(s, args...)
}
