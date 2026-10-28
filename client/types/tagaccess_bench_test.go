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
	"os"
	"testing"
)

// CBAC tag access benchmarks.
//
// api=user is UserDetails.FilterTags and UserDetails.HasTagAccess; api=pkg is
// the package-level FilterTags and CheckTagAccess, given the user's grants as
// prime and the groups' grants as set.
//
//	go test -run '^$' -bench TagAccess -benchmem -count 10 ./client/types/
//
// To measure a change, run the same command on the commit before it and on
// the change, then compare the two outputs with benchstat (see the end of this
// comment).
//
// TAG_BENCH_PROFILE selects the fixture sizes:
//
//	dev    (default) 10k tags, 5 groups, 10 grants, 5% globs
//	site   30k tags, 20 groups, 25 grants, 5% globs; sweeps groups up to 200,
//	       the size of a large deployment with hundreds of groups
//	scale  500k tags, 20 groups, 25 grants, 5% globs
//	stress 2M tags, 100 groups, 100 grants, 5% globs
//
// BenchmarkTagAccessFilter sweeps one dimension at a time around the profile
// baseline and reports ns/tag and allowed/op. allowed/op must not change
// between the two sides of a comparison. The dimensions are:
//
//	tags    tags known to the webserver
//	groups  groups the user belongs to
//	grants  grants on the user and on each group
//	globpct percent of grants that are complex globs, not exact or prefix*
//	narrow  1 makes every grant cover at most a few tags, so the user is
//	        allowed only a small fraction of the tag list. Most tags then miss
//	        every grant, which is the worst case.
//
// BenchmarkTagAccessShape ignores TAG_BENCH_PROFILE and runs fixed, named
// shapes (see benchFixedShapes): tags30k-groups200; tags65536-groups20,
// tags65536-groups200, and tags65536-groups20-narrow at the single-indexer
// maximum of 65,536 tags; and tags500k-groups20, tags500k-groups20-globpct0,
// and tags500k-groups1, the original scale measurements.
//
// BenchmarkTagAccessCheck covers exact, prefix, and glob hits, and a miss, at
// the profile baseline.
//
// Compare two runs with benchstat:
//
//	go run golang.org/x/perf/cmd/benchstat@latest old.txt new.txt

const (
	benchFamilies = 100 // distinct tag families, tag i is in family i%benchFamilies
	benchSeed     = 3145
)

// benchShape is one point in the fixture space.
type benchShape struct {
	tags    int
	groups  int
	grants  int
	globPct int
	narrow  bool
}

// benchProfile is a baseline shape plus the values swept for each dimension.
// Each sweep varies one dimension and holds the others at the baseline.
type benchProfile struct {
	base    benchShape
	tags    []int
	groups  []int
	grants  []int
	globPct []int
}

var benchProfiles = map[string]benchProfile{
	"dev": {
		base:    benchShape{tags: 10_000, groups: 5, grants: 10, globPct: 5},
		tags:    []int{1_000, 10_000},
		groups:  []int{1, 5, 20},
		grants:  []int{5, 10, 25},
		globPct: []int{0, 5, 20},
	},
	"site": {
		base:    benchShape{tags: 30_000, groups: 20, grants: 25, globPct: 5},
		groups:  []int{1, 5, 20, 50, 100, 200},
		globPct: []int{0},
	},
	"scale": {
		base:    benchShape{tags: 500_000, groups: 20, grants: 25, globPct: 5},
		tags:    []int{100_000, 500_000, 1_000_000},
		groups:  []int{1, 20, 100},
		grants:  []int{5, 25, 100},
		globPct: []int{0, 5, 20},
	},
	"stress": {
		base:    benchShape{tags: 2_000_000, groups: 100, grants: 100, globPct: 5},
		tags:    []int{1_000_000, 2_000_000, 4_000_000},
		groups:  []int{20, 100, 200},
		grants:  []int{25, 100, 250},
		globPct: []int{0, 5, 20, 50},
	},
}

func loadBenchProfile(b *testing.B) benchProfile {
	b.Helper()
	if testing.Short() {
		b.Skip("tag access benchmarks are skipped in short mode")
	}
	name := os.Getenv("TAG_BENCH_PROFILE")
	if name == "" {
		name = "dev"
	}
	p, ok := benchProfiles[name]
	if !ok {
		b.Fatalf("unknown TAG_BENCH_PROFILE %q, want dev, site, scale, or stress", name)
	}
	return p
}

func benchTagName(i int) string {
	return fmt.Sprintf("f%03d_s%07d", i%benchFamilies, i/benchFamilies)
}

var benchTagCache = map[int][]string{}

// benchTags returns n deterministic tag names. Lists are cached across
// sub-benchmarks because the large ones are slow to build.
func benchTags(n int) []string {
	if t, ok := benchTagCache[n]; ok {
		return t
	}
	t := make([]string, n)
	for i := range t {
		t[i] = benchTagName(i)
	}
	benchTagCache[n] = t
	return t
}

// benchGrant returns one grant derived from a random tag in [0, nTags). Of the
// grants that are not complex globs, 70% are exact names and 30% are prefix*.
// Narrow grants keep more of the tag's sequence number, so each covers at most
// a few tags (at most one per family for the leading-star glob).
func benchGrant(r *rand.Rand, nTags, globPct int, narrow bool) string {
	i := r.IntN(nTags)
	fam, seq := i%benchFamilies, fmt.Sprintf("%07d", i/benchFamilies)
	if r.IntN(100) < globPct {
		kind := r.IntN(4)
		if narrow {
			switch kind {
			case 0:
				return fmt.Sprintf("f%d?%d_s%s", fam/10, fam%10, seq)
			case 1:
				return fmt.Sprintf("f%03d_s%s[0-4]", fam, seq[:6])
			case 2:
				return fmt.Sprintf("{f%03d,f%03d}_s%s", fam, (fam+1)%benchFamilies, seq)
			default:
				return fmt.Sprintf("*_s%s", seq)
			}
		}
		switch kind {
		case 0:
			return fmt.Sprintf("f%d?%d_s%s*", fam/10, fam%10, seq[:5])
		case 1:
			return fmt.Sprintf("f%03d_s%s[0-4]*", fam, seq[:4])
		case 2:
			return fmt.Sprintf("{f%03d,f%03d}_s%s*", fam, (fam+1)%benchFamilies, seq[:5])
		default:
			return fmt.Sprintf("*_s%s?", seq[:6])
		}
	}
	if r.IntN(10) < 3 {
		if narrow {
			return fmt.Sprintf("f%03d_s%s*", fam, seq[:6])
		}
		return fmt.Sprintf("f%03d_s%s*", fam, seq[:4+r.IntN(2)])
	}
	return benchTagName(i)
}

// benchUser builds a non-admin user with s.grants grants of its own and
// s.groups groups of s.grants grants each. The same shape always yields the
// same user.
func benchUser(s benchShape) *UserDetails {
	r := rand.New(rand.NewPCG(benchSeed, uint64(s.groups)<<32|uint64(s.grants)))
	grants := func() []string {
		g := make([]string, s.grants)
		for i := range g {
			g[i] = benchGrant(r, s.tags, s.globPct, s.narrow)
		}
		return g
	}
	ud := &UserDetails{UID: 2, User: "bench"}
	ud.CBAC.Tags.Grants = grants()
	ud.Groups = make([]GroupDetails, s.groups)
	for i := range ud.Groups {
		ud.Groups[i] = GroupDetails{GID: int32(i + 1), Name: fmt.Sprintf("group%d", i)}
		ud.Groups[i].CBAC.Tags.Grants = grants()
	}
	return ud
}

// groupGrants returns the groups' tag grants in the form the package-level
// functions take.
func groupGrants(ud *UserDetails) []TagAccess {
	set := make([]TagAccess, len(ud.Groups))
	for i, g := range ud.Groups {
		set[i] = g.CBAC.Tags
	}
	return set
}

// benchSink keeps results alive so the compiler cannot drop the work.
var (
	benchSinkTags []string
	benchSinkBool bool
)

// BenchmarkTagAccessFilter measures a full filter of the tag list, the work
// done on every search launch, completion, and tag listing.
func BenchmarkTagAccessFilter(b *testing.B) {
	p := loadBenchProfile(b)

	type sweep struct {
		dim    string
		values []int
		set    func(*benchShape, int)
	}
	sweeps := []sweep{
		{"tags", p.tags, func(s *benchShape, v int) { s.tags = v }},
		{"groups", p.groups, func(s *benchShape, v int) { s.groups = v }},
		{"grants", p.grants, func(s *benchShape, v int) { s.grants = v }},
		{"globpct", p.globPct, func(s *benchShape, v int) { s.globPct = v }},
		{"narrow", []int{1}, func(s *benchShape, v int) { s.narrow = v == 1 }},
	}

	for _, impl := range filterImpls {
		for _, sw := range sweeps {
			for _, v := range sw.values {
				shape := p.base
				sw.set(&shape, v)
				b.Run(fmt.Sprintf("api=%s/%s=%d", impl.api, sw.dim, v), func(b *testing.B) {
					benchFilter(b, impl.filter, shape)
				})
			}
		}
	}
}

// benchFixedShapes are named shapes that do not depend on TAG_BENCH_PROFILE.
// Run one by name, for example:
//
//	go test -run '^$' -bench 'TagAccessShape/api=user/shape=tags30k-groups200$' -benchmem ./client/types/
var benchFixedShapes = []struct {
	name  string
	shape benchShape
}{
	// A large single deployment: 30k tags and hundreds of groups.
	{"tags30k-groups200", benchShape{tags: 30_000, groups: 200, grants: 25, globPct: 5}},
	// 65,536 tags is the most one indexer will hold (maxTagCount in the
	// backend's tag manager).
	{"tags65536-groups20", benchShape{tags: 65_536, groups: 20, grants: 25, globPct: 5}},
	{"tags65536-groups200", benchShape{tags: 65_536, groups: 200, grants: 25, globPct: 5}},
	{"tags65536-groups20-narrow", benchShape{tags: 65_536, groups: 20, grants: 25, globPct: 5, narrow: true}},
	// The scale profile baseline and its 0% glob and 1 group variants, the
	// shapes of the original backend measurements.
	{"tags500k-groups20", benchShape{tags: 500_000, groups: 20, grants: 25, globPct: 5}},
	{"tags500k-groups20-globpct0", benchShape{tags: 500_000, groups: 20, grants: 25, globPct: 0}},
	{"tags500k-groups1", benchShape{tags: 500_000, groups: 1, grants: 25, globPct: 5}},
}

// BenchmarkTagAccessShape runs the full filter at each of benchFixedShapes.
func BenchmarkTagAccessShape(b *testing.B) {
	if testing.Short() {
		b.Skip("tag access benchmarks are skipped in short mode")
	}
	for _, impl := range filterImpls {
		for _, fs := range benchFixedShapes {
			b.Run(fmt.Sprintf("api=%s/shape=%s", impl.api, fs.name), func(b *testing.B) {
				benchFilter(b, impl.filter, fs.shape)
			})
		}
	}
}

// filterImpls are the filter entry points every filter benchmark measures.
var filterImpls = []struct {
	api    string
	filter func(ud *UserDetails, set []TagAccess, tags []string) []string
}{
	{"user", func(ud *UserDetails, _ []TagAccess, tags []string) []string {
		return ud.FilterTags(tags)
	}},
	{"pkg", func(ud *UserDetails, set []TagAccess, tags []string) []string {
		return FilterTags(tags, ud.CBAC.Tags, set)
	}},
}

// benchFilter times filter over the tag list and user for shape, and reports
// ns/tag and allowed/op.
func benchFilter(b *testing.B, filter func(*UserDetails, []TagAccess, []string) []string, shape benchShape) {
	b.Helper()
	tags := benchTags(shape.tags)
	ud := benchUser(shape)
	set := groupGrants(ud)
	b.ReportAllocs()
	for b.Loop() {
		benchSinkTags = filter(ud, set, tags)
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(len(tags)), "ns/tag")
	b.ReportMetric(float64(len(benchSinkTags)), "allowed/op")
}

// BenchmarkTagAccessCheck measures a single tag check at the profile baseline.
// The hit cases are resolved by a grant on the user's last group, which is the
// slowest place for a hit.
func BenchmarkTagAccessCheck(b *testing.B) {
	p := loadBenchProfile(b)
	shape := p.base
	tags := benchTags(shape.tags)
	ud := benchUser(shape)

	// pick returns the first tag at or after index i that the fixture does
	// not already grant, so only the grant appended below can allow it.
	pick := func(i int) string {
		for ; i < len(tags); i++ {
			if !ud.HasTagAccess(tags[i]) {
				return tags[i]
			}
		}
		b.Fatalf("no ungranted tag at or after index %d", i)
		return ""
	}
	exact, prefix, glob := pick(len(tags)/2), pick(len(tags)/3), pick(len(tags)/4)
	last := &ud.Groups[len(ud.Groups)-1].CBAC.Tags
	last.Grants = append(last.Grants,
		exact,
		prefix+"*",
		fmt.Sprintf("%s[0-9]", glob[:len(glob)-1]),
	)
	set := groupGrants(ud)

	cases := []struct {
		name string
		tag  string
		want bool
	}{
		{"exact", exact, true},
		{"prefix", prefix, true},
		{"glob", glob, true},
		{"miss", "zz_nomatch", false},
	}
	impls := []struct {
		api   string
		check func(string) bool
	}{
		{"user", ud.HasTagAccess},
		{"pkg", func(tg string) bool { return CheckTagAccess(tg, ud.CBAC.Tags, set) }},
	}

	for _, impl := range impls {
		for _, c := range cases {
			if got := impl.check(c.tag); got != c.want {
				b.Fatalf("api=%s case=%s tag %q: got %v, want %v", impl.api, c.name, c.tag, got, c.want)
			}
			b.Run(fmt.Sprintf("api=%s/case=%s", impl.api, c.name), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					benchSinkBool = impl.check(c.tag)
				}
			})
		}
	}
}
