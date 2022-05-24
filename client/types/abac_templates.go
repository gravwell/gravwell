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
	fullCapStringList []string
	fullCapList       []Capability
	templateSet       []CapabilityTemplate
	capabilitySet     = [...]CapabilityDesc{
		Search.CapabilityDesc(),
		Download.CapabilityDesc(),
		AttachSearch.CapabilityDesc(),
		SaveSearch.CapabilityDesc(),
		BackgroundSearch.CapabilityDesc(),
		GetTags.CapabilityDesc(),
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
	}
	readOnlyCapList = []Capability{
		Search,
		Download,
		AttachSearch,
		GetTags,
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
		KitRead,
		PlaybookRead,
		LicenseRead,
		Stats,
		NotificationRead,
		SystemInfoRead,
		TokenRead,
		SecretRead,
	}
)

func init() {
	fullCapList = make([]Capability, 0, len(capabilitySet))
	fullCapStringList = make([]string, 0, len(capabilitySet))
	for _, v := range capabilitySet {
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
}

func CapabilityDescriptions() (r []CapabilityDesc) {
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
