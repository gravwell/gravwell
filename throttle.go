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

type Parent struct {
	burst int
	lm    *rate.Limiter
}

type ThrottleConn struct {
	net.Conn
	burst int
	lm    *rate.Limiter
	to    time.Duration
	ctx   context.Context
	cncl  func()
}

type Conn interface {
	net.Conn
	SetReadTimeout(time.Duration) error
	SetWriteTimeout(time.Duration) error
	ClearWriteTimeout() error
	ClearReadTimeout() error
}

func NewParent(bps int64, burstMult int) *Parent {
	if burstMult <= 0 {
		burstMult = defaultBurstMultiplier
	}
	burst := int(bps) * burstMult
	return &Parent{
		burst: burst,
		lm:    rate.NewLimiter(rate.Limit(bps), burst),
	}
}

func (p *Parent) NewThrottleConn(c net.Conn) *ThrottleConn {
	ctx, cancel := context.WithCancel(context.Background())
	return &ThrottleConn{
		Conn:  c,
		burst: p.burst,
		lm:    p.lm,
		cncl:  cancel,
		ctx:   ctx,
	}
}

func NewWriteThrottler(bps int64, burstMult int, c net.Conn) (wt *ThrottleConn) {
	if burstMult <= 0 {
		burstMult = defaultBurstMultiplier
	}
	burst := int(bps) * burstMult
	return &ThrottleConn{
		Conn:  c,
		burst: burst,
		lm:    rate.NewLimiter(rate.Limit(bps), burst),
	}
}

func (w *ThrottleConn) Close() error {
	if w.cncl != nil {
		w.cncl()
	}
	return w.Conn.Close()
}

func (w *ThrottleConn) SetReadTimeout(to time.Duration) error {
	return w.Conn.SetReadDeadline(time.Now().Add(to))
}

func (w *ThrottleConn) ClearReadTimeout() error {
	return w.Conn.SetReadDeadline(time.Time{})
}

func (w *ThrottleConn) SetWriteTimeout(to time.Duration) error {
	w.to = to
	return w.Conn.SetWriteDeadline(time.Now().Add(to))
}

func (w *ThrottleConn) ClearWriteTimeout() error {
	w.to = 0
	return w.Conn.SetWriteDeadline(time.Time{})
}

func (w *ThrottleConn) Write(b []byte) (n int, err error) {
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

func NewUnthrottledConn(c net.Conn) fullSpeed {
	return fullSpeed{
		Conn: c,
	}
}
