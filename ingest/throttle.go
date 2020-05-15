package ingest

import (
	"context"
	"net"
	"time"

	"golang.org/x/time/rate"
)

const (
	defaultBurstMultiplier = 1
)

type parent struct {
	burst int
	lm    *rate.Limiter
}

type throttleConn struct {
	net.Conn
	burst int
	lm    *rate.Limiter
	to    time.Duration
	ctx   context.Context
	cncl  func()
}

type conn interface {
	net.Conn
	SetReadTimeout(time.Duration) error
	SetWriteTimeout(time.Duration) error
	ClearWriteTimeout() error
	ClearReadTimeout() error
}

func newParent(bps int64, burstMult int) *parent {
	if burstMult <= 0 {
		burstMult = defaultBurstMultiplier
	}
	burst := int(bps) * burstMult
	return &parent{
		burst: burst,
		lm:    rate.NewLimiter(rate.Limit(bps), burst),
	}
}

func (p *parent) newThrottleConn(c net.Conn) *throttleConn {
	ctx, cancel := context.WithCancel(context.Background())
	return &throttleConn{
		Conn:  c,
		burst: p.burst,
		lm:    p.lm,
		cncl:  cancel,
		ctx:   ctx,
	}
}

func newWriteThrottler(bps int64, burstMult int, c net.Conn) (wt *throttleConn) {
	if burstMult <= 0 {
		burstMult = defaultBurstMultiplier
	}
	burst := int(bps) * burstMult
	return &throttleConn{
		Conn:  c,
		burst: burst,
		lm:    rate.NewLimiter(rate.Limit(bps), burst),
	}
}

func (w *throttleConn) Close() error {
	if w.cncl != nil {
		w.cncl()
	}
	return w.Conn.Close()
}

func (w *throttleConn) SetReadTimeout(to time.Duration) error {
	return w.Conn.SetReadDeadline(time.Now().Add(to))
}

func (w *throttleConn) ClearReadTimeout() error {
	return w.Conn.SetReadDeadline(time.Time{})
}

func (w *throttleConn) SetWriteTimeout(to time.Duration) error {
	w.to = to
	return w.Conn.SetWriteDeadline(time.Now().Add(to))
}

func (w *throttleConn) ClearWriteTimeout() error {
	w.to = 0
	return w.Conn.SetWriteDeadline(time.Time{})
}

func (w *throttleConn) Write(b []byte) (n int, err error) {
	var r int
	ctx := w.ctx
	if w.to > 0 {
		var cancel func()
		ctx, cancel = context.WithTimeout(w.ctx, w.to)
		defer cancel()
	}
	for n < len(b) {
		sz := len(b) - n
		if sz > w.burst {
			sz = w.burst
		}
		if r, err = w.Conn.Write(b[n : n+sz]); err != nil {
			return
		}
		if err = w.lm.WaitN(ctx, r); err != nil {
			return
		}
		n += r
	}
	return
}

type fullSpeed struct {
	net.Conn
}

func (fs fullSpeed) SetReadTimeout(to time.Duration) error {
	return fs.Conn.SetReadDeadline(time.Now().Add(to))
}

func (fs fullSpeed) ClearReadTimeout() error {
	return fs.Conn.SetReadDeadline(time.Time{})
}

func (fs fullSpeed) SetWriteTimeout(to time.Duration) error {
	return fs.Conn.SetWriteDeadline(time.Now().Add(to))
}

func (fs fullSpeed) ClearWriteTimeout() error {
	return fs.Conn.SetWriteDeadline(time.Time{})
}

func newUnthrottledConn(c net.Conn) fullSpeed {
	return fullSpeed{
		Conn: c,
	}
}
