package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"

	"github.com/crewjam/rfc5424"
	"github.com/gravwell/gravwell/v4/ingest/log"
)

const redacted = `REDACTED`

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
	logger      debugLogger
	next        http.Handler
	allRequests bool
}

func newDebugLoggingMiddleware(next http.Handler, logger debugLogger, all bool) *debugLoggingMiddlware {
	return &debugLoggingMiddlware{
		logger:      logger,
		next:        next,
		allRequests: all,
	}
}

func (d *debugLoggingMiddlware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.next.ServeHTTP(w, r)

	if d.allRequests || responseCode(w) >= 400 {
		rkv := requestKV(w, r)
		d.logger.Debug("http debug", rkv...)
		logRequestHeaders(d.logger, r)
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

	if responseCode(w) >= 400 {
		rkv := requestKV(w, r)
		d.logger.Debug("http debug", rkv...)
		logRequestHeaders(d.logger, r)
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
		logRequestHeaders(d.logger, r)
	}

	return err
}

func (d *debugLoggingAuther) Login(w http.ResponseWriter, r *http.Request) {
	d.next.Login(w, r)
	if responseCode(w) >= 400 {
		rkv := requestKV(w, r)
		d.logger.Debug("http debug", rkv...)
		logRequestHeaders(d.logger, r)
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
		log.KV("url", redactedRequestURI(r)),
		log.KV("ip", getRemoteIP(r)),
	)
}

// redactedRequestURI returns the request path and query for logging with every
// query value masked, since preshared-parameter listeners and the HEC health
// check carry their token in the query.
func redactedRequestURI(r *http.Request) string {
	q := r.URL.Query()
	if len(q) == 0 {
		return r.URL.EscapedPath()
	}
	for _, vs := range q {
		for i := range vs {
			vs[i] = redacted
		}
	}
	return r.URL.EscapedPath() + "?" + q.Encode()
}

// loggableHeaders are the request headers whose values are safe to log. Every
// other header value is masked, because auth tokens can travel in any header
// (preshared-header listeners let the operator pick the header name).
var loggableHeaders = map[string]bool{
	"Accept":                   true,
	"Accept-Encoding":          true,
	"Connection":               true,
	"Content-Encoding":         true,
	"Content-Length":           true,
	"Content-Type":             true,
	"Transfer-Encoding":        true,
	"User-Agent":               true,
	"X-Forwarded-For":          true,
	"X-Real-Ip":                true,
	"X-Splunk-Request-Channel": true,
}

// logRequestHeaders writes one debug line per request header, masking the
// values of headers not in loggableHeaders.
func logRequestHeaders(lgr debugLogger, r *http.Request) {
	for k, v := range r.Header {
		if loggableHeaders[http.CanonicalHeaderKey(k)] {
			lgr.Debug(fmt.Sprintf("%v: %v", k, v))
		} else {
			lgr.Debug(fmt.Sprintf("%v: [%s]", k, redacted))
		}
	}
}

func responseCode(w http.ResponseWriter) int {
	if trw, ok := w.(*trackingRW); ok {
		return trw.code
	}
	return 0
}
