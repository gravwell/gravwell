package main

import (
	"io"
	"net/http"
	"time"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

type debugLogger interface {
	Debug(msg string, sds ...rfc5424.SDParam) error
}

type debugMiddleware struct {
	logger debugLogger
	next   http.Handler
}

func newDebugMiddleware(logger debugLogger, next http.Handler) *debugMiddleware {
	return &debugMiddleware{
		logger: logger,
		next:   next,
	}
}

func (d *debugMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	wrapped := &trackingRW{
		ResponseWriter: w,
	}
	body := &trackingRC{
		rc: r.Body,
	}
	r.Body = body

	d.next.ServeHTTP(wrapped, r)

	rkv := requestKV(wrapped, r)
	d.logger.Debug("http debug", rkv...)
	debugout("http debug: %s %v\n", time.Now().Format(time.RFC3339), rkv)
	// We don't want to log the headers as that likely makes it to a gravwell instance.
	// This would result in token leaks.
	for k, v := range r.Header {
		debugout("\t%v: %v\n", k, v)
	}
}

// trackingRC wraps an io.ReadCloser and keeps track of how many bytes were read.
// Intended for use with http bodies.
type trackingRC struct {
	rc    io.ReadCloser
	bytes int
}

func (t *trackingRC) Read(p []byte) (int, error) {
	i, err := t.rc.Read(p)
	if i > 0 {
		t.bytes += i
	}
	return i, err
}

func (t *trackingRC) Close() error {
	return t.rc.Close()
}

type trackingRW struct {
	http.ResponseWriter
	code  int
	bytes int
}

func (trw *trackingRW) WriteHeader(code int) {
	trw.code = code
	trw.ResponseWriter.WriteHeader(code)
}

func (trw *trackingRW) Write(b []byte) (r int, err error) {
	r, err = trw.ResponseWriter.Write(b)
	trw.bytes += r
	if trw.code == 0 {
		trw.code = 200
	}
	return
}

// requestKV gets commons request metadata and returns a slice of log params.
// This should be run after writing the response (eg in a defered func) in
// order to capture the response code and bytes written.
func requestKV(w http.ResponseWriter, r *http.Request) []rfc5424.SDParam {
	params := make([]rfc5424.SDParam, 0, 5)
	if trw, ok := w.(*trackingRW); ok {
		params = append(params, log.KV("code", trw.code))

	}
	if trc, ok := r.Body.(*trackingRC); ok {
		params = append(params, log.KV("bytes", trc.bytes))
	}
	return append(params,
		log.KV("method", r.Method),
		log.KV("url", r.URL.RequestURI()),
		log.KV("ip", getRemoteIP(r)),
	)
}
