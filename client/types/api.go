/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

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
	//MAJOR API VERSIONS should always be compatible, there just may be
	//additional features
	API_VERSION_MAJOR uint32 = 0
	API_VERSION_MINOR uint32 = 1
)

// Helpers for the marshaling functions
var (
	emptyList   = []byte(`[]`)
	emptyObj    = []byte(`{}`)
	emptyRawObj = RawObject(`{}`)
	emptyString = []byte(`""`)
	jsonNull    = []byte(`null`)
)

type RawObject json.RawMessage

type es struct{}

var empty es

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
	BuildDate  time.Time `json:",omitempty"`
	BuildID    string    `json:",omitempty"`
	GUIBuildID string    `json:",omitempty"`
}

// The full version of Gravwell in this build - eg 4.1.2
type CanonicalVersion struct {
	Major uint32
	Minor uint32
	Point uint32
}

func ApiVersion() ApiInfo {
	return ApiInfo{
		Major: API_VERSION_MAJOR,
		Minor: API_VERSION_MINOR,
	}
}

// Return the full build version of Gravwell eg "4.1.2"
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

func CheckApiVersion(remote ApiInfo) error {
	local := ApiVersion()
	if local.Major == remote.Major {
		return nil //we match
	}
	return fmt.Errorf("Version mismatch!\nLocal: %d.%d\nRemote %d.%d\n",
		local.Major, local.Minor, remote.Major, remote.Minor)

}

func parseUint32(v string) (r uint32, err error) {
	var x uint64
	if x, err = strconv.ParseUint(v, 10, 32); err == nil {
		r = uint32(x)
	}
	return
}

// Return a CanonicalVersion object containing the given version string. Must
// be in the form of "X.Y.Z".
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

// NewerVersion returns true if the incoming version is newer than coming
func (cv CanonicalVersion) NewerVersion(ncv CanonicalVersion) bool {
	return cv.Compare(ncv) > 0
}

// Compare returns the following:
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
	LoginStatus bool
	Reason      string `json:",omitempty"`
	Admin       bool   `json:",omitempty"`
	JWT         string `json:",omitempty"`
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

	ServerTime           time.Time
	ServerTimezone       string
	ServerTimezoneOffset int

	MaxFileSize     uint64 // the maximum size allowed for user file uploads
	MaxResourceSize uint64 // the largest resource you're allowed to make

	IngestAllowed bool // set to true if the user is allowed to use the ingest APIs
}

type SearchAgentConfig struct {
	Webserver_Address                []string
	Insecure_Skip_TLS_Verify         bool
	Insecure_Use_HTTP                bool
	Search_Agent_Auth                string
	Scratch_Path                     string
	Max_Script_Run_Time              int64 // minutes!
	Log_File                         string
	Log_Level                        string
	Disable_Network_Script_Functions bool // disables "risky" scripting functions (network stuff)
	HTTP_Proxy                       string
}

type emptyInts []int32

func (ei emptyInts) MarshalJSON() ([]byte, error) {
	if len(ei) == 0 {
		return emptyList, nil
	}
	return json.Marshal([]int32(ei))
}

type emptyStrings []string

func (es emptyStrings) MarshalJSON() ([]byte, error) {
	if len(es) == 0 {
		return emptyList, nil
	}
	return json.Marshal([]string(es))
}

type emptyByteArrays [][]byte

func (eba emptyByteArrays) MarshalJSON() ([]byte, error) {
	if len(eba) == 0 {
		return emptyList, nil
	}
	return json.Marshal([][]byte(eba))
}

type emptyInt64s []int64

func (ei emptyInt64s) MarshalJSON() ([]byte, error) {
	if len(ei) == 0 {
		return emptyList, nil
	}
	return json.Marshal([]int64(ei))
}

type emptyFloat64s []float64

func (ei emptyFloat64s) MarshalJSON() ([]byte, error) {
	if len(ei) == 0 {
		return emptyList, nil
	}
	return json.Marshal([]float64(ei))
}

//marshalling and handlers for our raw object type
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

func (m *LoggingLevels) MarshalJSON() ([]byte, error) {
	type alias LoggingLevels
	return json.Marshal(&struct {
		alias
		Levels emptyStrings
	}{
		alias:  alias(*m),
		Levels: emptyStrings(m.Levels),
	})
}
