/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package dynamic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"time"
	"uuid"

	"github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/ingest/config/dynamic/rpc"
	"github.com/gravwell/gravwell/v4/ingest/log"
)

const (
	// backoffBase is the first wait after a failed connection, and the step the backoff
	// doubles from.
	backoffBase = time.Second

	// backoffMax caps the wait between attempts.  A webserver that has been down for an
	// hour should still be picked up within a minute of coming back.
	backoffMax = time.Minute

	// callTimeout bounds a single RPC.  A wedged server must not stall the loop forever.
	callTimeout = 30 * time.Second

	// sessionDrainTimeout is how long a closed session is given to finish the work it
	// still had running before it is abandoned.  It bounds how long Close can take.
	sessionDrainTimeout = 10 * time.Second
)

// backoff produces the wait after n consecutive failures, capped and jittered.
//
// The jitter matters more than the cap: without it every ingester that lost the same
// webserver reconnects in lockstep and hammers it the moment it comes back.  Full jitter
// over the interval is the standard answer and costs nothing.
func backoff(n int) time.Duration {
	d := backoffBase << min(n, 16) // 16 doublings is already far past the cap
	if d > backoffMax || d <= 0 {  // <= 0 catches the shift overflowing
		d = backoffMax
	}
	// full jitter, anywhere in (0, d].  Int63n draws from [0, d), so the millisecond that
	// keeps the wait off zero can push the result just past d: clamped rather than left
	// to exceed the cap the line above went to the trouble of applying.
	return min(time.Duration(rand.Int63n(int64(d)))+time.Millisecond, d)
}

// start launches the background client.  It returns immediately, the loop owns its own
// lifetime and stops when the manager's context is cancelled.
func (dcm *DynamicConfigManager) start() {
	dcm.wg.Go(dcm.run)
}

// run keeps a session to a webserver up, reconnecting with backoff, for as long as the
// manager lives.
func (dcm *DynamicConfigManager) run() {
	var fails int
	for {
		if dcm.ctx.Err() != nil {
			return
		}
		// rotate through the configured webservers so that a dead one does not pin us
		raw := dcm.Webserver[fails%len(dcm.Webserver)]
		endpoint, err := url.Parse(raw)
		if err != nil {
			fails++
			dcm.lgr.Error("dynamic config webserver endpoint is unusable",
				log.KV("webserver", raw), log.KVErr(err))
			if !dcm.sleep(backoff(fails)) {
				return
			}
			continue
		}
		// the route is part of the protocol rather than a deployment detail, so it comes
		// from the one place that defines it and is not restated here: a second copy of a
		// path both ends have to agree on is a path they can end up disagreeing about
		endpoint.Path = client.INGESTERS_CONTROL_URL

		sess, err := dcm.dial(endpoint.String())
		if err != nil {
			fails++
			wait := backoff(fails)
			dcm.lgr.Warn("dynamic config connection failed, backing off",
				log.KV("webserver", endpoint.String()), log.KV("attempt", fails),
				log.KV("retry-in", wait), log.KVErr(err))
			if !dcm.sleep(wait) {
				return
			}
			continue
		}
		dcm.lgr.Info("dynamic config connected", log.KV("webserver", endpoint.String()))
		fails = 0 // a successful connection resets the backoff

		if err = dcm.session(sess); err != nil && dcm.ctx.Err() == nil {
			dcm.lgr.Warn("dynamic config session ended", log.KV("webserver", endpoint.String()), log.KVErr(err))
		}
		sess.Close()
		// and wait for it to actually be finished.  A pushed configuration is handled on
		// the session's own goroutine, so without this the manager can be shut down and
		// report itself stopped while a handler is still writing config files.
		//
		// Bounded, and deliberately not on the manager's context: by the time Close is
		// what brought us here that context is already cancelled, so deriving from it
		// would wait for nothing.  A handler wedged on a storage mount that has gone away
		// never returns, and waiting on it forever would mean the ingester could not be
		// shut down at all.
		dcm.drainSession(sess)

		// a session that came up and then dropped still backs off, otherwise a server
		// that accepts and immediately hangs up becomes a tight reconnect loop
		fails++
		if !dcm.sleep(backoff(fails)) {
			return
		}
	}
}

// drainSession waits for a closed session to finish whatever it was still running, up to
// sessionDrainTimeout.  A session that will not drain is abandoned rather than waited on,
// and said so out loud: the work it is stuck in is still holding whatever it held.
func (dcm *DynamicConfigManager) drainSession(sess *rpc.Session) {
	ctx, cf := context.WithTimeout(context.Background(), sessionDrainTimeout)
	defer cf()
	if err := sess.WaitContext(ctx); err != nil {
		dcm.lgr.Warn("dynamic config session did not finish, abandoning it",
			log.KV("waited", sessionDrainTimeout), log.KVErr(err))
	}
}

// sleep waits, and reports false if the manager was shut down instead.
func (dcm *DynamicConfigManager) sleep(d time.Duration) bool {
	tmr := time.NewTimer(d)
	defer tmr.Stop()
	select {
	case <-dcm.ctx.Done():
		return false
	case <-tmr.C:
		return true
	}
}

// dial brings up an authenticated session to one webserver.
func (dcm *DynamicConfigManager) dial(endpoint string) (*rpc.Session, error) {
	ctx, cf := context.WithTimeout(dcm.ctx, callTimeout)
	defer cf()
	return rpc.Dial(ctx, rpc.ClientConfig{
		Webserver: endpoint,
		Path:      client.INGESTERS_CONTROL_URL,
		Token:     dcm.Auth_Token,
		Class:     dcm.Class,
		ID:        dcm.guid,
		Logger:    dcm.lgr,
		Handlers:  dcm.handlers,
	})
}

// session runs one connection: declare what we can run, then poll for what we should be
// running until the session drops or we are shut down.
func (dcm *DynamicConfigManager) session(sess *rpc.Session) (err error) {
	if err = dcm.sendRegistrations(sess); err != nil {
		return fmt.Errorf("failed to register kinds %w", err)
	}

	tckr := time.NewTicker(dcm.pollInterval)
	defer tckr.Stop()
	for {
		if err = dcm.refresh(sess); err != nil {
			return
		}
		select {
		case <-dcm.ctx.Done():
			return dcm.ctx.Err()
		case <-sess.Done():
			return sess.Err()
		case <-dcm.nudge:
			// something changed locally, usually a newly registered kind, so redeclare
			// before asking again
			if err = dcm.sendRegistrations(sess); err != nil {
				return fmt.Errorf("failed to register kinds %w", err)
			}
		case <-tckr.C:
		}
	}
}

// sendRegistrations declares everything this ingester can run.  The full set goes every
// time, so the server replaces rather than merges and a kind that is gone really goes.
func (dcm *DynamicConfigManager) sendRegistrations(sess *rpc.Session) (err error) {
	req := RegisterKindsRequest{
		ID:    dcm.guid,
		Class: dcm.Class,
		Kinds: dcm.Kinds(),
	}
	ctx, cf := context.WithTimeout(dcm.ctx, callTimeout)
	defer cf()
	if err = sess.Call(ctx, MethodRegisterKinds, req, nil); err != nil {
		return
	}
	dcm.lgr.Info("dynamic config registered kinds", log.KV("count", len(req.Kinds)))
	return
}

// refresh asks the server what we should be running and applies the answer.
func (dcm *DynamicConfigManager) refresh(sess *rpc.Session) (err error) {
	q := RunnerQuery{
		ID:    dcm.guid,
		Class: dcm.Class,
		Kinds: dcm.KindNames(),
	}
	if len(q.Kinds) == 0 {
		// nothing has been registered yet, asking would only ever return nothing and a
		// server is entitled to treat an empty kind list as a mistake
		return nil
	}
	var set RunnerSet
	ctx, cf := context.WithTimeout(dcm.ctx, callTimeout)
	defer cf()
	if err = sess.Call(ctx, MethodListRunners, q, &set); err != nil {
		return
	}
	if err = dcm.Sync(set.Runners); err != nil {
		return
	}
	return dcm.reportStatus(sess)
}

// reportStatus tells the webserver what became of the configurations it handed us.
//
// The complete set goes every time, which is what makes a runner that has come good clear
// itself: it is simply reported without an error rather than needing a retraction that
// something would have to remember to send.  It also goes on every poll rather than only
// when something changed, so that a server can tell "this ingester says it is fine" from
// "this ingester has not spoken in an hour".
//
// A server that refuses the method, or does not have it, is not worth dropping a session
// over.  Status is what the ingester reports about its work, not the work itself, so a
// server that will not listen costs us nothing but the report.  A dead connection is a
// different matter and is handed back to the session loop.
func (dcm *DynamicConfigManager) reportStatus(sess *rpc.Session) (err error) {
	req := ReportStatusRequest{
		ID:       dcm.guid,
		Class:    dcm.Class,
		Statuses: dcm.Statuses(),
	}
	ctx, cf := context.WithTimeout(dcm.ctx, callTimeout)
	defer cf()
	if err = sess.Call(ctx, MethodReportStatus, req, nil); err != nil {
		if _, ok := errors.AsType[rpc.RemoteError](err); ok {
			dcm.lgr.Warn("dynamic config status report refused", log.KVErr(err))
			return nil
		}
		return
	}
	return
}

// Sync reconciles the remote set of configured runners with what is on disk.
//
// The comparison is done on the rendered INI rather than on the definitions themselves.
// That is deliberate: a definition makes a round trip through JSON on the way here, so an
// int arrives as a float and a []string as a []any, and comparing the decoded values
// would report a change on every poll.  The INI is what actually lands on disk, so
// comparing it answers the only question that matters, would the file be different.
//
// Runners this ingester registered itself are left alone.  The server does not know about
// them, and letting its answer delete them would take out the ingester's own static
// configuration.
//
// Every definition is checked before anything is written.  One that this ingester cannot
// run is neither written nor deleted: whatever was already on disk keeps running and the
// reason is recorded against that runner for the next status report.  That is deliberate.
// An operator who saves a typo should get told about the typo, not have the working
// configuration it replaced pulled out from under a running ingester.
//
// The error return is reserved rather than used: everything that can currently go wrong
// with one runner, up to and including failing to write its file, is recorded against
// that runner and reported instead, because a whole sync abandoned halfway is a worse
// answer than a directory that is right apart from the one thing that could not be
// written.  Callers still have to handle an error, a future failure that really is
// wholesale has somewhere to go.
func (dcm *DynamicConfigManager) Sync(remote []RunnerDefinition) error {
	return dcm.sync(remote, true)
}

// sync is Sync with a say over the orphan sweep.  A poll carries the server's complete
// set, so anything of the server's left on disk that it did not name is stale and the
// sweep is right to remove it.  A push carries one definition folded into whatever this
// process has seen so far, and before the first poll that is nothing at all: sweeping on
// that would delete every other configuration on disk and stop the runners behind them,
// only for the next poll to put them straight back.  The push leaves the sweep to the poll.
func (dcm *DynamicConfigManager) sync(remote []RunnerDefinition, sweep bool) error {
	dcm.mtx.Lock()
	defer dcm.mtx.Unlock()

	// render and check everything the server sent before touching the directory
	type incoming struct {
		rd  RunnerDefinition
		ini string
	}
	wanted := make(map[uuid.UUID]incoming, len(remote))
	// rejected holds the ones we cannot run, and why.  It is kept apart from wanted for
	// two reasons: nothing in it is ever written, and the reconcile loop below has to be
	// able to tell "the server sent something broken" from "the server deleted this",
	// which look identical if all you know is that it is not in wanted.
	rejected := make(map[uuid.UUID]RunnerStatus)

	for _, rd := range remote {
		if rd.UUID == uuid.Nil() {
			// a status is keyed by UUID, so one without a UUID cannot even be reported
			// back, which leaves a log line as the only place to say anything
			dcm.lgr.Warn("dynamic config skipping a runner with no UUID",
				log.KV("kind", rd.Kind), log.KV("name", rd.Name))
			continue
		}
		blob, rerr := rd.INI()
		if rerr == nil {
			// the definition renders, now find out whether the plugin will have it
			rerr = dcm.validateLocked(rd)
		}
		if rerr != nil {
			dcm.lgr.Error("dynamic config rejected a runner",
				log.KV("kind", rd.Kind), log.KV("name", rd.Name),
				log.KV("uuid", rd.UUID), log.KVErr(rerr))
			rejected[rd.UUID] = runnerStatus(rd, rerr)
			continue
		}
		wanted[rd.UUID] = incoming{rd: rd, ini: blob}
	}

	var added, updated, removed int
	kept := make([]configuredRunner, 0, len(dcm.Configured))

	for _, cur := range dcm.Configured {
		if !cur.remote {
			kept = append(kept, cur) // ours, not the server's to take away
			continue
		}
		if _, bad := rejected[cur.UUID]; bad {
			// the server still has this runner, it just sent a version we cannot run.
			// Hold on to the copy that works, the objection goes back up as a status.
			kept = append(kept, cur)
			continue
		}
		want, still := wanted[cur.UUID]
		if !still {
			// deleted upstream
			if cur.backingFile != `` {
				if rerr := os.Remove(cur.backingFile); rerr != nil && !os.IsNotExist(rerr) {
					dcm.lgr.Error("dynamic config failed to remove a config",
						log.KV("file", cur.backingFile), log.KVErr(rerr))
				}
			}
			removed++
			continue
		}
		delete(wanted, cur.UUID)

		existing, rerr := os.ReadFile(cur.backingFile)
		if rerr == nil && string(existing) == markRemote(want.ini) {
			kept = append(kept, cur) // unchanged, leave the file alone
			continue
		}
		pth := dcm.runnerPath(want.rd)
		if werr := writeConfFile(pth, markRemote(want.ini)); werr != nil {
			// a failure here is ours rather than the configuration's, but it is still the
			// answer to "why is this runner not what I asked for", so it is reported the
			// same way.  Carrying on means one unwritable file does not strand every
			// other change in this sync half applied.
			dcm.lgr.Error("dynamic config failed to write a config",
				log.KV("file", pth), log.KVErr(werr))
			rejected[cur.UUID] = runnerStatus(want.rd, fmt.Errorf("failed to write %s %w", pth, werr))
			kept = append(kept, cur)
			continue
		}
		// the name may have changed, which changes the file name, so drop the old one
		if cur.backingFile != `` && cur.backingFile != pth {
			os.Remove(cur.backingFile)
		}
		kept = append(kept, configuredRunner{RunnerDefinition: want.rd, backingFile: pth, remote: true})
		updated++
	}

	// whatever is left in wanted is new to this process.  It is not necessarily new to
	// the disk: the list starts empty every time the process does, so on a restart every
	// one of the server's configurations lands here with its file already in place.  A
	// file that already holds exactly this is kept as it stands rather than rewritten,
	// otherwise every restart rewrote every file and reloaded every runner for nothing.
	for _, want := range wanted {
		pth := dcm.runnerPath(want.rd)
		if existing, rerr := os.ReadFile(pth); rerr == nil && string(existing) == markRemote(want.ini) {
			kept = append(kept, configuredRunner{RunnerDefinition: want.rd, backingFile: pth, remote: true})
			continue
		}
		if werr := writeConfFile(pth, markRemote(want.ini)); werr != nil {
			dcm.lgr.Error("dynamic config failed to write a config",
				log.KV("file", pth), log.KVErr(werr))
			rejected[want.rd.UUID] = runnerStatus(want.rd, fmt.Errorf("failed to write %s %w", pth, werr))
			continue
		}
		kept = append(kept, configuredRunner{RunnerDefinition: want.rd, backingFile: pth, remote: true})
		added++
	}

	// anything of the server's still on disk that this reconcile did not account for is
	// left over from a previous run: a configuration deleted or renamed while this
	// ingester was down, which nothing in memory remembers because the list starts empty
	// every time the process does.  Without this sweep those files are loaded forever.
	if sweep {
		removed += dcm.sweepOrphans(kept, rejected)
	}

	// what the sync itself concluded.  Load failures are laid over the top when the set is
	// read rather than baked in here, see Statuses: merging at the point of use is what
	// lets a repaired configuration stop being reported without anything having to go
	// back and unpick an entry that was written into this slice earlier.
	dcm.statuses = buildStatuses(kept, rejected)

	// kept is the truth about the directory whether or not anything moved.  It has to be
	// recorded even on a quiet sync, because on a restart the first sync finds every file
	// already in place and nothing moves, and a list left empty then would mean the next
	// push knew about none of them.
	dcm.Configured = kept
	if added == 0 && updated == 0 && removed == 0 {
		return nil // nothing moved, do not wake the ingester
	}
	dcm.lgr.Info("dynamic config changed", log.KV("added", added),
		log.KV("updated", updated), log.KV("removed", removed))
	dcm.bump()
	return nil
}

// runnerPath is where a runner's config lives, kind_name_uuid.conf in the storage
// directory.  It is derived rather than remembered so that the same runner always lands
// in the same place.
func (dcm *DynamicConfigManager) runnerPath(rd RunnerDefinition) string {
	return filepath.Join(dcm.Storage,
		fmt.Sprintf("%s_%s_%v.conf", fnameChunk(rd.Kind), fnameChunk(rd.Name), rd.UUID))
}

// remoteMarker is written as the first line of every configuration this ingester takes
// from a webserver.
//
// It is what makes a file on disk answerable for itself.  Nothing else distinguishes a
// configuration the server sent from one the ingester registered for itself: both are
// written by the same code, into the same directory, under the same naming.  Without a
// marker, reconciling the directory against what the server currently has would either
// leave the server's deleted configurations behind forever or delete the ingester's own,
// and there is no way to tell which from the file name alone.
//
// It is a comment, so gcfg reads straight past it.
//
// gcfg opens a comment on either a hash or a semicolon, and every config this product
// ships uses a hash.  The semicolon form is what this used to write, so it is still
// recognized on the way in: the marker is what says a file is the server's to delete, and
// a file that stopped answering that question would be swept by nothing and loaded
// forever.  One is rewritten into the other the next time the server sends its config.
const (
	remoteMarkerText   = `gravwell dynamic configuration, managed by the webserver`
	remoteMarker       = `# ` + remoteMarkerText
	legacyRemoteMarker = `; ` + remoteMarkerText // written by ingesters before the hash
)

// markRemote prefixes an INI block with the marker.
func markRemote(ini string) string {
	return remoteMarker + "\n" + ini
}

// isRemoteConfig reports whether a file on disk is one the webserver owns.
//
// Only the first line is read.  A file that is unreadable, or that does not carry the
// marker, is treated as not ours to delete, which is the safe direction to be wrong in:
// the cost of keeping a file that should have gone is a stale configuration an operator
// can see and remove, and the cost of deleting one that should have stayed is somebody
// else's configuration disappearing.
func isRemoteConfig(pth string) bool {
	f, err := os.Open(pth)
	if err != nil {
		return false
	}
	defer f.Close()
	// the two markers differ only in their opening character, so one read covers both
	buf := make([]byte, len(remoteMarker))
	n, err := io.ReadFull(f, buf)
	if err != nil && n < len(remoteMarker) {
		return false
	}
	line := string(buf[:n])
	return line == remoteMarker || line == legacyRemoteMarker
}

// sweepOrphans deletes the webserver's configurations that this reconcile did not keep.
//
// Only files carrying the marker are candidates, so a configuration the ingester
// registered for itself is never touched no matter what the server says.  A file the
// reconcile did account for is kept whether or not it was rewritten, including one held
// back because the server sent a version that would not run: that file is the last thing
// that worked, and deleting it is exactly what this ingester takes pains not to do.
//
// The caller holds the lock.
func (dcm *DynamicConfigManager) sweepOrphans(kept []configuredRunner, rejected map[uuid.UUID]RunnerStatus) (removed int) {
	files, err := overlayFiles(dcm.Storage)
	if err != nil {
		dcm.lgr.Error("dynamic config could not scan storage for stale configurations",
			log.KV("storage", dcm.Storage), log.KVErr(err))
		return
	}
	accounted := make(map[string]bool, len(kept))
	for _, cur := range kept {
		if cur.backingFile != `` {
			accounted[cur.backingFile] = true
		}
	}
	for _, pth := range files {
		if accounted[pth] || !isRemoteConfig(pth) {
			continue
		}
		// a file whose definition the server still has but which we rejected is not
		// stale, it is the copy being held on to
		if id, _, ok := parseRunnerFile(pth); ok {
			if _, bad := rejected[id]; bad {
				continue
			}
		}
		if rerr := os.Remove(pth); rerr != nil && !os.IsNotExist(rerr) {
			dcm.lgr.Error("dynamic config failed to remove a stale configuration",
				log.KV("file", pth), log.KVErr(rerr))
			continue
		}
		dcm.lgr.Info("dynamic config removed a stale configuration", log.KV("file", pth))
		removed++
	}
	return
}

// runnerStatus builds the report for a definition this ingester could not take.
func runnerStatus(rd RunnerDefinition, err error) RunnerStatus {
	rs := RunnerStatus{UUID: rd.UUID, Kind: rd.Kind, Name: rd.Name}
	if err != nil {
		rs.Error = err.Error()
	}
	return rs
}

// buildStatuses turns the outcome of a sync into the set reported to the webserver.
//
// Everything the server has a stake in appears exactly once: a runner we rejected carries
// its reason, a runner we are carrying reports clean, and a runner this ingester
// registered for itself appears not at all, because the server never handed it to us and
// has no business forming an opinion about it.  A rejected runner may also still be in
// kept, holding the last configuration that worked, so rejections are laid down first and
// win.
//
// The result is ordered by UUID so that two consecutive reports of the same state are
// byte for byte the same report.
func buildStatuses(kept []configuredRunner, rejected map[uuid.UUID]RunnerStatus) (r []RunnerStatus) {
	r = make([]RunnerStatus, 0, len(kept)+len(rejected))
	for _, rs := range rejected {
		r = append(r, rs)
	}
	for _, cur := range kept {
		if !cur.remote {
			continue
		}
		if _, bad := rejected[cur.UUID]; bad {
			continue
		}
		r = append(r, RunnerStatus{UUID: cur.UUID, Kind: cur.Kind, Name: cur.Name})
	}
	sort.Slice(r, func(i, j int) bool {
		return bytes.Compare(r[i].UUID[:], r[j].UUID[:]) < 0
	})
	return
}

// mergeLoadErrors lays load failures over a set of statuses.
//
// A load failure wins over whatever the sync concluded.  The sync decides whether a
// definition is one this ingester would run and whether the file got written; the load
// finds out whether that file comes back as a working configuration.  A runner can pass
// the first and fail the second, and reporting it clean on the strength of the first
// would be saying it is running when it is not.
//
// A failure for a runner the sync never mentioned is carried through rather than dropped.
// That is the case that matters most at startup: nothing has synced yet, so the only
// thing known about a configuration on disk is that it will not load.
func mergeLoadErrors(statuses []RunnerStatus, loadErrors map[uuid.UUID]RunnerStatus) []RunnerStatus {
	if len(loadErrors) == 0 {
		return statuses
	}
	out := make([]RunnerStatus, 0, len(statuses)+len(loadErrors))
	seen := make(map[uuid.UUID]bool, len(loadErrors))
	for _, rs := range statuses {
		if le, bad := loadErrors[rs.UUID]; bad {
			// keep the kind and name the sync knew, the file name they were parsed out of
			// has been through fnameChunk and may not be what an operator typed
			if le.Kind == `` {
				le.Kind = rs.Kind
			}
			if le.Name == `` {
				le.Name = rs.Name
			}
			out = append(out, le)
			seen[rs.UUID] = true
			continue
		}
		out = append(out, rs)
	}
	for id, le := range loadErrors {
		if !seen[id] {
			out = append(out, le)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return bytes.Compare(out[i].UUID[:], out[j].UUID[:]) < 0
	})
	return out
}

// Statuses is the latest verdict on every configuration the webserver has handed us, as
// of the last sync.  The slice is a copy, a caller may hold it.
func (dcm *DynamicConfigManager) Statuses() (r []RunnerStatus) {
	dcm.mtx.Lock()
	defer dcm.mtx.Unlock()
	return dcm.statusesLocked()
}

// statusesLocked is the reported set: what the last sync concluded, with whatever would
// not load off disk laid over the top.  The caller holds the lock.
func (dcm *DynamicConfigManager) statusesLocked() []RunnerStatus {
	merged := mergeLoadErrors(dcm.statuses, dcm.loadErrors)
	r := make([]RunnerStatus, len(merged))
	copy(r, merged)
	return r
}

// statusFor is the verdict on one configuration, if we have formed one.
func (dcm *DynamicConfigManager) statusFor(id uuid.UUID) (rs RunnerStatus, ok bool) {
	dcm.mtx.Lock()
	defer dcm.mtx.Unlock()
	for _, cur := range dcm.statusesLocked() {
		if cur.UUID == id {
			return cur, true
		}
	}
	return
}

// bump signals the main loop that the configuration on disk has changed.
//
// The channel is buffered by one and the send is non blocking, so a burst of changes
// collapses into a single reload rather than queueing one per change, and a caller that
// is not listening never blocks the sync.
func (dcm *DynamicConfigManager) bump() {
	select {
	case dcm.ch <- struct{}{}:
	default:
	}
}

// applyConfig is what a webserver calls to push a single configuration without waiting
// for the next poll.  It is a hint, not a replacement for the poll: the runner is merged
// into the current set and the full reconciliation still happens on the next refresh.
//
// The answer is the verdict on this particular configuration, so a push that cannot be
// run is refused where the operator is standing rather than only turning up in a status
// list they have to go and look at.  The full status set is reported first either way, so
// the push and the next poll cannot disagree about what this ingester thinks.
func (dcm *DynamicConfigManager) applyConfig(ctx context.Context, params json.RawMessage) (any, error) {
	var rd RunnerDefinition
	if err := json.Unmarshal(params, &rd); err != nil {
		return nil, fmt.Errorf("bad configuration %w", err)
	}
	if rd.UUID == uuid.Nil() {
		return nil, errors.New("configuration has no UUID")
	}
	// fold it into the set we already hold so that a push cannot delete anything, only
	// add or change
	current := dcm.remoteRunners()
	replaced := false
	for i := range current {
		if current[i].UUID == rd.UUID {
			current[i] = rd
			replaced = true
			break
		}
	}
	if !replaced {
		current = append(current, rd)
	}
	// no orphan sweep here, see sync: this set is only what we have seen so far, not
	// the server's complete set
	if err := dcm.sync(current, false); err != nil {
		return nil, err
	}
	if sess, ok := rpc.SessionFrom(ctx); ok {
		if err := dcm.reportStatus(sess); err != nil {
			dcm.lgr.Warn("dynamic config failed to report status after a push", log.KVErr(err))
		}
	}
	if st, ok := dcm.statusFor(rd.UUID); ok && !st.OK() {
		return nil, errors.New(st.Error)
	}
	return map[string]any{`ok`: true}, nil
}

// remoteRunners copies the definitions we currently hold from the server.
func (dcm *DynamicConfigManager) remoteRunners() (r []RunnerDefinition) {
	dcm.mtx.Lock()
	defer dcm.mtx.Unlock()
	for _, cur := range dcm.Configured {
		if cur.remote {
			r = append(r, cur.RunnerDefinition)
		}
	}
	return
}
