/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

func tagUser(admin bool, user []string, groups ...[]string) *UserDetails {
	ud := &UserDetails{UID: 2, Admin: admin}
	ud.CBAC.Tags.Grants = user
	for i, g := range groups {
		grp := GroupDetails{GID: int32(i + 1)}
		grp.CBAC.Tags.Grants = g
		ud.Groups = append(ud.Groups, grp)
	}
	return ud
}

// outcome runs f and returns its result, or "panic" if it panics. Some valid
// gobwas patterns, such as "a?-{}", panic in Match, so a panic is an outcome
// to compare, not a test failure.
func outcome(f func() any) (r any) {
	defer func() {
		if recover() != nil {
			r = "panic"
		}
	}()
	return f()
}

// assertConsistent checks that the filters, which use the merged matcher,
// agree with the per-tag checks, which walk the grants one at a time:
// UserDetails.FilterTags and the package-level FilterTags (with the user's
// grants as prime and the groups' grants as set) must keep exactly the tags
// that HasTagAccess allows, and return nil when they keep none.
// UserDetails.FilterTags returns the input unchanged for admins. Some gobwas
// patterns panic in Match. The checks walk grants in order, so they can panic
// where the filters, which try exact and prefix grants first, do not; when a
// check panics, the filters are not compared.
func assertConsistent(t *testing.T, ud *UserDetails, tags []string) {
	t.Helper()
	set := make([]TagAccess, len(ud.Groups))
	for i, g := range ud.Groups {
		set[i] = g.CBAC.Tags
	}
	prime := ud.CBAC.Tags

	var want []string
	for _, tg := range tags {
		got := outcome(func() any { return ud.HasTagAccess(tg) })
		if pkg := outcome(func() any { return CheckTagAccess(tg, prime, set) }); pkg != got {
			t.Errorf("CheckTagAccess(%q) = %v, HasTagAccess %v (user %q, groups %v)", tg, pkg, got, prime.Grants, set)
		}
		if got == "panic" {
			return
		}
		if got == true {
			want = append(want, tg)
		}
	}

	userWant := want
	if ud.Admin {
		userWant = tags
	}
	filters := []struct {
		name string
		got  func() any
		want []string
	}{
		{"UserDetails.FilterTags", func() any { return ud.FilterTags(tags) }, userWant},
		{"FilterTags", func() any { return FilterTags(tags, prime, set) }, want},
	}
	for _, f := range filters {
		if got := outcome(f.got); !reflect.DeepEqual(got, f.want) {
			t.Errorf("%s(%q) = %#v, want %#v (user %q, groups %v)", f.name, tags, got, f.want, prime.Grants, set)
		}
	}
}

// TestTagAccessPinned pins the edge cases whose answers are easy to get wrong,
// so they do not depend only on the filters agreeing with the checks.
func TestTagAccessPinned(t *testing.T) {
	cases := []struct {
		name  string
		grant string
		tag   string
		want  bool
	}{
		{"prefix", "zeek*", "zeekconn", true},
		{"prefix matches the bare prefix", "zeek*", "zeek", true},
		{"escaped star is literal", `foo\*`, "foo*", true},
		{"escaped star is not a prefix", `foo\*`, "foobar", false},
		{"invalid pattern never matches", "[", "anything", false},
		{"invalid pattern matches itself", "[", "[", true},
		{"glob grant matches itself", "{a,b}", "{a,b}", true},
		{"question mark is one character", "a?", "ab", true},
		{"question mark is not two characters", "a?", "abc", false},
		{"star allows the empty tag", "*", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ud := tagUser(false, []string{c.grant})
			if got := ud.HasTagAccess(c.tag); got != c.want {
				t.Errorf("HasTagAccess(%q) with grant %q = %v, want %v", c.tag, c.grant, got, c.want)
			}
			got := ud.FilterTags([]string{c.tag})
			if (len(got) == 1) != c.want {
				t.Errorf("FilterTags([%q]) with grant %q = %q, want allowed %v", c.tag, c.grant, got, c.want)
			}
		})
	}

	if got := tagUser(false, []string{"zeek*"}).FilterTags([]string{"default"}); got != nil {
		t.Errorf("FilterTags with no match = %#v, want nil", got)
	}
}

func TestTagAccessConsistent(t *testing.T) {
	tags := []string{
		"", "default", "syslog", "syslog2", "sys", "zeekconn", "zeekdns", "zeek",
		"win", "winlog", "a", "ab", "abc", "abd", "b", "foo", "foobar", "foo*",
		"a*", "{a,b}", "[", "a!b", "unknown", "é", "éa", "\xff", "\xffa", "a--",
	}
	cases := []struct {
		name   string
		admin  bool
		user   []string
		groups [][]string
	}{
		{name: "no grants"},
		{name: "empty grant lists", user: []string{}, groups: [][]string{{}, {}}},
		{name: "admin without grants", admin: true},
		{name: "admin with grants", admin: true, user: []string{"default"}, groups: [][]string{{"zeek*"}}},
		{name: "exact", user: []string{"default", "syslog"}},
		{name: "prefix", user: []string{"zeek*"}},
		{name: "star", user: []string{"*"}},
		{name: "double star", user: []string{"a**"}},
		{name: "question", user: []string{"a?"}},
		{name: "class", user: []string{"[a-b]*"}},
		{name: "negated class", user: []string{"[!a]*"}},
		{name: "alternation", user: []string{"{syslog,zeek}*"}},
		{name: "escaped star is literal", user: []string{`foo\*`}},
		{name: "escaped char in prefix", user: []string{`fo\o*`}},
		{name: "invalid pattern", user: []string{"["}},
		{name: "invalid pattern among valid", user: []string{"[", "win*"}},
		{name: "bang is not a prefix grant", user: []string{"a!*"}},
		{name: "glob grant matches itself exactly", user: []string{"a*", "{a,b}", "a!b"}},
		{name: "invalid UTF-8 prefix", user: []string{"\xff*"}},
		{name: "multibyte prefix", user: []string{"é*"}},
		{name: "user only", user: []string{"win*"}, groups: [][]string{{}}},
		{name: "group only", groups: [][]string{{"zeekconn"}, {"sys*"}}},
		{name: "user and groups", user: []string{"default"}, groups: [][]string{{"zeek*"}, {"a?"}}},
		{name: "duplicates across sets", user: []string{"ab*", "syslog"}, groups: [][]string{{"ab*"}, {"syslog", "ab*"}}},
		{name: "subsumed prefixes", user: []string{"abc*", "ab*", "a*", "zeekc*"}, groups: [][]string{{"zeek*"}}},
		{name: "sibling prefixes", user: []string{"abd*", "abc*", "sy*"}},
		{name: "empty prefix", groups: [][]string{{"*", "default"}}},
		{name: "empty string grant", user: []string{""}},
		{name: "gobwas panic is preserved", user: []string{"a?-{}"}},
		{name: "gobwas panic before a later exact grant", user: []string{"a?-{}"}, groups: [][]string{{"a--"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertConsistent(t, tagUser(c.admin, c.user, c.groups...), tags)
		})
	}

	t.Run("no tags", func(t *testing.T) {
		assertConsistent(t, tagUser(false, []string{"*"}), nil)
		assertConsistent(t, tagUser(false, []string{"*"}), []string{})
	})
}

func TestTagAccessConsistentRandom(t *testing.T) {
	r := rand.New(rand.NewPCG(3145, 1))
	grantChars := []string{"a", "b", "c", "_", "-", "*", "?", "[", "]", "{", "}", ",", "!", `\`, "é", "\xff"}
	tagChars := []string{"a", "b", "c", "_", "-", "é", "\xff"}
	randStr := func(chars []string, maxLen int) string {
		var sb strings.Builder
		for range r.IntN(maxLen + 1) {
			sb.WriteString(chars[r.IntN(len(chars))])
		}
		return sb.String()
	}
	randGrants := func() []string {
		g := make([]string, r.IntN(4))
		for i := range g {
			g[i] = randStr(grantChars, 5)
			if r.IntN(3) == 0 {
				g[i] = randStr(tagChars, 3) + "*"
			}
		}
		return g
	}

	for i := range 3000 {
		var groups [][]string
		for range r.IntN(4) {
			groups = append(groups, randGrants())
		}
		u := tagUser(false, randGrants(), groups...)
		tags := make([]string, 20)
		for j := range tags {
			tags[j] = randStr(tagChars, 5)
		}
		// Grants as tags exercise the exact-equality path for glob grants.
		tags = append(tags, u.CBAC.Tags.Grants...)
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			assertConsistent(t, u, tags)
		})
		if t.Failed() {
			return
		}
	}
}

func TestPrefixGrant(t *testing.T) {
	cases := []struct {
		pattern string
		prefix  string
		ok      bool
	}{
		{"*", "", true},
		{"abc*", "abc", true},
		{"é*", "é", true},
		{"abc", "", false},
		{"", "", false},
		{"a**", "", false},
		{"a?*", "", false},
		{"[ab]*", "", false},
		{"{a,b}*", "", false},
		{"a!*", "", false},
		{`foo\*`, "", false},
		{`fo\o*`, "", false},
		{"*abc", "", false},
		{"\xff*", "", false},
	}
	for _, c := range cases {
		t.Run(c.pattern, func(t *testing.T) {
			p, ok := prefixGrant(c.pattern)
			if p != c.prefix || ok != c.ok {
				t.Errorf("prefixGrant(%q) = %q, %v, want %q, %v", c.pattern, p, ok, c.prefix, c.ok)
			}
		})
	}
}

func TestPruneSubsumed(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil", nil, []string{}},
		{"single", []string{"a"}, []string{"a"}},
		{"duplicates", []string{"ab", "ab"}, []string{"ab"}},
		{"subsumed", []string{"abc", "a", "ab"}, []string{"a"}},
		{"siblings", []string{"abd", "abc", "b"}, []string{"abc", "abd", "b"}},
		{"mixed", []string{"zeekc", "abc", "zeek", "ab", "b"}, []string{"ab", "b", "zeek"}},
		{"empty prefix covers all", []string{"x", "", "y"}, []string{""}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := pruneSubsumed(slices.Clone(c.in))
			if !slices.Equal(got, c.want) {
				t.Errorf("pruneSubsumed(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// resetGlobCache empties the process-wide glob cache for the test and again
// when it ends. Tests that call it must not run in parallel.
func resetGlobCache(t *testing.T) {
	t.Helper()
	reset := func() {
		globCache.Clear()
		globCacheSize.Store(0)
	}
	reset()
	t.Cleanup(reset)
}

func TestCompiledGlob(t *testing.T) {
	resetGlobCache(t)
	cases := []struct {
		pattern string
		tag     string
		valid   bool
		match   bool
	}{
		{"a?c", "abc", true, true},
		{"a?c", "abbc", true, false},
		{"{x,y}*", "yes", true, true},
		{`foo\*`, "foo*", true, true},
		{`foo\*`, "foobar", true, false},
		{"[", "[", false, false},
		{"[a-z", "a", false, false}, // invalid upstream and in the djfritz fork; "{a,b" is valid upstream
	}
	for _, c := range cases {
		t.Run(c.pattern+"/"+c.tag, func(t *testing.T) {
			for range 2 { // the second call is served from the cache
				g := compiledGlob(c.pattern)
				if (g != nil) != c.valid {
					t.Fatalf("compiledGlob(%q) valid = %v, want %v", c.pattern, g != nil, c.valid)
				}
				if g != nil && g.Match(c.tag) != c.match {
					t.Fatalf("compiledGlob(%q).Match(%q) = %v, want %v", c.pattern, c.tag, !c.match, c.match)
				}
				v, ok := globCache.Load(c.pattern)
				if !ok {
					t.Fatalf("pattern %q not cached", c.pattern)
				}
				if c.valid != (v != nil) {
					t.Fatalf("cached entry for %q = %v, want valid %v", c.pattern, v, c.valid)
				}
			}
		})
	}
	if got, want := globCacheSize.Load(), int64(5); got != want {
		t.Errorf("cache size = %d, want %d distinct patterns", got, want)
	}
}

func TestCompiledGlobCap(t *testing.T) {
	resetGlobCache(t)
	for i := range maxGlobCache {
		compiledGlob(fmt.Sprintf("p%d?", i))
	}
	if got := globCacheSize.Load(); got != maxGlobCache {
		t.Fatalf("cache size = %d, want %d", got, maxGlobCache)
	}
	// The next new pattern empties the full cache and is then cached itself.
	g := compiledGlob("over?cap")
	if g == nil || !g.Match("overXcap") {
		t.Fatal("pattern past the cap did not compile and match")
	}
	if _, ok := globCache.Load("over?cap"); !ok {
		t.Error("pattern past the cap was not cached after the reset")
	}
	if _, ok := globCache.Load("p0?"); ok {
		t.Error("pattern from before the reset is still cached")
	}
	if got := globCacheSize.Load(); got != 1 {
		t.Errorf("cache size = %d after the reset, want 1", got)
	}

	// A pattern dropped by the reset compiles again and is cached again.
	g = compiledGlob("p0?")
	if g == nil || !g.Match("p0x") {
		t.Fatal("pattern dropped by the reset did not compile and match")
	}
	if _, ok := globCache.Load("p0?"); !ok {
		t.Error("pattern dropped by the reset was not cached again")
	}
	if got := globCacheSize.Load(); got != 2 {
		t.Errorf("cache size = %d, want 2", got)
	}
}

// TestCompiledGlobConcurrentReset drives concurrent callers through several
// resets. Every result must still match, the counter must never fall below the
// number of cached patterns, and the cache must stay near the cap.
func TestCompiledGlobConcurrentReset(t *testing.T) {
	resetGlobCache(t)
	const workers, perWorker = 8, 3 * maxGlobCache / 8
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range perWorker {
				// Each worker revisits a few shared patterns between its own,
				// so hits and misses race with the resets.
				n := i*perWorker + j
				if j%4 == 0 {
					n = j % 64
				}
				g := compiledGlob(fmt.Sprintf("r%d?", n))
				if g == nil || !g.Match(fmt.Sprintf("r%dx", n)) {
					t.Errorf("compiledGlob(r%d?) did not match during resets", n)
					return
				}
			}
		}()
	}
	wg.Wait()

	var cached int64
	globCache.Range(func(_, _ any) bool {
		cached++
		return true
	})
	if size := globCacheSize.Load(); size < cached {
		t.Errorf("counter %d is below the %d cached patterns", size, cached)
	}
	if cached > maxGlobCache+workers {
		t.Errorf("cache holds %d patterns, want at most %d", cached, maxGlobCache+workers)
	}
}

func TestCompiledGlobConcurrent(t *testing.T) {
	resetGlobCache(t)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 200 {
				g := compiledGlob(fmt.Sprintf("c%d?", (i+j)%50))
				if g == nil || !g.Match(fmt.Sprintf("c%dx", (i+j)%50)) {
					t.Errorf("concurrent compiledGlob mismatch for pattern %d", (i+j)%50)
					return
				}
			}
		}()
	}
	wg.Wait()
	if got := globCacheSize.Load(); got != 50 {
		t.Errorf("cache size = %d, want 50", got)
	}
}

// TestFilterTagsCompilesGlobOnce is the regression test for compiling glob
// grants once per tag on every filter: a full filter must leave exactly one
// cache entry per distinct glob grant.
func TestFilterTagsCompilesGlobOnce(t *testing.T) {
	resetGlobCache(t)
	tags := make([]string, 1000)
	for i := range tags {
		tags[i] = fmt.Sprintf("tag%03d", i)
	}
	u := tagUser(false, []string{"tag00?", "exact"}, []string{"tag01[0-4]", "tag00?", "tag9*"})
	got := u.FilterTags(tags)
	if len(got) != 10+5+100 {
		t.Fatalf("FilterTags kept %d tags, want 115", len(got))
	}
	// Two distinct complex globs; exact and prefix grants never compile.
	if n := globCacheSize.Load(); n != 2 {
		t.Errorf("glob cache holds %d entries after a filter, want 2", n)
	}
	if !u.HasTagAccess("tag005") || globCacheSize.Load() != 2 {
		t.Error("HasTagAccess did not reuse the cached glob")
	}
}
