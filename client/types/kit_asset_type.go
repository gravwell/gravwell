/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

// KitAssetType represents the type of an item in a kit. It is a superset of
// AssetType, also including types for kit-only items (license, external)
// which have no counterpart in the webserver registry.
type KitAssetType string

const (
	// Registry-backed kit asset types. The string values match the corresponding AssetType.
	KitAssetMacro           KitAssetType = KitAssetType(AssetMacro)
	KitAssetAX              KitAssetType = KitAssetType(AssetAX)
	KitAssetSavedQuery      KitAssetType = KitAssetType(AssetSavedQuery)
	KitAssetResource        KitAssetType = KitAssetType(AssetResource)
	KitAssetFile            KitAssetType = KitAssetType(AssetFile)
	KitAssetTemplate        KitAssetType = KitAssetType(AssetTemplate)
	KitAssetScheduledSearch KitAssetType = KitAssetType(AssetScheduledSearch)
	KitAssetScheduledScript KitAssetType = KitAssetType(AssetScheduledScript)
	KitAssetFlow            KitAssetType = KitAssetType(AssetFlow)
	KitAssetAlert           KitAssetType = KitAssetType(AssetAlert)
	KitAssetPlaybook        KitAssetType = KitAssetType(AssetPlaybook)
	KitAssetDashboard       KitAssetType = KitAssetType(AssetDashboard)
	KitAssetActionable      KitAssetType = KitAssetType(AssetActionable)

	// Kit-only types not backed by the webserver registry.
	KitAssetExternal KitAssetType = "external"
	KitAssetLicense  KitAssetType = "license"
)

var kitAssetTypeSet = map[KitAssetType]struct{}{
	KitAssetMacro:           {},
	KitAssetAX:              {},
	KitAssetSavedQuery:      {},
	KitAssetResource:        {},
	KitAssetFile:            {},
	KitAssetTemplate:        {},
	KitAssetScheduledSearch: {},
	KitAssetScheduledScript: {},
	KitAssetFlow:            {},
	KitAssetAlert:           {},
	KitAssetPlaybook:        {},
	KitAssetDashboard:       {},
	KitAssetActionable:      {},
	KitAssetExternal:        {},
	KitAssetLicense:         {},
}

// Valid reports whether kat is a recognized kit asset type.
func (kat KitAssetType) Valid() bool {
	_, ok := kitAssetTypeSet[kat]
	return ok
}

// AsAssetType converts a KitAssetType to an AssetType when the type is backed
// by the webserver registry. Returns the zero value and false for kit-only types.
func (kat KitAssetType) AsAssetType() (AssetType, bool) {
	at := AssetType(kat)
	if ValidateAssetType(string(at)) {
		return at, true
	}
	return "", false
}
