/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"fmt"
)

const (
	TemplateFullUserName = `Full User`
	TemplateReadOnlyName = `Read Only User`
)

var (
	fullTagAccess            = TagAccess{Grants: []string{`*`}}
	fullCapabilityState      CapabilityState
	adminOnlyCapabilityState CapabilityState
	fullCapStringList        []string
	fullCapList              []Capability
	templateSet              []CapabilityTemplate
	capabilitySet            = [...]CapabilityDesc{
		Search.CapabilityDesc(),
		Download.CapabilityDesc(),
		AttachSearch.CapabilityDesc(),
		SaveSearch.CapabilityDesc(),
		BackgroundSearch.CapabilityDesc(),
		SetSearchGroup.CapabilityDesc(),
		SearchHistory.CapabilityDesc(),
		SearchGroupHistory.CapabilityDesc(),
		SearchAllHistory.CapabilityDesc(),
		DashboardRead.CapabilityDesc(),
		DashboardWrite.CapabilityDesc(),
		ResourceRead.CapabilityDesc(),
		ResourceWrite.CapabilityDesc(),
		TemplateRead.CapabilityDesc(),
		TemplateWrite.CapabilityDesc(),
		PivotRead.CapabilityDesc(),
		PivotWrite.CapabilityDesc(),
		MacroRead.CapabilityDesc(),
		MacroWrite.CapabilityDesc(),
		LibraryRead.CapabilityDesc(),
		LibraryWrite.CapabilityDesc(),
		ExtractorRead.CapabilityDesc(),
		ExtractorWrite.CapabilityDesc(),
		UserFileRead.CapabilityDesc(),
		UserFileWrite.CapabilityDesc(),
		KitRead.CapabilityDesc(),
		KitWrite.CapabilityDesc(),
		KitBuild.CapabilityDesc(),
		KitDownload.CapabilityDesc(),
		ScheduleRead.CapabilityDesc(),
		ScheduleWrite.CapabilityDesc(),
		SOARLibs.CapabilityDesc(),
		SOAREmail.CapabilityDesc(),
		PlaybookRead.CapabilityDesc(),
		PlaybookWrite.CapabilityDesc(),

		LicenseRead.CapabilityDesc(),
		Stats.CapabilityDesc(),
		Ingest.CapabilityDesc(),
		ListUsers.CapabilityDesc(),
		ListGroups.CapabilityDesc(),
		ListGroupMembers.CapabilityDesc(),
		NotificationRead.CapabilityDesc(),
		NotificationWrite.CapabilityDesc(),
		SystemInfoRead.CapabilityDesc(),
		TokenRead.CapabilityDesc(),
		TokenWrite.CapabilityDesc(),
		SecretRead.CapabilityDesc(),
		SecretWrite.CapabilityDesc(),
		AlertRead.CapabilityDesc(),
		AlertWrite.CapabilityDesc(),
		LogbotAI.CapabilityDesc(),
	}
	readOnlyCapList = []Capability{
		Search,
		Download,
		AttachSearch,
		SetSearchGroup, //required to be able to search if default group is set
		SearchHistory,
		SearchGroupHistory,
		SearchAllHistory,
		DashboardRead,
		ResourceRead,
		TemplateRead,
		PivotRead,
		MacroRead,
		LibraryRead,
		ExtractorRead,
		UserFileRead,
		PlaybookRead,
		LicenseRead,
		Stats,
		NotificationRead,
		SystemInfoRead,
		TokenRead,
		SecretRead,
		AlertRead,
		LogbotAI,
	}
	adminOnlyCapList = []Capability{
		KitRead,
		KitWrite,
		KitBuild,
		KitDownload,
	}
	tokenOnlyCapList = []Capability{
		KitRead,
		KitWrite,
		KitBuild,
		KitDownload,
	}
)

func init() {
	fullCapList = make([]Capability, 0, len(capabilitySet))
	fullCapStringList = make([]string, 0, len(capabilitySet))
	for _, v := range capabilitySet {
		if v.TokenOnly || v.AdminOnly {
			continue
		}
		fullCapList = append(fullCapList, v.Cap)
		fullCapStringList = append(fullCapStringList, v.Cap.Name())
	}
	templateSet = []CapabilityTemplate{
		CapabilityTemplate{
			Name: TemplateFullUserName,
			Desc: "Standard user that can operate all aspects of Gravwell.\nThis user has full access to automations.",
			Caps: fullCapList,
		},
		CapabilityTemplate{
			Name: TemplateReadOnlyName,
			Desc: "Standard user that can access all resources, but not create or modify stored data.\nThis user can execute queries.\nThis user cannot access automations.",
			Caps: readOnlyCapList,
		},
	}
	fullCapabilityState = CapabilityState{
		Grants: make([]string, 0, len(fullCapList)),
	}
	for _, c := range fullCapList {
		fullCapabilityState.Grants = append(fullCapabilityState.Grants, c.Name())
	}
	adminOnlyCapabilityState = CapabilityState{
		Grants: make([]string, 0, len(adminOnlyCapList)),
	}
	for _, c := range adminOnlyCapList {
		adminOnlyCapabilityState.Grants = append(adminOnlyCapabilityState.Grants, c.Name())
	}

}

func AllTagAccess() TagAccess {
	return fullTagAccess
}

func AllCapabilityAccess() CapabilityState {
	return fullCapabilityState
}

func AdminOnlyCapabilityList() []Capability {
	return adminOnlyCapList
}

func TokenOnlyCapabilityList() []Capability {
	return tokenOnlyCapList
}

func CapabilityDescriptions(admin bool, token bool) (r []CapabilityDesc) {
	for _, desc := range capabilitySet {
		if !desc.AdminOnly && !desc.TokenOnly { // include all plain caps
			r = append(r, desc)
			continue
		}
		if desc.AdminOnly && admin { // if admin caps requested and user is admin include admin caps
			if desc.TokenOnly && !token { // unless it's also a token cap and those weren't requested
				continue
			}
			r = append(r, desc)
		}
		if desc.TokenOnly && token { // if token caps requests include token caps
			if desc.AdminOnly && !admin { // unless it's also an admin cap and those weren't requested
				continue
			}
			r = append(r, desc)
		}
	}
	return capabilitySet[:]
}

func TemplateList() []CapabilityTemplate {
	return templateSet[:]
}

func CapabilityList() []Capability {
	return fullCapList
}

func CapabilityStringList() []string {
	return fullCapStringList
}

func ValidateCapabilities(cps []Capability) (err error) {
	for _, c := range cps {
		if c >= _maxCap {
			err = fmt.Errorf("uknown capability ID %d", c)
			break
		}
	}
	return
}
