/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package types implements the concrete data types for the Gravwell API
package types

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	// Equal major versions should always be compatible
	API_VERSION_MAJOR uint32 = 1
	// Minor versions define features sets, but have no bearing on compatibility.
	API_VERSION_MINOR uint32 = 0

	AUTH_TYPE_NONE     AuthType = `None` // for when you don't have MFA set up at all yet.
	AUTH_TYPE_TOTP     AuthType = `TOTP`
	AUTH_TYPE_RECOVERY AuthType = `RecoveryCodes`
)

// Helpers for the marshaling functions
var (
	emptyList   = []byte(`[]`)
	emptyObj    = []byte(`{}`)
	emptyRawObj = RawObject(`{}`)
	emptyString = []byte(`""`)
	jsonNull    = []byte(`null`)
)

type AuthType string

type RawObject json.RawMessage

type es struct{}

var empty es

// ErrorObject is a basic error object with the error value and an optional
// info structure that has more info about the error
type ErrorObject struct {
	Err  string
	Info string `json:",omitempty"`
}

// BaseListResponse contains the common set of fields returned when
// querying lists of assets.
//
// NOTE: BaseListResponse intentionally has no MarshalJSON of its own, for
// the same reason CommonFields doesn't (see CommonFields.MakeNilSlices()).
type BaseListResponse struct {
	CursorNext       string
	CursorPrev       string
	Offset           int
	TotalCount       int
	Type             string
	AvailableFilters []AvailableFilter
}

// MakeNilSlices returns a copy of b with nil slice field (currently just Labels)
// replaced by an empty, non-nil slice s.t. it marshals as "[]".
func (b BaseListResponse) MakeNilSlices() BaseListResponse {
	b.AvailableFilters = nonNilSlice(b.AvailableFilters)
	return b
}

type VersionInfo struct {
	API   ApiInfo
	Build BuildInfo
}

type ApiInfo struct {
	Major uint32
	Minor uint32
}

type BuildInfo struct {
	CanonicalVersion
	BuildDate  time.Time
	BuildID    string `json:",omitempty"`
	GUIBuildID string `json:",omitempty"`
}

type CanonicalVersion struct {
	Major uint32
	Minor uint32
	Point uint32
}

// ErrVersionMismatch returns an error stating that the local client and the remote server are running different major API versions and thus
// are not compatible.
type ErrVersionMismatch struct {
	Local  ApiInfo
	Remote ApiInfo
}

func (e ErrVersionMismatch) Error() string {
	return fmt.Sprintf("Version mismatch!\nLocal: %d.%d\nRemote %d.%d\n",
		e.Local.Major, e.Local.Minor, e.Remote.Major, e.Remote.Minor)
}

// Is tests only that the error is a VersionMismatchError without any concern for the numbers themselves.
func (ErrVersionMismatch) Is(target error) bool {
	switch target.(type) {
	case ErrVersionMismatch, *ErrVersionMismatch:
		return true
	default:
		return false
	}
}

func ApiVersion() ApiInfo {
	return ApiInfo{
		Major: API_VERSION_MAJOR,
		Minor: API_VERSION_MINOR,
	}
}

// Version returns the full build version of Gravwell eg "4.1.2"
func (bi BuildInfo) Version() string {
	return bi.CanonicalVersion.String()
}

func (bi BuildInfo) String() string {
	return fmt.Sprintf("%s (%s) %s [GUI: %s]",
		bi.CanonicalVersion.String(),
		bi.BuildDate.Format(`2006-01-02`), bi.BuildID, bi.GUIBuildID)
}

func (bi BuildInfo) NewerVersion(nbi BuildInfo) bool {
	return bi.CanonicalVersion.NewerVersion(nbi.CanonicalVersion)
}

// CheckApiVersion returns an error iff the remote's major version != the caller's major version.
func CheckApiVersion(remote ApiInfo) error {
	local := ApiVersion()
	if local.Major == remote.Major {
		return nil //we match
	}
	return ErrVersionMismatch{Local: local, Remote: remote}

}

func parseUint32(v string) (r uint32, err error) {
	var x uint64
	if x, err = strconv.ParseUint(v, 10, 32); err == nil {
		r = uint32(x)
	}
	return
}

// ParseCanonicalVersion validates and parses a version string
// it returns a CanonicalVersion object containing the given version string.
// Must be in the form of "X.Y.Z".
func ParseCanonicalVersion(s string) (r CanonicalVersion, err error) {
	var bits []string
	if s = strings.TrimSpace(s); len(s) == 0 {
		//return, this is the zero value
		return
	} else if bits = strings.Split(s, `.`); len(bits) != 3 {
		err = errors.New("Malformed version string")
		return
	}
	//at this point we know we have 3 bits
	if r.Major, err = parseUint32(bits[0]); err == nil {
		if r.Minor, err = parseUint32(bits[1]); err == nil {
			r.Point, err = parseUint32(bits[2])
		}
	}
	return
}

// NewerVersion returns true if the argument is newer than the receiver.
func (cv CanonicalVersion) NewerVersion(ncv CanonicalVersion) bool {
	return cv.Compare(ncv) > 0
}

// Compare returns the following:
//
//	0	- equal versions
//	<0	- incoming is older than existing
//	>0	- incoming is newer then existing
func (cv CanonicalVersion) Compare(ncv CanonicalVersion) int {
	if ncv.Major > cv.Major {
		return 1 //incoming newer
	} else if cv.Major > ncv.Major {
		return -1 //incoming older
	}
	//same major
	if ncv.Minor > cv.Minor {
		return 1 //incoming newer
	} else if cv.Minor > ncv.Minor {
		return -1 //incoming older
	}

	//same major and minor
	if ncv.Point > cv.Point {
		return 1 //incoming newer
	} else if cv.Point > ncv.Point {
		return -1 //incoming older
	}
	return 0 // same version
}

func (cv CanonicalVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", cv.Major, cv.Minor, cv.Point)
}

func (cv CanonicalVersion) Enabled() bool {
	return cv.Major > 0 || cv.Minor > 0 || cv.Point > 0
}

func (cv CanonicalVersion) Compatible(min, max CanonicalVersion) bool {
	if !cv.Enabled() {
		//if we are zero, then we are always compatible
		return true
	}
	//check if we are too low
	if min.Enabled() && cv.Compare(min) > 0 {
		return false // we are too old
	}
	if max.Enabled() && cv.Compare(max) < 0 {
		return false // we are too new
	}
	return true // all good!
}

type LoginRequest struct {
	User string
	Pass string
}

type LoginResponse struct {
	LoginStatus       bool
	Reason            string     `json:",omitempty"`
	AcceptedAuthTypes []AuthType `json:",omitempty"`
	Admin             bool       `json:",omitempty"`
	JWT               string     `json:",omitempty"`
	MFARequired       bool
	MFASetupRequired  bool
}

type MFAAuthRequest struct {
	AuthCode string
	AuthType AuthType
	Pass     string
	User     string
}

// MFATOTPSetupResponse is returned by the webserver when the
// user requests parameters to configure TOTP.
type MFATOTPSetupResponse struct {
	QRCode []byte // PNG-encoded image
	Seed   string // The secret key/seed
	URL    string // OTP URL
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (m MFATOTPSetupResponse) MarshalJSON() ([]byte, error) {
	type dummyMFATOTPSetupResponse MFATOTPSetupResponse
	m.QRCode = nonNilSlice(m.QRCode)
	return json.Marshal(dummyMFATOTPSetupResponse(m))
}

// MFATOTPInstallResponse is returned when the user has
// successfully configured TOTP on the webserver.
type MFATOTPInstallResponse struct {
	LoginResponse
	RecoveryCodes []string
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (m MFATOTPInstallResponse) MarshalJSON() ([]byte, error) {
	type dummyMFATOTPInstallResponse MFATOTPInstallResponse
	m.RecoveryCodes = nonNilSlice(m.RecoveryCodes)
	return json.Marshal(dummyMFATOTPInstallResponse(m))
}

type SSOStatus struct {
	Enabled bool
}

type WarnResp struct {
	Name string
	Err  error `json:",omitempty"`
}

type IngestResponse struct {
	Count int64
	Size  int64
	Tags  []string
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (i IngestResponse) MarshalJSON() ([]byte, error) {
	type dummyIngestResponse IngestResponse
	i.Tags = nonNilSlice(i.Tags)
	return json.Marshal(dummyIngestResponse(i))
}

func (wr WarnResp) MarshalJSON() ([]byte, error) {
	var s string
	if wr.Err != nil {
		s = wr.Err.Error()
	}
	return json.Marshal(&struct {
		Name string
		Err  string
	}{
		Name: wr.Name,
		Err:  s,
	})
}

func (wr *WarnResp) UnmarshalJSON(buff []byte) error {
	type alias struct {
		Name string
		Err  string `json:",omitempty"`
	}
	var a alias
	if err := json.Unmarshal(buff, &a); err != nil {
		return err
	}
	wr.Name = a.Name
	if len(a.Err) > 0 {
		wr.Err = errors.New(a.Err)
	}
	return nil
}

type GUISettings struct {
	DistributedWebservers bool
	DisableMapTileProxy   bool
	MapTileUrl            string

	// If true, the UI shouldn't display any notifications about new features
	DisableFeaturePopups bool

	// Indicates that we're in cloud mode - changes some behaviors
	CloudMode bool

	ServerTime           time.Time
	ServerTimezone       string
	ServerTimezoneOffset int

	MaxFileSize        uint64 // the maximum size allowed for user file uploads
	MaxResourceSize    uint64 // the largest resource you're allowed to make
	MaxJsonRequestSize uint64 // the largest object you're allowed to send in a JSON request body

	IngestAllowed bool // set to true if the user is allowed to use the ingest APIs
	NonCommercial bool // set to true if the license is a non-commercial license
}

type SearchAgentConfig struct {
	Searchagent_UUID                 string
	Webserver_Address                []string
	Insecure_Skip_TLS_Verify         bool
	Insecure_Use_HTTP                bool
	Search_Agent_Auth                string
	Scratch_Path                     string
	Max_Script_Run_Time              int64 // minutes!
	Log_File                         string
	Log_UDP_Target                   string
	Log_Level                        string
	Disable_Network_Script_Functions bool // disables "risky" scripting functions (network stuff)
	Disable_Self_Ingest              bool // disables ingesting search agent logs to indexers
	HTTP_Proxy                       string

	Search_Rate  int64 // searches launched per second
	Search_Burst int64 // allows some burst
	Script_Rate  int64
	Script_Burst int64
	Flow_Rate    int64
	Flow_Burst   int64
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (sac SearchAgentConfig) MarshalJSON() ([]byte, error) {
	type dummySearchAgentConfig SearchAgentConfig
	sac.Webserver_Address = nonNilSlice(sac.Webserver_Address)
	return json.Marshal(dummySearchAgentConfig(sac))
}

type emptyStrings []string

func (es emptyStrings) MarshalJSON() ([]byte, error) {
	if len(es) == 0 {
		return emptyList, nil
	}
	return json.Marshal([]string(es))
}

func (o RawObject) MarshalJSON() ([]byte, error) {
	if len(o) == 0 || o == nil {
		return emptyObj, nil
	}
	b := json.RawMessage(o)
	return json.Marshal(&b)
}

func (o *RawObject) UnmarshalJSON(buff []byte) error {
	var b json.RawMessage
	if err := json.Unmarshal(buff, &b); err != nil {
		return err
	}
	*o = RawObject(b)
	return nil
}

func (o RawObject) String() string {
	return string(o)
}

type LoggingLevels struct {
	Levels  []string
	Current string
}

type LogLevel struct {
	Level string
}

// MarshalJSON ensures slices and maps marshal as "[]"/"{}" instead of "null".
func (m LoggingLevels) MarshalJSON() ([]byte, error) {
	type dummyLoggingLevels LoggingLevels
	m.Levels = nonNilSlice(m.Levels)
	return json.Marshal(dummyLoggingLevels(m))
}
