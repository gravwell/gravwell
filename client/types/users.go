/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"net"
	"time"

	"crypto/rand"
	"fmt"
	"github.com/google/uuid"
)

const (
	NotificationLevelInfo     string = `info`
	NotificationLevelWarn     string = `warn`
	NotificationLevelError    string = `error`
	NotificationLevelCritical string = `critical`
)

var (
	//essentially a never expires
	NeverExpires = time.Date(2099, 12, 31, 12, 0, 0, 0, time.UTC)
)

type TokenSigningKey struct {
	Key        []byte
	Expiration time.Time
}

type UserBackup struct {
	Groups []GroupDetails
	Users  []UserDetails
}

func (ub *UserBackup) ClearSynced() {
	for i := range ub.Groups {
		ub.Groups[i].Synced = false
	}
	for i := range ub.Users {
		ub.Users[i].Synced = false
		//wipe the groups as well just in case
		for j := range ub.Users[i].Groups {
			ub.Users[i].Groups[j].Synced = false
		}
	}
}

// AuthSession contains all the information needed to authenticate.
type Session struct {
	ID          uint64 `json:",omitempty"`
	JWT         string `json:",omitempty"`
	UID         int32  `json:",omitempty"`
	Origin      net.IP
	LastHit     time.Time
	UDets       *UserDetails `json:",omitempty"`
	TempSession bool
	Synced      bool
}

func (s *Session) Encode() ([]byte, error) {
	bb := bytes.NewBuffer(nil)
	if err := gob.NewEncoder(bb).Encode(s); err != nil {
		return nil, err
	}
	return bb.Bytes(), nil
}

func DecodeSession(b []byte) (*Session, error) {
	var s Session
	bb := bytes.NewBuffer(b)
	if err := gob.NewDecoder(bb).Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

type UserPreference struct {
	UID     int32
	Name    string
	Updated time.Time
	Data    []byte
	Synced  bool
}
type UserPreferences []UserPreference

type UserSessions struct {
	UID      int32
	User     string
	Sessions []Session
}

type UserDetails struct {
	UID        int32
	User       string
	Name       string
	Email      string
	Admin      bool
	Locked     bool
	TS         time.Time `json:",omitempty"`
	DefaultGID int32     `json:",omitempty"`
	Groups     []GroupDetails
	MFA        MFAUserConfig
	Hash       []byte `json:"-"` //do not include in API responses
	Synced     bool
	CBAC       CBACRules `json:"-"` //do not include in API responses
	SSOUser    bool      // set true if user is managed via SSO
}

type MFAUserConfig struct {
	TOTP          TOTPUserConfig
	RecoveryCodes RecoveryCodes
}

// MFAEnabled returns true if *any* MFA option is configured
func (c *MFAUserConfig) MFAEnabled() bool {
	return len(c.MFATypesEnabled()) > 0
}

// MFATypesEnabled gives a list of the types of MFA the user has set up.
func (c *MFAUserConfig) MFATypesEnabled() (r []AuthType) {
	if c.TOTP.Enabled {
		r = append(r, AUTH_TYPE_TOTP)
	}
	if c.RecoveryCodes.Enabled {
		r = append(r, AUTH_TYPE_RECOVERY)
	}
	return
}

// ClearSecrets blanks out any sensitive stuff within the config.
// Call this if there's any concern over where the object will end up.
func (c *MFAUserConfig) ClearSecrets() {
	c.TOTP.URL = ""
	c.TOTP.Seed = ""
	c.RecoveryCodes.Codes = []string{}
}

type TOTPUserConfig struct {
	Enabled bool
	URL     string `json:"-"` // A TOTP URL contains all details in one place
	Seed    string `json:"-"` // The secret key
}

type RecoveryCodes struct {
	Enabled   bool
	Codes     []string `json:"-"`
	Remaining int      // how many codes are left
	Generated time.Time
}

// MFAInfo describes system-wide MFA policies as well as the user's
// own MFA configuration.
type MFAInfo struct {
	UserConfig  MFAUserConfig
	MFARequired bool // If true, system requires MFA
}

func GenerateRecoveryCodes(count int) (RecoveryCodes, error) {
	r := RecoveryCodes{
		Enabled:   true,
		Remaining: count,
		Generated: time.Now(),
	}
	for i := 0; i < count; i++ {
		b := make([]byte, 6)
		if _, err := rand.Read(b); err != nil {
			return r, err
		}
		r.Codes = append(r.Codes, fmt.Sprintf("%x", b))
	}
	return r, nil
}

type GroupDetails struct {
	GID    int32
	Name   string
	Desc   string
	Synced bool
	CBAC   CBACRules `json:"-"` //do not include in API responses
}

type AddUser struct {
	User  string
	Pass  string
	Name  string
	Email string
	Admin bool
}

type AddGroup struct {
	Name string
	Desc string
}

type UpdateUser struct {
	User   string
	Name   string
	Email  string
	Admin  bool
	Locked bool
}

type UserAddGroups struct {
	GIDs []int32
}

type UserDefaultSearchGroup struct {
	GID int32
}

type AdminActionResp struct {
	UID   int32 `json:",omitempty"`
	Admin bool  `json:",omitempty"`
}

type RenderModuleInfo struct {
	Name        string
	Description string
	Examples    []string
}

type RespError struct {
	Error string `json:",omitempty"`
}

type SearchModuleInfo struct {
	Name         string
	Info         string
	Examples     []string
	Collapsing   func(string) bool `json:"-"`
	FrontendOnly bool              // true if this module MUST run on frontend (anko)
	Sorting      bool
}

type ChangePassword struct {
	OrigPass string
	NewPass  string
}

type Notification struct {
	UID         int32
	GID         int32
	Sender      int32     //who sent it
	Type        uint32    //ID which specifies the type of notification
	Broadcast   bool      //was this a broadcast to multiple users
	Sent        time.Time //when was it sent
	Expires     time.Time //when does it expire
	IgnoreUntil time.Time //Don't display until after this time
	Msg         string
	Origin      uuid.UUID // which device sent it (currently only used on indexers)
	Level       string    `json:",omitempty"` //generic keyword indicating how bad this notification is
	Link        string    `json:",omitempty"`
}

func (n *Notification) Expired() bool {
	if n.Expires.Before(time.Now()) {
		return true
	}
	return false
}

type BackendNotification struct {
	Notification
	Action NotificationAction
	GUID   uuid.UUID
}

type NotificationAction uint32

var (
	SetBackendNotification   NotificationAction = 0
	ClearBackendNotification NotificationAction = 1
)

// structure for license updates and warnings
type LicenseUpdateError struct {
	Name string
	Err  string
}

type NotificationSet map[uint64]Notification

// CanRead returns true if the user is allowed to read something
// with the specified UID and GID ownerships, taking into account
// the Admin flag on the user.
func (ud *UserDetails) CanRead(uid int32, gids []int32) bool {
	if ud.Admin {
		return true
	}
	return ud.UserCanRead(uid, gids)
}

// UserCanRead returns true if the user is allowed to read something
// without respect to the admin, basically if owner or in groups
func (ud *UserDetails) UserCanRead(uid int32, gids []int32) bool {
	if ud.UID == uid {
		return true
	}
	for _, group := range gids {
		if ud.InGroup(group) {
			return true
		}
	}
	return false
}

// CanModify returns true if the user is allowed to modify
// or delete something with the specified UID ownership
func (ud *UserDetails) CanModify(uid int32) bool {
	if ud.Admin || ud.UID == uid {
		return true
	}
	return false
}

func (ud *UserDetails) GIDs() []int32 {
	gids := make([]int32, len(ud.Groups))
	for i := range ud.Groups {
		gids[i] = ud.Groups[i].GID
	}
	return gids
}

func (ud *UserDetails) InGroup(gid int32) bool {
	for i := range ud.Groups {
		if ud.Groups[i].GID == gid {
			return true
		}
	}
	return false
}

func (ud *UserDetails) InAllGroups(gids []int32) bool {
	for _, gid := range gids {
		if !ud.InGroup(gid) {
			return false
		}
	}
	return true
}

func (ud *UserDetails) GroupNames() (gps []string) {
	for i := range ud.Groups {
		gps = append(gps, ud.Groups[i].Name)
	}
	return
}

func (ud *UserDetails) GroupTagAccess() (r []TagAccess) {
	for i := range ud.Groups {
		r = append(r, ud.Groups[i].CBAC.Tags)
	}
	return
}

// ClearSecrets blanks out any sensitive stuff within the struct.
// Call this if there's any concern over where the object will end up.
func (ud *UserDetails) ClearSecrets() {
	ud.Hash = []byte{}
	ud.MFA.ClearSecrets()
}

func (ups UserPreferences) MarshalJSON() ([]byte, error) {
	if len(ups) == 0 {
		return emptyList, nil
	}
	return json.Marshal([]UserPreference(ups))
}

// marshaller hacks to get it to return [] on empty lists
func (u UserDetails) MarshalJSON() ([]byte, error) {
	type alias UserDetails
	return json.Marshal(&struct {
		alias
		Groups groupsAlias
	}{
		alias:  alias(u),
		Groups: groupsAlias(u.Groups),
	})
}

type groupsAlias []GroupDetails

func (ga groupsAlias) MarshalJSON() ([]byte, error) {
	if len(ga) == 0 {
		return emptyList, nil
	}
	//this will cause an infinite recursion if we don't change the type
	return json.Marshal([]GroupDetails(ga))
}

func (s *UserSessions) MarshalJSON() ([]byte, error) {
	type alias UserSessions
	return json.Marshal(&struct {
		alias
		Sessions sessions
	}{
		alias:    alias(*s),
		Sessions: sessions(s.Sessions),
	})
}

type sessions []Session

func (s sessions) MarshalJSON() ([]byte, error) {
	if len(s) == 0 {
		return emptyList, nil
	}
	return json.Marshal([]Session(s))
}

func (uag *UserAddGroups) MarshalJSON() ([]byte, error) {
	type alias UserAddGroups
	return json.Marshal(&struct {
		alias
		GIDs emptyInts
	}{
		alias: alias(*uag),
		GIDs:  emptyInts(uag.GIDs),
	})
}
