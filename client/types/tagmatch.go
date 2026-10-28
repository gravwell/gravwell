/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/gobwas/glob"
)

// maxGlobCache caps the process-wide compiled glob cache. Only complex globs
// are cached, not exact or prefix* grants, so 8192 is well above any
// realistic set of distinct patterns (200 groups of 25 grants is 5,000 grants
// in all). An entry is at most about 1.6 KB, so a full cache is about 13 MB.
// Patterns from deleted or edited grants are never removed one by one, so a
// full cache is emptied and refilled; live patterns compile again on their
// next use.
const maxGlobCache = 8192

var (
	globCache     sync.Map // pattern string -> glob.Glob, nil if the pattern does not compile
	globCacheSize atomic.Int64
)

// compiledGlob returns the compiled matcher for pattern, or nil if pattern does
// not compile. Failures are cached too, so a bad grant is not recompiled on
// every check.
func compiledGlob(pattern string) glob.Glob {
	if v, ok := globCache.Load(pattern); ok {
		g, _ := v.(glob.Glob)
		return g
	}
	g, err := glob.Compile(pattern)
	if err != nil {
		g = nil
	}
	// Only the caller that wins the swap clears, so concurrent callers do not
	// empty the cache twice. A store that lands between the swap and Clear is
	// lost and only makes the count run high, which clears a little early.
	if n := globCacheSize.Load(); n >= maxGlobCache && globCacheSize.CompareAndSwap(n, 0) {
		globCache.Clear()
	}
	if _, loaded := globCache.LoadOrStore(pattern, g); !loaded {
		globCacheSize.Add(1)
	}
	return g
}

// prefixGrant reports whether pattern is a pure "prefix*" grant and returns the
// prefix. Patterns with a backslash are excluded because `foo\*` is an escaped,
// literal match in gobwas, not a prefix. Invalid UTF-8 is excluded because
// gobwas decodes the pattern into runes, which changes the bytes it matches.
func prefixGrant(pattern string) (string, bool) {
	p, ok := strings.CutSuffix(pattern, "*")
	if !ok || strings.ContainsAny(p, globChars) || strings.ContainsRune(p, '\\') || !utf8.ValidString(p) {
		return "", false
	}
	return p, true
}

// grantMatches reports whether a single grant allows tag: an exact name, a
// pure prefix, or a compiled glob with no separators.
func grantMatches(pattern, tag string) bool {
	if pattern == tag {
		return true
	} else if !isGlob(pattern) {
		return false
	} else if p, ok := prefixGrant(pattern); ok {
		return strings.HasPrefix(tag, p)
	}
	g := compiledGlob(pattern)
	return g != nil && g.Match(tag)
}

// tagMatcher is the merged, deduplicated set of several TagAccess grant lists,
// such as a user's and their groups', built once so a list of tags can be
// filtered in a single pass.
type tagMatcher struct {
	all      bool                // a "*" grant was seen
	exact    map[string]struct{} // every grant verbatim, globs included; also the dedup set
	prefixes []string            // sorted, with prefixes covered by a shorter one removed
	globs    []glob.Glob
}

// newTagMatcher merges prime and every TagAccess in set into one matcher. Grants
// are added in the order CheckTagAccess checks them, so globs run in that order
// too.
func newTagMatcher(prime TagAccess, set []TagAccess) *tagMatcher {
	n := len(prime.Grants)
	for _, ta := range set {
		n += len(ta.Grants)
	}
	m := &tagMatcher{exact: make(map[string]struct{}, n)}
	m.addGrants(prime.Grants)
	for _, ta := range set {
		m.addGrants(ta.Grants)
	}
	m.prefixes = pruneSubsumed(m.prefixes)
	return m
}

func (m *tagMatcher) addGrants(grants []string) {
	for _, gr := range grants {
		if _, dup := m.exact[gr]; dup {
			continue
		}
		m.exact[gr] = struct{}{}
		if !isGlob(gr) {
			continue
		}
		if p, ok := prefixGrant(gr); ok {
			m.all = m.all || p == ""
			m.prefixes = append(m.prefixes, p)
		} else if g := compiledGlob(gr); g != nil {
			m.globs = append(m.globs, g)
		}
	}
}

// pruneSubsumed sorts ps in place and drops every prefix that has a shorter
// prefix in the set. Afterwards no element is a prefix of another, so the only
// element that can be a prefix of a tag is the greatest element <= that tag.
func pruneSubsumed(ps []string) []string {
	slices.Sort(ps)
	out := ps[:0]
	for _, p := range ps {
		if len(out) == 0 || !strings.HasPrefix(p, out[len(out)-1]) {
			out = append(out, p)
		}
	}
	return out
}

func (m *tagMatcher) match(tag string) bool {
	if m.all {
		return true
	}
	if _, ok := m.exact[tag]; ok {
		return true
	}
	if i, found := slices.BinarySearch(m.prefixes, tag); found {
		return true
	} else if i > 0 && strings.HasPrefix(tag, m.prefixes[i-1]) {
		return true
	}
	for _, g := range m.globs {
		if g.Match(tag) {
			return true
		}
	}
	return false
}
