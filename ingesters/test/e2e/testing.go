package e2e

import "testing"

func Fatal(t *testing.T, args ...interface{}) {
	t.Helper()
	defer t.Fatal(args...)
	mtx.RLock()
	defer mtx.RUnlock()
	if instance == nil {
		return
	}
	SaveTestFiles(t, instance, None, []string{
		"/opt/gravwell/etc/gravwell.conf",
		"/opt/gravwell/log/info.log",
		"/opt/gravwell/log/warn.log",
		"/opt/gravwell/log/error.log",
	})
}

func Fatalf(t *testing.T, s string, args ...interface{}) {
	t.Helper()
	defer t.Fatalf(s, args...)
	mtx.RLock()
	defer mtx.RUnlock()
	if instance == nil {
		return
	}
	SaveTestFiles(t, instance, None, []string{
		"/opt/gravwell/etc/gravwell.conf",
		"/opt/gravwell/log/info.log",
		"/opt/gravwell/log/warn.log",
		"/opt/gravwell/log/error.log",
	})
}
