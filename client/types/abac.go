/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
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
	SecretRead        Capability = 47
	SecretWrite       Capability = 48
	_maxCap           Capability = 49 //REMINDER - when adding capabilities, make sure to expand this number
)

const (
	TokenHeader string = `Gravwell-Token`
)

var (
	ErrUnknownCapability = errors.New("Unknown capability")
)

// The default access rule to apply to tags (false = allow).
type DefaultAccessRule bool

const (
	DefaultAllow = false
	DefaultDeny  = true
)

// ABACRules is the main structure that holds default stats and overrides for for API and tag access
// the Capabilities and Tags sub structures handle access independently
type ABACRules struct {
	Capabilities CapabilitySet
	Tags         TagAccess
}

// CapabilitySet is the compacted set of default values and overrides
// The CapabilitySet is translated from the CapabilityState and held internally for faster operations
type CapabilitySet struct {
	Default   DefaultAccessRule
	Overrides []byte
}

// CapabilityState is the expanded set of capabilities that is exchanged between clients the the API
// The overrides specified using the full name of a capability to make the API more explicit
type CapabilityState struct {
	Default   DefaultAccessRule
	Overrides []string
}

// CapabilityDesc is an enhanced structure containing a capability value, its name, and a brief description
type CapabilityDesc struct {
	Cap  Capability
	Name string
	Desc string
}

// CapabilityTemplate is group of capabilities with a name and description, this is used to build up a simplified set of
// macro capabilities like "can run all searches" or "can read results but not write them"
type CapabilityTemplate struct {
	Name string
	Desc string
	Caps []Capability
}

// check returns two values:
//
//	explicit: Whether or not the capability is in the CapabilitySet object.
//	grant: Whether or not the capability is allowed to be accessed.
func (cs CapabilitySet) check(c Capability) (explicit bool, grant bool) {
	explicit = cs.IsSet(c)
	grant = explicit == bool(cs.Default)
	return
}

// Has checks if a capability is allowed given the default value and overrides
func (cs CapabilitySet) Has(c Capability) bool {
	_, allow := cs.check(c)
	return allow
}

// IsSet checks if a capability override is set
func (cs CapabilitySet) IsSet(c Capability) bool {
	return CheckCapability(cs.Overrides, c)
}

// SetOverride sets an override on the capability set
func (cs *CapabilitySet) SetOverride(c Capability) (r bool) {
	if !c.Valid() {
		return
	}
	if cs.IsSet(c) {
		r = true
	} else if buff, err := AddCapability(cs.Overrides, c); err == nil {
		r = true
		cs.Overrides = buff
	}
	return
}

// ClearOverride removes a capability override from the CapabilitySet
func (cs *CapabilitySet) ClearOverride(c Capability) (r bool) {
	if !c.Valid() {
		return
	}
	if cs.IsSet(c) {
		RemoveCapability(cs.Overrides, c)
	}
	return true // it's cleared
}

// CapabilityList returns a list of capability descrptions that are in this set
func (cs *CapabilitySet) CapabilityList() (r []CapabilityDesc) {
	for _, c := range fullCapList {
		if cs.Has(c) {
			r = append(r, c.CapabilityDesc())
		}
	}
	return
}

// CapabilityDesc converts a Capability into a CapabilityDescription
func (c Capability) CapabilityDesc() CapabilityDesc {
	return CapabilityDesc{
		Cap:  c,
		Name: c.Name(),
		Desc: c.Description(),
	}
}

// Check if the capability value is valid/known
func (c Capability) Valid() bool {
	return c < _maxCap
}

// Name returns the ASCII name of a capability
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
	case SecretRead:
		return `SecretRead`
	case SecretWrite:
		return `SecretWrite`
	}
	return `UNKNOWN`
}

// Parse attempts to resolve a capability value from a name
// Parse will ignore case and trim surrounding whitespace
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
	case `secretread`:
		*c = TokenRead
	case `secretwrite`:
		*c = TokenWrite
	default:
		err = ErrUnknownCapability
	}
	return
}

// String implements the stringer interface, it does not return a parsable name but rather a shorthand description
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
		return `Read System Info`
	case TokenRead:
		return `Read Authorization Tokens`
	case TokenWrite:
		return `Write Authorization Tokens`
	case SecretRead:
		return `Read Secrets`
	case SecretWrite:
		return `Write and Delete Secrets`
	}
	return `UNKNOWN`
}

// Description returns an ASCII description of a capability value
func (c Capability) Description() string {
	switch c {
	case Search:
		return `User can execute ad-hoc queries`
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
		return `User can read and use actionables`
	case PivotWrite:
		return `User can create and modify actionables`
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
		return `User can create and update scheduled queries and SOAR scripts`
	case SOARLibs:
		return `User can import SOAR libraries`
	case SOAREmail:
		return `User can use SOAR email functions`
	case PlaybookRead:
		return `User can read playbooks`
	case PlaybookWrite:
		return `User can create, update, and delete playbooks`
	case LicenseRead:
		return `User can view license information`
	case Stats:
		return `User can view system and ingest stats`
	case Ingest:
		return `User can ingest data through webserver`
	case ListUsers:
		return `User can list all users on the system`
	case ListGroups:
		return `User can list all groups on the system`
	case ListGroupMembers:
		return `User can list the members of a group`
	case NotificationRead:
		return `User can read notifications`
	case NotificationWrite:
		return `User can dismiss notifications and create new ones`
	case SystemInfoRead:
		return `User can read system info about indexers and webservers`
	case TokenRead:
		return `User can read authorization tokens`
	case TokenWrite:
		return `User can write authorization tokens`
	case SecretRead:
		return `User can read and access secrets`
	case SecretWrite:
		return `User can create, update, and delete secrets`
	}
	return `UNKNOWN`
}

// CapError is an enhanced error that will return why an API told you know
// Typically its an error message and the capability you would need in order to use the API
type CapError struct {
	Cap   Capability
	Error error
}

// TagAccess is the structure that holds a default access to tags and a set of optional explicit overrides
// if the default rule is allow, any tag set in Overrides is disallowed
// if the default rule is deny, any tag set in the overrides is allowed
type TagAccess struct {
	Default   DefaultAccessRule
	Overrides []string //override sets an explicit allow or deny depending on Default state
}

// check returns two values:
//
//	explicit: Whether or not the tag is in the TagAccess object.
//	grant: Whether or not the tag is allowed to be accessed.
func (ta TagAccess) check(tg string) (explicit bool, grant bool) {
	explicit = ta.tagInSet(tg)
	grant = explicit == bool(ta.Default)
	return
}

func (ta TagAccess) tagInSet(tg string) bool {
	for _, v := range ta.Overrides {
		if v == tg {
			return true
		}
	}
	return false
}

// Validate walks all tags in a TagAccess object, ensuring they meet all length
// and formatting rules:
//  1. Length cannot be greater than 4096 characters.
//  2. The tag cannot contain any of the following characters:
//     !@#$%^&*()=+<>,.:;`\"'{[}]|
//  3. You cannot specify more than 65536 tags.
func (ta *TagAccess) Validate() (err error) {
	otags := ta.Overrides[0:0]
	tm := make(map[string]es, len(ta.Overrides))
	for _, tg := range ta.Overrides {
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
		ta.Overrides = otags
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
	for _, at := range a.Overrides {
		for _, bt := range b.Overrides {
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
	if explicit, allowed = prime.check(tg); explicit {
		return
	}
	for _, g := range set {
		if explicit, allowed = g.check(tg); explicit || !allowed {
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

func (ud *UserDetails) HasTagAccess(tg string) (allowed bool) {
	var explicit bool
	//any rules applied to a user that explicitely call out a tag override all other group rules
	if explicit, allowed = ud.ABAC.Tags.check(tg); explicit {
		return
	}
	//there is no explicit setting on the user, check groups
	for _, g := range ud.Groups {
		localExplicit, localAllow := g.ABAC.Tags.check(tg)
		if localExplicit {
			if !localAllow {
				//explicit deny
				allowed = false
				return
			}
			//explicit allow, toggle explicit
			explicit = true
			allowed = true
		} else if !explicit && !localAllow {
			//if we are NOT explicit AND the new rule says deny, go ahead and set to deny
			allowed = localAllow
		}
	}
	return
}

func (ud *UserDetails) FilterTags(all []string) (r []string) {
	if ud.Admin {
		return all
	}
	for _, t := range all {
		if ud.HasTagAccess(t) {
			r = append(r, t)
		}
	}
	return
}

func (dtr DefaultAccessRule) String() string {
	if dtr == DefaultAllow {
		return `Default Allow`
	}
	return `Default Deny`
}

// CheckUserCapabilityAccess checks if a user has access to a given capability based on their direct and group assignments
func CheckUserCapabilityAccess(ud *UserDetails, c Capability) (allowed bool) {
	var explicit bool
	//check if the user has an explicit deny or allow assigned directly to them
	//if so, THAT is our answer
	if explicit, allowed = ud.ABAC.Capabilities.check(c); explicit {
		return
	}

	//there is no explicit setting on the user, check groups
	for _, g := range ud.Groups {
		localExplicit, localAllow := g.ABAC.Capabilities.check(c)
		if localExplicit {
			if !localAllow {
				//explicit deny
				allowed = false
				return
			}
			//explicit allow, toggle explicit
			explicit = true
			allowed = true
		} else if !explicit && !localAllow {
			//if we are NOT explicit AND the new rule says deny, go ahead and set to deny
			allowed = localAllow
		}
	}
	return
}

// CreateUserCapabilityList creates a comprehensive list of capabilities the user has access to based on their direct and group assignments
func CreateUserCapabilityList(ud *UserDetails) (r []CapabilityDesc) {
	for _, c := range fullCapList {
		if CheckUserCapabilityAccess(ud, c) {
			r = append(r, c.CapabilityDesc())
		}
	}
	return
}

// HasCapability returns whether the user has access to a given capability
func (ud *UserDetails) HasCapability(c Capability) bool {
	return CheckUserCapabilityAccess(ud, c)
}

// CapabilityList creates a comprehensive list of capabilities the user has access to based on their direct and group assignments
func (ud *UserDetails) CapabilityList() []CapabilityDesc {
	return CreateUserCapabilityList(ud)
}

// Token is a complete API compatible token, it contains ownership information and all capabilities associated with the token
type Token struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Desc         string    `json:"description"`
	UID          int32     `json:"uid"`
	Created      time.Time `json:"createdAt"`
	Expires      time.Time `json:"expiresAt,omitempty"`
	Capabilities []string  `json:"capabilities"`
}

// TokenCreate is the structure used to ask the API to make a new token, only the request parameters are present
type TokenCreate struct {
	Name         string    `json:"name"`
	Desc         string    `json:"description"`
	Expires      time.Time `json:"expiresAt,omitempty"`
	Capabilities []string  `json:"capabilities"`
}

// TokenFull represents the response value for a token create request
// this type is the only type that contains the token value and is ONLY provided when creating a new token
type TokenFull struct {
	Token
	Value string `json:"token"`
}

// TokenFullWire is the internal type for storing token values
type TokenFullWire struct {
	TokenFull
	Caps []byte
}

// Expired returns whether a token is expired or not, if no expiration is set then the token is not expired
func (t Token) Expired() bool {
	if t.Expires.IsZero() {
		return false
	}
	return time.Now().After(t.Expires)
}

// ExpiresString returns a human friendly string of when a token expires
func (t Token) ExpiresString() string {
	if t.Expires.IsZero() {
		return `NEVER`
	}
	return t.Expires.Format(time.RFC3339)
}

// CapabilitiesString returns a human friendly space delimited list of capabilities
func (t Token) CapabilitiesString() string {
	return strings.Join(t.Capabilities, " ")
}

// Encode encodes a list of capabilities into a buffer
func EncodeCapabilities(caps []Capability) (b []byte, err error) {
	if len(caps) == 0 {
		return
	}
	//check our list
	if err = ValidateCapabilities(caps); err != nil {
		return
	}
	//sweep and calculate the buffer size to minimize allocations
	var l int
	for _, c := range caps {
		if off, _ := bitmask(c); off > l {
			l = off
		}
	}
	b = make([]byte, l+1)
	for _, c := range caps {
		if b, err = AddCapability(b, c); err != nil {
			b = nil
			return
		}
	}
	return
}

// AddCapability adds the capability c to the bitmask b
func AddCapability(b []byte, cp Capability) (r []byte, err error) {
	off, mask := bitmask(cp)
	if off >= len(b) {
		r = make([]byte, off+1)
		copy(r, b)
	} else {
		r = b
	}
	//check again for safety
	r[off] |= mask
	return
}

// RemoveCapability removes the capability c in the bitmask b
func RemoveCapability(b []byte, c Capability) (r bool) {
	if off, mask := bitmask(c); off < len(b) {
		//remove the bit
		if r = (b[off] & mask) != 0; r {
			b[off] ^= mask
		}
	}
	return
}

// CheckCapability checks if the capability c is set in the bitmask b
func CheckCapability(b []byte, c Capability) (r bool) {
	if off, mask := bitmask(c); off < len(b) {
		//remove the bit
		r = (b[off] & mask) != 0
	}
	return
}

// bitmask calculates the offset and mask required to encode into a buffer
func bitmask(c Capability) (offset int, mask byte) {
	offset = (int(c) / 8)
	mask = byte(1 << (int(c) % 8))
	return
}

// HasCapability checks if a given ABACRules set has a capability
func (abr *ABACRules) HasCapability(c Capability) (allowed bool) {
	allowed = abr.Capabilities.Default == DefaultAllow
	if abr.Capabilities.IsSet(c) {
		//explicit override, thats the answer
		allowed = !allowed
	}
	return
}

// CapabilityList returns a comprehensive set of capability descriptions that the given ruleset has access to
func (abr *ABACRules) CapabilityList() (r []CapabilityDesc) {
	for _, c := range fullCapList {
		if abr.HasCapability(c) {
			r = append(r, c.CapabilityDesc())
		}
	}
	return
}

// export a CapabilityState from the underlying capability rules
func (abr *ABACRules) CapabilityState() (r CapabilityState) {
	r.Default = abr.Capabilities.Default
	for _, c := range fullCapList {
		if abr.Capabilities.IsSet(c) {
			r.Overrides = append(r.Overrides, c.Name())
		}
	}
	return
}

// CapabilitySet converts the human friendly CapabilityState into an optimized and encoded CapabilitySet for internal use
func (st CapabilityState) CapabilitySet() (cs CapabilitySet, err error) {
	var c Capability
	cs.Default = st.Default
	for _, s := range st.Overrides {
		if err = c.Parse(s); err != nil {
			return
		}
		cs.SetOverride(c)
	}
	return
}

// CapabilityList returns a list of capability descriptions that this capability state has access to
func (st CapabilityState) CapabilityList() (lst []CapabilityDesc, err error) {
	var cs CapabilitySet
	if cs, err = st.CapabilitySet(); err == nil {
		lst = cs.CapabilityList()
	}
	return
}

// CapabilityState takes a capability template and converts it into a capability set that can be sent to the API
// This defaults to a state with default deny and explicit allow
func (ct CapabilityTemplate) CapabilityState() (s CapabilityState) {
	s.Default = DefaultDeny
	s.Overrides = make([]string, 0, len(ct.Caps))
	for _, c := range ct.Caps {
		s.Overrides = append(s.Overrides, c.Name())
	}
	return
}
