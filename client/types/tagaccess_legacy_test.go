/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import "github.com/gobwas/glob"

// This file is a frozen copy of the original CBAC tag access implementation
// from cbac.go. It is the reference the equivalence tests and benchmarks
// compare against, and it must not be changed to track cbac.go.

func legacyMatchGlob(pattern, val string) (r bool) {
	if g, err := glob.Compile(pattern); err == nil && g != nil {
		r = g.Match(val)
	}
	return
}

func legacyMatchTag(pattern, tag string) bool {
	if pattern == tag {
		return true
	} else if isGlob(pattern) && legacyMatchGlob(pattern, tag) {
		return true
	}
	return false
}

func legacyCheck(ta TagAccess, tg string) bool {
	for _, v := range ta.Grants {
		if legacyMatchTag(v, tg) {
			return true
		}
	}
	return false
}

func legacyCheckTagAccess(tg string, prime TagAccess, set []TagAccess) (allowed bool) {
	if allowed = legacyCheck(prime, tg); allowed {
		return
	}
	for _, g := range set {
		if allowed = legacyCheck(g, tg); allowed {
			return
		}
	}
	return
}

func legacyFilterTags(tags []string, prime TagAccess, set []TagAccess) (r []string) {
	for _, t := range tags {
		if legacyCheckTagAccess(t, prime, set) {
			r = append(r, t)
		}
	}
	return
}

func legacyHasTagAccess(ud *UserDetails, tg string) (allowed bool) {
	if allowed = legacyCheck(ud.CBAC.Tags, tg); allowed {
		return
	}
	for _, g := range ud.Groups {
		if allowed = legacyCheck(g.CBAC.Tags, tg); allowed {
			return
		}
	}
	return
}

func legacyUserFilterTags(ud *UserDetails, all []string) (r []string) {
	if ud.Admin {
		return all
	}
	for _, t := range all {
		if legacyHasTagAccess(ud, t) {
			r = append(r, t)
		}
	}
	return
}
