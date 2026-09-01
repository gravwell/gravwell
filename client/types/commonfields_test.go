/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import "testing"

// TestAssetSearchInfoConstant pins the wire value of the new AssetType.
// It must stay "search_info" -- it's what the backend's rtypes.AssetSearchInfo
// (pkg/registry/types/asset.go in the backend repo) and the OpenAPI spec's
// AssetType enum both already expect.
func TestAssetSearchInfoConstant(t *testing.T) {
	if AssetSearchInfo != "search_info" {
		t.Errorf(`AssetSearchInfo = %q, want "search_info"`, AssetSearchInfo)
	}
}

// TestAssetSearchInfoInMap ensures AssetSearchInfo was added to AssetTypeMap
// alongside the const -- ValidateAssetType/lookups over the map are how
// callers typically check a Type string is recognized, so forgetting this
// entry silently makes "search_info" look invalid even though the const
// exists.
func TestAssetSearchInfoInMap(t *testing.T) {
	got, ok := AssetTypeMap[string(AssetSearchInfo)]
	if !ok {
		t.Fatal("AssetSearchInfo missing from AssetTypeMap")
	}
	if got != AssetSearchInfo {
		t.Errorf("AssetTypeMap[%q] = %q, want %q", AssetSearchInfo, got, AssetSearchInfo)
	}
}

func TestValidateAssetTypeAcceptsSearchInfo(t *testing.T) {
	if !ValidateAssetType(string(AssetSearchInfo)) {
		t.Error(`ValidateAssetType("search_info") = false, want true`)
	}
}

// TestAssetTypeMapNoDuplicateValues catches copy/paste mistakes when adding
// a new AssetType const (e.g. reusing an existing string literal), which
// would silently make two Go identifiers collide on the wire.
func TestAssetTypeMapNoDuplicateValues(t *testing.T) {
	seen := make(map[AssetType]bool, len(AssetTypeMap))
	for k, v := range AssetTypeMap {
		if AssetType(k) != v {
			t.Errorf("AssetTypeMap[%q] = %q, key/value mismatch", k, v)
		}
		if seen[v] {
			t.Errorf("duplicate AssetType value %q in AssetTypeMap", v)
		}
		seen[v] = true
	}
}
