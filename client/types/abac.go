/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest"
)

type Capability uint16

const (
	Search             Capability = 0
	Download           Capability = 1
	SaveSearch         Capability = 2
	AttachSearch       Capability = 3
	BackgroundSearch   Capability = 4
	GetTags            Capability = 5
	SetSearchGroup     Capability = 6
	SearchHistory      Capability = 7
	SearchGroupHistory Capability = 8
	SearchAllHistory   Capability = 9
	DashboardRead      Capability = 10
	DashboardWrite     Capability = 11
	ResourceRead       Capability = 12
	ResourceWrite      Capability = 13
	TemplateRead       Capability = 14
	TemplateWrite      Capability = 15
	PivotRead          Capability = 16
	PivotWrite         Capability = 17
	MacroRead          Capability = 18
	MacroWrite         Capability = 19
	LibraryRead        Capability = 20
	LibraryWrite       Capability = 21
	ExtractorRead      Capability = 22
	ExtractorWrite     Capability = 23
	UserFileRead       Capability = 24
	UserFileWrite      Capability = 25
	KitRead            Capability = 26
	KitWrite           Capability = 27
	KitBuild           Capability = 28
	KitDownload        Capability = 29
	ScheduleRead       Capability = 30
	ScheduleWrite      Capability = 31
	SOARLibs           Capability = 32
	SOAREmail          Capability = 33
	PlaybookRead       Capability = 34
	PlaybookWrite      Capability = 35

	//management capabilities
	LicenseRead       Capability = 36
	Stats             Capability = 37
	Ingest            Capability = 38
	ListUsers         Capability = 39
	ListGroups        Capability = 40
	ListGroupMembers  Capability = 41
	NotificationRead  Capability = 42
	NotificationWrite Capability = 43
	SystemInfoRead    Capability = 44
	TokenRead         Capability = 45
	TokenWrite        Capability = 46
)

const (
	TokenHeader string = `Gravwell-Token`
)

var (
	ErrUnknownCapability = errors.New("Unknown capability")
)

type CapabilityDesc struct {
	Cap  Capability
	Name string
	Desc string
}

type CapabilityTemplate struct {
	Name string
	Desc string
	Caps []Capability
}

func (c Capability) CapabilityDesc() CapabilityDesc {
	return CapabilityDesc{
		Cap:  c,
		Name: c.Name(),
		Desc: c.Description(),
	}
}

func (c Capability) Name() string {
	switch c {
	case Search:
		return `Search`
	case Download:
		return `Download`
	case SaveSearch:
		return `SaveSearch`
	case AttachSearch:
		return `AttachSearch`
	case BackgroundSearch:
		return `BackgroundSearch`
	case GetTags:
		return `GetTags`
	case SetSearchGroup:
		return `SetSearchGroup`
	case SearchHistory:
		return `SearchHistory`
	case SearchGroupHistory:
		return `SearchGroupHistory`
	case SearchAllHistory:
		return `SearchAllHistory`
	case DashboardRead:
		return `DashboardRead`
	case DashboardWrite:
		return `DashboardWrite`
	case ResourceRead:
		return `ResourceRead`
	case ResourceWrite:
		return `ResourceWrite`
	case TemplateRead:
		return `TemplateRead`
	case TemplateWrite:
		return `TemplateWrite`
	case PivotRead:
		return `PivotRead`
	case PivotWrite:
		return `PivotWrite`
	case MacroRead:
		return `MacroRead`
	case MacroWrite:
		return `MacroWrite`
	case LibraryRead:
		return `LibraryRead`
	case LibraryWrite:
		return `LibraryWrite`
	case ExtractorRead:
		return `ExtractorRead`
	case ExtractorWrite:
		return `ExtractorWrite`
	case UserFileRead:
		return `UserFileRead`
	case UserFileWrite:
		return `UserFileWrite`
	case KitRead:
		return `KitRead`
	case KitWrite:
		return `KitWrite`
	case KitBuild:
		return `KitBuild`
	case KitDownload:
		return `KitDownload`
	case ScheduleRead:
		return `ScheduleRead`
	case ScheduleWrite:
		return `ScheduleWrite`
	case SOARLibs:
		return `SOARLibs`
	case SOAREmail:
		return `SOAREmail`
	case PlaybookRead:
		return `PlaybookRead`
	case PlaybookWrite:
		return `PlaybookWrite`
	case LicenseRead:
		return `LicenseRead`
	case Stats:
		return `Stats`
	case Ingest:
		return `Ingest`
	case ListUsers:
		return `ListUsers`
	case ListGroups:
		return `ListGroups`
	case ListGroupMembers:
		return `ListGroupMembers`
	case NotificationRead:
		return `NotificationRead`
	case NotificationWrite:
		return `NotificationWrite`
	case SystemInfoRead:
		return `SystemInfoRead`
	case TokenRead:
		return `TokenRead`
	case TokenWrite:
		return `TokenWrite`
	}
	return `UNKNOWN`
}

func (c *Capability) Parse(v string) (err error) {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case `search`:
		*c = Search
	case `download`:
		*c = Download
	case `savesearch`:
		*c = SaveSearch
	case `attachsearch`:
		*c = AttachSearch
	case `backgroundsearch`:
		*c = BackgroundSearch
	case `gettags`:
		*c = GetTags
	case `setsearchgroup`:
		*c = SetSearchGroup
	case `searchhistory`:
		*c = SearchHistory
	case `searchgrouphistory`:
		*c = SearchGroupHistory
	case `searchallhistory`:
		*c = SearchAllHistory
	case `dashboardread`:
		*c = DashboardRead
	case `dashboardwrite`:
		*c = DashboardWrite
	case `resourceread`:
		*c = ResourceRead
	case `resourcewrite`:
		*c = ResourceWrite
	case `templateread`:
		*c = TemplateRead
	case `templatewrite`:
		*c = TemplateWrite
	case `pivotread`:
		*c = PivotRead
	case `pivotwrite`:
		*c = PivotWrite
	case `macroread`:
		*c = MacroRead
	case `macrowrite`:
		*c = MacroWrite
	case `libraryread`:
		*c = LibraryRead
	case `librarywrite`:
		*c = LibraryWrite
	case `extractorread`:
		*c = ExtractorRead
	case `extractorwrite`:
		*c = ExtractorWrite
	case `userfileread`:
		*c = UserFileRead
	case `userfilewrite`:
		*c = UserFileWrite
	case `kitread`:
		*c = KitRead
	case `kitwrite`:
		*c = KitWrite
	case `kitbuild`:
		*c = KitBuild
	case `kitdownload`:
		*c = KitDownload
	case `scheduleread`:
		*c = ScheduleRead
	case `schedulewrite`:
		*c = ScheduleWrite
	case `soarlibs`:
		*c = SOARLibs
	case `soaremail`:
		*c = SOAREmail
	case `playbookread`:
		*c = PlaybookRead
	case `playbookwrite`:
		*c = PlaybookWrite
	case `licenseread`:
		*c = LicenseRead
	case `stats`:
		*c = Stats
	case `ingest`:
		*c = Ingest
	case `listusers`:
		*c = ListUsers
	case `listgroups`:
		*c = ListGroups
	case `listgroupmembers`:
		*c = ListGroupMembers
	case `notificationread`:
		*c = NotificationRead
	case `notificationwrite`:
		*c = NotificationWrite
	case `systeminforead`:
		*c = SystemInfoRead
	case `tokenread`:
		*c = TokenRead
	case `tokenwrite`:
		*c = TokenWrite
	default:
		err = ErrUnknownCapability
	}
	return
}

func (c Capability) String() string {
	switch c {
	case Search:
		return `Execute Search`
	case Download:
		return `Download Search`
	case AttachSearch:
		return `Attach Search`
	case SaveSearch:
		return `Save Search`
	case BackgroundSearch:
		return `Background Search`
	case GetTags:
		return `Get Tags`
	case SetSearchGroup:
		return `Set Search Group`
	case SearchHistory:
		return `Search History`
	case SearchGroupHistory:
		return `Search Group History`
	case SearchAllHistory:
		return `Search History All`
	case DashboardRead:
		return `Dashboard Read`
	case DashboardWrite:
		return `Dashboard Write`
	case ResourceRead:
		return `Resource Read`
	case ResourceWrite:
		return `Resource Write`
	case TemplateRead:
		return `Template Read`
	case TemplateWrite:
		return `Template Write`
	case PivotRead:
		return `Pivot Read`
	case PivotWrite:
		return `Pivot Write`
	case MacroRead:
		return `Macro Read`
	case MacroWrite:
		return `Macro Write`
	case LibraryRead:
		return `Search Library Read`
	case LibraryWrite:
		return `Search Library Write`
	case ExtractorRead:
		return `Extractor Read`
	case ExtractorWrite:
		return `Extractor Write`
	case UserFileRead:
		return `User File Read`
	case UserFileWrite:
		return `User File Write`
	case KitRead:
		return `Kit Read`
	case KitWrite:
		return `Kit Write`
	case KitBuild:
		return `Kit Build`
	case KitDownload:
		return `Kit Download`
	case ScheduleRead:
		return `Schedule Read`
	case ScheduleWrite:
		return `Schedule Write`
	case SOARLibs:
		return `SOAR Libraries`
	case SOAREmail:
		return `SOAR Email`
	case PlaybookRead:
		return `Playbook Read`
	case PlaybookWrite:
		return `Playbook Write`
	case LicenseRead:
		return `License Information`
	case Stats:
		return `Stats`
	case Ingest:
		return `Ingest`
	case ListUsers:
		return `List Users`
	case ListGroups:
		return `List Groups`
	case ListGroupMembers:
		return `List Group Members`
	case NotificationRead:
		return `Notification Read`
	case NotificationWrite:
		return `Notification Write`
	case SystemInfoRead:
		return `Read system info`
	case TokenRead:
		return `Read Authorization Tokens`
	case TokenWrite:
		return `Write Authorization Tokens`
	}
	return `UNKNOWN`
}

func (c Capability) Description() string {
	switch c {
	case Search:
		return `User can execute adhoc queries`
	case Download:
		return `User can download search results`
	case AttachSearch:
		return `User can attach to existing queries`
	case SaveSearch:
		return `User can save search results for later viewing`
	case BackgroundSearch:
		return `User can launch queries in the background`
	case GetTags:
		return `User can get a complete list of tags available`
	case SetSearchGroup:
		return `User can set the default search group`
	case SearchHistory:
		return `User can view their own search history`
	case SearchGroupHistory:
		return `User can view the search history for their groups`
	case SearchAllHistory:
		return `User can see the search history for all available groups`
	case DashboardRead:
		return `User can load a dashboard`
	case DashboardWrite:
		return `User can create and modify dashboards`
	case ResourceRead:
		return `User can read and list resources`
	case ResourceWrite:
		return `User can create and update resources`
	case TemplateRead:
		return `User can read and use search templates`
	case TemplateWrite:
		return `User can create and modify templates`
	case PivotRead:
		return `User can read and use search pivots`
	case PivotWrite:
		return `User can create and modify search pivots`
	case MacroRead:
		return `User can read and use macros`
	case MacroWrite:
		return `User can create and modify macros`
	case LibraryRead:
		return `User can view and execute search library queries`
	case LibraryWrite:
		return `User can create and modify search library queries`
	case ExtractorRead:
		return `User can view and use auto extractors`
	case ExtractorWrite:
		return `User can create and modify auto extractors`
	case UserFileRead:
		return `User can view user files`
	case UserFileWrite:
		return `User can create and update user files`
	case KitRead:
		return `User can view kits`
	case KitWrite:
		return `User can install, update, and remove kits`
	case KitBuild:
		return `User can build new kits`
	case KitDownload:
		return `User can download an installed or staged kit`
	case ScheduleRead:
		return `User can view scheduled queries and see results`
	case ScheduleWrite:
		return `User can create and update Scheduled Queries and SOAR scripts`
	case SOARLibs:
		return `User can import SOAR libraries`
	case SOAREmail:
		return `User can use the SOAR Email functionality`
	case PlaybookRead:
		return `User can read playbooks`
	case PlaybookWrite:
		return `User can create, update, and delete playbooks`
	case LicenseRead:
		return `User can view the license information`
	case Stats:
		return `User can view system and ingest stats`
	case Ingest:
		return `User can ingest data through webserver`
	case ListUsers:
		return `User can list all users on the system`
	case ListGroups:
		return `User can list all groups on the system`
	case ListGroupMembers:
		return `User can list the mebers of a group`
	case NotificationRead:
		return `User can read notifications`
	case NotificationWrite:
		return `User can dismiss notifications and create new ones`
	case SystemInfoRead:
		return `User can read system info about idnexers and webservers`
	case TokenRead:
		return `User can read authorization tokens`
	case TokenWrite:
		return `User can write authorization tokens`
	}
	return `UNKNOWN`
}

type CapError struct {
	Cap   Capability
	Error error
}

// The default access rule to apply to tags (false = allow).
type DefaultTagRule bool

const (
	TagDefaultAllow = false
	TagDefaultDeny  = true
)

type TagAccess struct {
	Default DefaultTagRule
	Tags    []string
}

// Check returns two values:
//	explicit: Whether or not the tag is in the TagAccess object.
//	grant: Whether or not the tag is allowed to be accessed.
func (ta TagAccess) Check(tg string) (explicit bool, grant bool) {
	explicit = ta.tagInSet(tg)
	grant = explicit == bool(ta.Default)
	return
}

func (ta TagAccess) tagInSet(tg string) bool {
	for _, v := range ta.Tags {
		if v == tg {
			return true
		}
	}
	return false
}

// Validate walks all tags in a TagAccess object, ensuring they meet all length
// and formatting rules:
//	1. Length cannot be greater than 4096 characters.
//	2. The tag cannot contain any of the following characters:
//		!@#$%^&*()=+<>,.:;`\"'{[}]|
//	3. You cannot specify more than 65536 tags.
func (ta *TagAccess) Validate() (err error) {
	otags := ta.Tags[0:0]
	tm := make(map[string]es, len(ta.Tags))
	for _, tg := range ta.Tags {
		if err = ingest.CheckTag(tg); err != nil {
			return
		}
		if _, ok := tm[tg]; !ok {
			otags = append(otags, tg)
			tm[tg] = empty
		}
	}
	if len(otags) > 0xffff {
		err = errors.New("Too many tags")
	} else {
		ta.Tags = otags
	}
	return
}

// CheckTagConflict compares two TagAccess objects for conflicting rules. It
// will return on the first conflict found and return the name of the tag.
func CheckTagConflict(a, b TagAccess) (conflict bool, tag string) {
	if a.Default == b.Default {
		//no way to conflict
		return
	}

	//they do not match, so there better not be any overlaps
	for _, at := range a.Tags {
		for _, bt := range b.Tags {
			if at == bt {
				conflict = true
				tag = at
				return
			}
		}
	}
	return
}

// CheckTagAccess returns true if the tag tg is allowed in the given TagAccess object.
func CheckTagAccess(tg string, prime TagAccess, set []TagAccess) (allowed bool) {
	var explicit bool
	//any rules applied to a user that explicitely call out a tag override all other group rules
	if explicit, allowed = prime.Check(tg); explicit {
		return
	}
	for _, g := range set {
		if explicit, allowed = g.Check(tg); explicit || !allowed {
			return
		}
	}
	return
}

// Return the set of tags permitted within a given slice of tags.
func FilterTags(tags []string, prime TagAccess, set []TagAccess) (r []string) {
	for _, t := range tags {
		if CheckTagAccess(t, prime, set) {
			r = append(r, t)
		}
	}
	return
}

func (dtr DefaultTagRule) String() string {
	if dtr == TagDefaultAllow {
		return `Default Allow`
	}
	return `Default Deny`
}

type Token struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Desc         string    `json:"description"`
	UID          int32     `json:"uid"`
	Created      time.Time `json:"createdAt"`
	Expires      time.Time `json:"expiresAt,omitempty"`
	Capabilities []string  `json:"capabilities"`
}

type TokenCreate struct {
	Name         string    `json:"name"`
	Desc         string    `json:"description"`
	Expires      time.Time `json:"expiresAt,omitempty"`
	Capabilities []string  `json:"capabilities"`
}

type TokenFull struct {
	Token
	Value string `json:"token"`
}

type TokenFullWire struct {
	TokenFull
	Caps []byte
}

func (t Token) Expired() bool {
	if t.Expires.IsZero() {
		return false
	}
	return time.Now().After(t.Expires)
}

func (t Token) ExpiresString() string {
	if t.Expires.IsZero() {
		return `NEVER`
	}
	return t.Expires.Format(time.RFC3339)
}

func (t Token) CapabilitiesString() string {
	return strings.Join(t.Capabilities, " ")
}
