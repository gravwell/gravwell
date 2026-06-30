package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v3/ingest/log"
)

var DefaultDebugLogger debugLogger

func init() {
	l := log.New(os.Stdout)
	l.SetLevel(log.DEBUG)
	DefaultDebugLogger = l
}

type debugLogger interface {
	Debug(msg string, sds ...rfc5424.SDParam) error
}

type debugLoggingMiddlware struct {
	logger debugLogger
	next   http.Handler
}

func newDebugLoggingMiddleware(next http.Handler, logger debugLogger) *debugLoggingMiddlware {
	return &debugLoggingMiddlware{
		logger: logger,
		next:   next,
	}
}

func (d *debugLoggingMiddlware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.next.ServeHTTP(w, r)

	rkv := requestKV(w, r)
	d.logger.Debug("http debug", rkv...)
	for k, v := range r.Header {
		d.logger.Debug(fmt.Sprintf("%v: %v", k, v))
	}
}

type debugLoggingHandler struct {
	logger debugLogger
	next   handleFunc
}

func newDebugLoggingHandler(next handleFunc, logger debugLogger) *debugLoggingHandler {
	return &debugLoggingHandler{
		logger: logger,
		next:   next,
	}
}

func (d *debugLoggingHandler) Handle(h *handler, rh routeHandler, w http.ResponseWriter, r *http.Request, rdr io.Reader, ip net.IP) {
	d.next(h, rh, w, r, rdr, ip)

	rkv := requestKV(w, r)
	d.logger.Debug("http debug", rkv...)
	for k, v := range r.Header {
		d.logger.Debug(fmt.Sprintf("%v: %v", k, v))
	}
}

type debugLoggingAuther struct {
	logger debugLogger
	next   authHandler
}

func newDebugLoggingAuther(next authHandler, logger debugLogger) *debugLoggingAuther {
	return &debugLoggingAuther{
		logger: logger,
		next:   next,
	}
}

func (d *debugLoggingAuther) AuthRequest(r *http.Request) error {
	err := d.next.AuthRequest(r)
	// This is really wacky. We only handle the error case as otherwise the
	// request continues to another handler...
	if err != nil {
		rw := &trackingRW{
			code: http.StatusUnauthorized, // lie because custom interfaces...
		}
		rkv := requestKV(rw, r)
		d.logger.Debug("http debug", rkv...)
		for k, v := range r.Header {
			d.logger.Debug(fmt.Sprintf("%v: %v", k, v))
		}
	}

	return err
}

func (d *debugLoggingAuther) Login(w http.ResponseWriter, r *http.Request) {
	d.next.Login(w, r)
	rkv := requestKV(w, r)
	d.logger.Debug("http debug", rkv...)
	for k, v := range r.Header {
		d.logger.Debug(fmt.Sprintf("%v: %v", k, v))
	}
}

type trackingMiddleware struct {
	next http.Handler
}

func newTrackingMiddleware(next http.Handler) *trackingMiddleware {
	return &trackingMiddleware{
		next: next,
	}
}

func (d *trackingMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	wrapped := &trackingRW{
		ResponseWriter: w,
	}
	body := &trackingRC{
		rc: r.Body,
	}
	r.Body = body

	d.next.ServeHTTP(wrapped, r)
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
// order to capture the response code and bytes read.
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
