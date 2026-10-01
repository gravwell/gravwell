package microsoft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gravwell/gravwell/v3/hosted"
	"github.com/gravwell/gravwell/v3/hosted/storage"
)

// Cap in-cycle waiting, never the vendor's retry eligibility. Longer hints
// defer that origin without blocking unrelated origins or losing the deadline.
const maximumRetryWait = 10 * time.Minute

// Retry eligibility belongs to the job and origin, not its payload routing mode.
const retryStateKey = "microsoft/retry-origins-v1"
const normalizedRetryStateKey = "microsoft-normalized-v1/retry-origins-v1"

type retryEligibility struct {
	Until   time.Time `json:"until,omitempty"`
	Blocked bool      `json:"blocked,omitempty"`
}

type retryWaitDeadlineKey struct{}

func withRetryWaitDeadline(ctx context.Context) context.Context {
	if _, ok := ctx.Value(retryWaitDeadlineKey{}).(time.Time); ok {
		return ctx
	}
	return context.WithValue(ctx, retryWaitDeadlineKey{}, time.Now().Add(maximumRetryWait))
}

func retryEligibilityFor(attempt int, header string, now time.Time) retryEligibility {
	raw := strings.TrimSpace(header)
	if raw != "" && strings.Trim(raw, "0123456789") == "" {
		seconds, err := strconv.ParseUint(raw, 10, 64)
		// Keep deadlines representable by the durable JSON timestamp contract.
		last := time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
		if err != nil || seconds > uint64(last.Unix()-now.Unix()) {
			return retryEligibility{Blocked: true}
		}
		return retryEligibility{Until: time.Unix(now.Unix()+int64(seconds), int64(now.Nanosecond())).UTC()}
	}
	if until, err := http.ParseTime(raw); err == nil {
		if until.Before(now) {
			until = now
		}
		return retryEligibility{Until: until.UTC()}
	}
	return retryEligibility{Until: now.Add(retryDelay(attempt, ""))}
}

func retryOrigin(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return "", errors.New("invalid Microsoft request origin")
	}
	return strings.ToLower(u.Scheme + "://" + u.Host), nil
}

type retryStateError struct{ err error }

func (e *retryStateError) Error() string { return "Microsoft retry eligibility storage failed" }
func (e *retryStateError) Unwrap() error { return e.err }

// This state is separate from source delivery progress and shared by all
// selectors of this configured job. Bind on every Handle so recreated clients
// retain actual origin eligibility. No token or response body is stored.
func (c *Client) bindRetryState(rt hosted.Storage, syncState func() error) error {
	loaded := map[string]retryEligibility{}
	mirrorNormalizedState := false
	for _, key := range []string{retryStateKey, normalizedRetryStateKey} {
		saved, err := rt.Get(key)
		if errors.Is(err, storage.ErrStorageNotFound) {
			continue
		}
		if err != nil {
			return &retryStateError{err}
		}
		var values map[string]retryEligibility
		if err := json.Unmarshal(saved, &values); err != nil {
			return &retryStateError{err}
		}
		for origin, eligibility := range values {
			prior := loaded[origin]
			if eligibility.Blocked || (!prior.Blocked && eligibility.Until.After(prior.Until)) {
				loaded[origin] = eligibility
			}
		}
		mirrorNormalizedState = mirrorNormalizedState || key == normalizedRetryStateKey
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for origin, eligibility := range loaded {
		prior := c.cooldowns[origin]
		if eligibility.Blocked || (!prior.Blocked && eligibility.Until.After(prior.Until)) {
			c.cooldowns[origin] = eligibility
		}
	}
	c.persistCooldowns = func(values map[string]retryEligibility) error {
		b, err := json.Marshal(values)
		if err != nil {
			return &retryStateError{err}
		}
		if err := rt.Put(retryStateKey, b); err != nil {
			return &retryStateError{err}
		}
		if syncState != nil {
			if err := syncState(); err != nil {
				return &retryStateError{err}
			}
		}
		return nil
	}
	// Retain both routing-mode keys; never clear source histories.
	if mirrorNormalizedState {
		return c.persistCooldowns(c.cooldowns)
	}
	return nil
}

func (c *Client) retainCooldown(endpoint string, eligibility retryEligibility) error {
	origin, err := retryOrigin(endpoint)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	prior := c.cooldowns[origin]
	if eligibility.Blocked || (!prior.Blocked && eligibility.Until.After(prior.Until)) {
		c.cooldowns[origin] = eligibility
	}
	if c.persistCooldowns != nil {
		return c.persistCooldowns(c.cooldowns)
	}
	return nil
}

func (c *Client) waitCooldown(ctx context.Context, endpoint string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	origin, err := retryOrigin(endpoint)
	if err != nil {
		return err
	}
	c.mu.Lock()
	eligibility := c.cooldowns[origin]
	c.mu.Unlock()
	if eligibility.Blocked {
		return errors.New("Microsoft retry origin deferred: Retry-After exceeds representable eligibility; operator correction of this origin's retry state is required")
	}
	now := time.Now()
	if !eligibility.Until.After(now) {
		return nil
	}
	deadline := now.Add(maximumRetryWait)
	if bound, ok := ctx.Value(retryWaitDeadlineKey{}).(time.Time); ok {
		deadline = bound
	}
	if eligibility.Until.After(deadline) {
		return fmt.Errorf("Microsoft retry origin deferred until %s; in-cycle wait budget exceeded", eligibility.Until.Format(time.RFC3339Nano))
	}
	return sleepContext(ctx, time.Until(eligibility.Until))
}
