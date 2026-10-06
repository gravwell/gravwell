/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package dynamic

import (
	"slices"
	"uuid"
)

// The methods spoken between an ingester and a webserver over an authenticated RPC
// session.  They live here rather than in the rpc package because rpc moves bytes and
// knows nothing about configuration, and they live here rather than in either endpoint
// because both ends have to agree on them.

const (
	// MethodRegisterKinds is called by an ingester once per connection to declare
	// everything it is able to run.  It sends the complete set, so the server replaces
	// whatever it held for that ingester rather than merging, which is what lets a kind
	// that has been removed from a build actually disappear.
	MethodRegisterKinds = `registerKinds`

	// MethodListRunners is called by an ingester to ask which configured runners it
	// should be running.
	MethodListRunners = `listRunners`

	// MethodApplyConfig is called by a webserver down to an ingester to push a single
	// configuration without waiting for the next poll.  An ingester that does not
	// implement it simply gets its changes on the next poll instead.
	MethodApplyConfig = `applyConfig`

	// MethodReportStatus is called by an ingester to say what became of the
	// configurations it was handed.  Like MethodRegisterKinds it carries the complete
	// set, so the server replaces whatever it held for that ingester rather than merging.
	// That is what makes a runner that has come good actually clear: it reports with no
	// error rather than having to send a retraction nobody would remember to send.
	MethodReportStatus = `reportStatus`
)

// RunnerStatus is one ingester's verdict on one configuration.
//
// An empty Error means the ingester rendered the configuration, parsed it back with the
// same loader it uses at startup, and the plugin's own Verify accepted it.  Anything else
// is the reason it could not, in the plugin's words, because a config that is well formed
// on the wire can still be meaningless to the thing that has to run it: an Interval of
// "3" is a perfectly good string and not a duration.
//
// Kind and Name ride along so that a server can name a runner in its interface without
// having to still hold a definition for it.
type RunnerStatus struct {
	UUID  uuid.UUID
	Kind  string `json:",omitempty"`
	Name  string `json:",omitempty"`
	Error string `json:",omitempty"`
}

// OK reports whether the ingester accepted this configuration.
func (rs RunnerStatus) OK() bool { return rs.Error == `` }

// ReportStatusRequest is the complete set of verdicts from one ingester.
//
// It carries no timestamps on purpose.  The server stamps what it receives with its own
// clock, because a time from the ingester would be skewed by however wrong that host's
// clock is, and an operator comparing two ingesters needs one clock rather than two.
type ReportStatusRequest struct {
	ID       uuid.UUID
	Class    string         `json:",omitempty"`
	Statuses []RunnerStatus `json:",omitempty"`
}

// RegisterKindsRequest declares what an ingester can run.
//
// The identity is carried in the body as well as being bound into the session handshake.
// A server must trust the session, not the body: the handshake proves who the peer is and
// this is only a convenience for logging and for servers that route on class.
type RegisterKindsRequest struct {
	ID    uuid.UUID
	Class string             `json:",omitempty"`
	Kinds []RunnerDefinition `json:",omitempty"`
}

// RunnerQuery asks for the configurations an ingester should be running.
//
// The filters are how a webserver decides what belongs to whom: Kinds is what this build
// can actually run, and ID, Class and Groups are what an Assignment can point at.  A server
// returns a runner when its kind is supported here and its assignment names this ingester,
// names this class, names a group this ingester is in, or names nobody at all.
type RunnerQuery struct {
	ID    uuid.UUID
	Class string   `json:",omitempty"`
	Kinds []string `json:",omitempty"`

	// Groups is the groups this ingester belongs to.
	//
	// The server fills this in from its own membership data.  It is never read off the
	// wire, and an ingester does not get to say what groups it is in: a request body
	// claiming membership of a group would be handing out every configuration pinned to
	// that group to whoever asked for it.  ID and Class come from the authenticated
	// session for the same reason.
	//
	// It is a field on the query rather than an argument to Matches so that one rule
	// covers the poll and the push, which is the only way those two can agree.
	Groups []string `json:",omitempty"`
}

// Matches reports whether a runner should be handed to the ingester that sent this query.
// It lives here so that both ends agree on the rule rather than each inventing one.
//
// A runner has to be of a kind this ingester registered, and it has to pass every filter
// its assignment sets.  A list that is present excludes everything not in it, which is
// the point: a configuration pinned to three UUIDs must never reach a fourth ingester.
// A list that is absent is not a filter at all.
func (q RunnerQuery) Matches(rd RunnerDefinition) bool {
	if !q.supports(rd.Kind) {
		return false
	}
	if rd.Assigned.Empty() {
		return true // unassigned, anything that can run it may have it
	}
	if !rd.Assigned.AllowsUUID(q.ID) {
		return false
	}
	if !rd.Assigned.AllowsClass(q.Class) {
		return false
	}
	return q.inGroup(rd.Assigned.Group)
}

// inGroup reports whether an assignment's group, if it sets one, names a group this
// ingester is in.  An assignment that names no group is not filtered by group at all; one
// that does reaches only the members, so a runner pinned to a group an ingester is not in
// is handed to nobody rather than to everybody.
func (q RunnerQuery) inGroup(group string) bool {
	if group == `` {
		return true
	}
	return slices.Contains(q.Groups, group)
}

func (q RunnerQuery) supports(kind string) bool {
	return slices.Contains(q.Kinds, kind)
}

// RunnerSet is the answer to a RunnerQuery.
type RunnerSet struct {
	Runners []RunnerDefinition `json:",omitempty"`
}
