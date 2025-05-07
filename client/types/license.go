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
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gravwell/gravwell/v3/ingest/entry"
)

const (
	// license type magic numbers
	Free       LicenseType = 0x2518766a3f4f7bca // minimal ingest, single instance, limited features, completely free
	Eval       LicenseType = 0xb7c489d229961f64 // single instance (backend and frontend must be on the same machine) but we throw a bunch of stuff up in the GUI
	Community  LicenseType = 0xa332f9b1f64789d2 // single instance, limited ingest per day
	Fractional LicenseType = 0xe5354cae719162c3 // single instance, full features, limited ingest per day
	Single     LicenseType = 0x6f848b5ce61db26a //single instance (backend and frontend must be on the same machine)
	Enterprise LicenseType = 0x6e67a154aa1d503e //single instance, but all features allowed
	Cluster    LicenseType = 0x16e6aac870ea32ee //MxN configuration (many headends, restricted backends)
	Unlimited  LicenseType = 0x387dd2c0faa6e1e3 //MxN configuration (many headends, many backends)
	Cloud      LicenseType = 0xa51975a973c6340f //Hosted cloud systems where number of nodes doesn't matter and ingest is tracked

	// feature override bitmasks
	Replication     FeatureOverride = 1
	SingleSignon    FeatureOverride = 1 << 1
	Overwatch       FeatureOverride = 1 << 2
	NoStats         FeatureOverride = 1 << 3
	UnlimitedCPU    FeatureOverride = 1 << 4
	CBAC            FeatureOverride = 1 << 5
	UnlimitedIngest FeatureOverride = 1 << 6
	LogbotLLM       FeatureOverride = 1 << 7

	ReplicationName     string = `replication`
	SingleSignonName    string = `sso`
	OverwatchName       string = `overwatch`
	NoStatsName         string = `nostats`
	UnlimitedCPUName    string = `unlimitedcpu`
	CBACName            string = `abac`
	UnlimitedIngestName string = `unlimitedingest`
	LogbotLLMName       string = `logbotllm`

	// ingest rate constants
	gb = 1024 * 1024 * 1024
)

var (
	ErrNoMetadata          = errors.New("No metadata available")
	ErrIngestNotRestricted = errors.New("Ingest is not restricted")
)

var (
	Overrides = []FeatureOverride{
		Replication,
		SingleSignon,
		Overwatch,
		NoStats,
		UnlimitedCPU,
		UnlimitedIngest,
		CBAC,
		LogbotLLM,
	}

	OverrideNames = []string{
		ReplicationName,
		SingleSignonName,
		OverwatchName,
		NoStatsName,
		UnlimitedCPUName,
		UnlimitedIngestName,
		CBACName,
		LogbotLLMName,
	}
)

type LicenseType uint64
type FeatureOverride uint64

// A LicenseInfo block represents the overall configuration for a license - the
// type, customer information, expiration, etc.
type LicenseInfo struct {
	Version        uint64
	CustomerUUID   string `json:",omitempty"`
	CustomerNumber uint64
	Expiration     entry.Timestamp
	Type           LicenseType
	//MaxNodes is either maximum machines for cluster type, or sockets for single type
	MaxNodes  uint32
	Overrides FeatureOverride
	Metadata  []byte
	NFR       bool //non-commercial license override
	Hash      []byte
}

// Features is a list of features present on this license. It's used in the
// /api/license path to report what features are available (but not necessarily
// in use).
type Features struct {
	Replication     bool
	SingleSignon    bool
	Overwatch       bool
	NoStats         bool
	UnlimitedCPU    bool
	CBAC            bool
	UnlimitedIngest bool
	LogbotLLM       bool
}

type LicenseIndexerStatus struct {
	Indexer  string `json:"indexer"`
	Serviced bool   `json:"ready"`
	Error    string `json:"error,omitempty"`
}

type LicenseDistributionStatus struct {
	Status string                 `json:"status"`
	States []LicenseIndexerStatus `json:"states,omitempty"`

	// information about whether the system is allowed to run in unlicensed/free mode
	UnlicensedAllowed bool `json:"unlicensed_allowed"`

	// if system cannot run in unlicensed mode, a list of reasons will be provided
	// they may be things like "system is configured as a cluster" or "CBAC is enabled"
	DisallowUnlicensedReasons []string `json:"disallow_unlicensed_reasons,omitempty"`
}

type LicenseIndexerInfo struct {
	Indexer string      `json:"indexer"`
	Error   error       `json:"error,omitempty"`
	Info    LicenseInfo `json:"info,omitempty"`
}

// LicenseUsageBucket is a time bucket of license quota activity
// A typical license tracks a 24 hour rolling window with 1 hour buckets
// Unlimited licenses do not track ingest at all
type LicenseUsageBucket struct {
	Start time.Time //start of this bucket
	End   time.Time //end of this bucket
	Size  uint64    //ingest bucket size
	Count uint64    //ingest bucket count
}

// LicenseUsage is the data structure that is handed back to indicate how much of a license quota is used
// and what the usage looks like over the rolling windows.
// Unlimited licenses will return Unlimited = true with everything else empty
type LicenseUsage struct {
	Unlimited bool                 // license is unlimited, nothing else will be here
	Quota     uint64               // license ingest limitation
	Used      uint64               // license ingest usage
	Entries   uint64               // total count of entries (does not impact license)
	History   []LicenseUsageBucket `json:",omitempty"`
	Error     error                `json:",omitempty"`
}

// LicenseUsageReport is the meta structure that contains all the license tracking data for potentially many indexers
// The typical use cases are a single cluster with unlimited ingest, a single indexer with unlimited ingest, or a single indexer with limited ingest
// however, overwatch topologies may have mixed licensing across the indexers
type LicenseUsageReport struct {
	Unlimited bool                    //every single reporting indexer has unlimited ingest OR an error, nothing to report
	Indexers  map[string]LicenseUsage `json:",omitempty"` // if all indexers are unlimited this won't bbe included at all
}

// Validate ensures the license info is valid.
func (li LicenseInfo) Validate() error {
	if li.Version == 0 || li.Version > 0x100 {
		return errors.New("Invalid version")
	}
	if li.CustomerNumber == 0 {
		return errors.New("Invalid customer number")
	}
	if _, err := uuid.Parse(li.CustomerUUID); err != nil {
		return errors.New("Bad UUID " + err.Error())
	}
	if !li.Type.Valid() {
		return errors.New("Invalid license type")
	}
	return nil
}

// Serial number is a hex string composed of the following
// <cust number>-<version>-<license type><max nodes>-<expiration>
func (li LicenseInfo) Serial() string {
	maxnodes := fmt.Sprintf("%d", li.MaxNodes)
	if li.Type == Unlimited {
		maxnodes = `X`
	}
	return fmt.Sprintf("%x-%d-%s%s-%x", li.CustomerNumber, li.Version, li.Type.Abbr(), maxnodes, li.Expiration.Sec)
}

// SKU is <version><license type><max nodes>
func (li LicenseInfo) SKU() string {
	maxnodes := fmt.Sprintf("%d", li.MaxNodes)
	if li.Type == Unlimited {
		maxnodes = `X`
	}
	overrides := ``
	if li.Overrides != 0 {
		overrides = fmt.Sprintf("x%x", uint64(li.Overrides))
	}
	return fmt.Sprintf("%d%s%s%s", li.Version, li.Type.Abbr(), maxnodes, overrides)
}

func (li LicenseInfo) OverwatchEnabled() bool {
	return li.Overrides.Set(Overwatch)
}

func (li LicenseInfo) NoStatsEnabled() bool {
	if li.Type.AllFeatures() {
		return true
	}
	return li.Overrides.Set(NoStats)
}

func (li LicenseInfo) UnlimitedCPUEnabled() bool {
	if li.Type.AllFeatures() {
		return true
	}
	return li.Overrides.Set(UnlimitedCPU)
}

func (li LicenseInfo) UnlimitedIngestEnabled() bool {
	if li.Overrides.Set(UnlimitedIngest) {
		return true
	}

	return li.Type != Fractional && li.Type != Community && li.Type != Free
}

func (li LicenseInfo) SSOEnabled() bool {
	return true // everyone gets this now
}

func (li LicenseInfo) LogbotLLMEnabled() bool {
	if li.Type.AllFeatures() || li.Overrides.Set(LogbotLLM) {
		return true
	}
	return false
}

func (li LicenseInfo) ReplicationEnabled() bool {
	switch li.Type {
	case Unlimited:
		return true
	case Enterprise:
		return true
	case Cluster:
		return true
	case Cloud:
		return true
	}
	return li.Overrides.Set(Replication)
}

func (li LicenseInfo) CBACEnabled() bool {
	switch li.Type {
	case Unlimited:
		return true
	case Enterprise:
		return true
	case Cluster:
		return true
	case Cloud:
		return true
	}
	return li.Overrides.Set(CBAC)
}

func EncodeMetadata(md map[string]interface{}) ([]byte, error) {
	bb := bytes.NewBuffer(nil)
	if err := gob.NewEncoder(bb).Encode(md); err != nil {
		return nil, err
	}
	return bb.Bytes(), nil
}

func (li LicenseInfo) decodeMetadata(obj interface{}) error {
	if len(li.Metadata) == 0 {
		return ErrNoMetadata
	}
	bb := bytes.NewBuffer(li.Metadata)
	return gob.NewDecoder(bb).Decode(obj)
}

func (li LicenseInfo) Get(key string) (val interface{}, err error) {
	var md map[string]interface{}
	var ok bool
	if err = li.decodeMetadata(&md); err != nil {
		return
	}
	if val, ok = md[key]; !ok {
		err = ErrNoMetadata
	}
	return
}

func (li LicenseInfo) Features() Features {
	return Features{
		Replication:     li.ReplicationEnabled(),
		SingleSignon:    li.SSOEnabled(),
		Overwatch:       li.OverwatchEnabled(),
		NoStats:         li.NoStatsEnabled(),
		UnlimitedCPU:    li.UnlimitedCPUEnabled(),
		CBAC:            li.CBACEnabled(),
		UnlimitedIngest: li.UnlimitedIngestEnabled(),
		LogbotLLM:       li.LogbotLLMEnabled(),
	}
}

func (lt LicenseType) Valid() bool {
	switch lt {
	case Free:
		return true
	case Cluster:
		return true
	case Unlimited:
		return true
	case Single:
		return true
	case Eval:
		return true
	case Community:
		return true
	case Fractional:
		return true
	case Enterprise:
		return true
	case Cloud:
		return true
	}
	return false
}

func (lt LicenseType) String() string {
	switch lt {
	case Free:
		return `free`
	case Cluster:
		return `cluster`
	case Unlimited:
		return `unlimited`
	case Single:
		return `single`
	case Eval:
		return `eval`
	case Community:
		return `community`
	case Fractional:
		return `fractional`
	case Enterprise:
		return `enterprise`
	case Cloud:
		return `cloud`
	default:
	}
	return "Unknown"
}

func (lt LicenseType) Abbr() string {
	switch lt {
	case Free:
		return `O`
	case Cluster:
		return `C`
	case Unlimited:
		return `U`
	case Single:
		return `S`
	case Eval:
		return `E`
	case Community:
		return `P`
	case Fractional:
		return `F`
	case Enterprise:
		return `N`
	case Cloud:
		return `H`
	default:
	}
	return "X"
}

func (lt LicenseType) AllFeatures() (r bool) {
	switch lt {
	case Unlimited:
		r = true
	case Enterprise:
		r = true
	case Cloud:
		r = true
	}
	return
}

func (lt LicenseType) SingleNode() (r bool) {
	switch lt {
	case Free:
		r = true
	case Community:
		r = true
	case Eval:
		r = true
	case Enterprise:
		r = true
	case Fractional:
		r = true
	case Single:
		r = true
	}
	return
}

func (lt LicenseType) MarshalJSON() ([]byte, error) {
	return []byte(`"` + lt.String() + `"`), nil
}

func parseUint(txt string) (uint64, error) {
	base := 10
	if strings.HasPrefix(txt, `0x`) {
		base = 16
		txt = strings.TrimPrefix(txt, `0x`)
	} else if strings.HasPrefix(txt, `0b`) {
		base = 2
		txt = strings.TrimPrefix(txt, `0b`)
	} else if strings.HasPrefix(txt, `0`) {
		base = 8
	}
	return strconv.ParseUint(txt, base, 64)
}

func (lt *LicenseType) UnmarshalJSON(v []byte) error {
	vs := strings.Trim(string(v), `"`) // this is safe becuase the cast to string on v will copy it
	temp, err := ParseType(vs)
	if err != nil {
		//try to parse it as an integer
		if vint, lerr := parseUint(vs); lerr != nil || !LicenseType(vint).Valid() {
			return err
		} else {
			temp = LicenseType(vint)
		}
	}
	*lt = temp
	return nil
}

func ParseType(c string) (LicenseType, error) {
	c = strings.ToLower(c)
	switch c {
	case `free`:
		return Free, nil
	case `cluster`:
		return Cluster, nil
	case `unlimited`:
		return Unlimited, nil
	case `single`:
		return Single, nil
	case `eval`:
		return Eval, nil
	case `community`:
		return Community, nil
	case `fractional`:
		return Fractional, nil
	case `enterprise`:
		return Enterprise, nil
	case `cloud`:
		return Cloud, nil
	}
	return 0, errors.New("unknown license type")
}

func FeatureOverridesString(fo FeatureOverride) (s string) {
	mask := FeatureOverride(0x1)
	var t FeatureOverride
	for i := 0; i < 63; i++ {
		t = (fo & mask)
		if t != 0 {
			if s == `` {
				s = t.String()
			} else {
				s += ` ` + t.String()
			}
		}
		mask = mask << 1
	}
	return
}

func NewFeatureOverride(name string) (fo FeatureOverride, err error) {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case ReplicationName:
		fo = Replication
	case SingleSignonName:
		fo = SingleSignon
	case OverwatchName:
		fo = Overwatch
	case NoStatsName:
		fo = NoStats
	case UnlimitedCPUName:
		fo = UnlimitedCPU
	case UnlimitedIngestName:
		fo = UnlimitedIngest
	case CBACName:
		fo = CBAC
	case LogbotLLMName:
		fo = LogbotLLM
	default:
		err = fmt.Errorf("Unknown feature override name %q", name)
	}
	return
}

func ParseFeatureOverrides(v string) (fo FeatureOverride, err error) {
	if v = strings.ToLower(v); v == `none` || len(v) == 0 {
		fo = 0
		return
	}
	bits := strings.Split(v, ",")
	var temp FeatureOverride
	for _, b := range bits {
		b = strings.TrimSpace(b)
		if temp, err = NewFeatureOverride(b); err != nil {
			return
		}
		fo.Update(temp)
	}
	return
}

func (fo FeatureOverride) String() (r string) {
	if fo == 0 {
		return `None`
	}
	if fo.Set(Replication) {
		r += `Replication `
	}
	if fo.Set(SingleSignon) {
		r += `SSO `
	}
	if fo.Set(Overwatch) {
		r += `Overwatch `
	}
	if fo.Set(NoStats) {
		r += `Nostats `
	}
	if fo.Set(UnlimitedCPU) {
		r += `Unlimited CPU `
	}
	if fo.Set(UnlimitedIngest) {
		r += `Unlimited Ingest `
	}
	if fo.Set(CBAC) {
		r += `CBAC `
	}
	if fo.Set(LogbotLLM) {
		r += `LogbotLLM `
	}
	return
}

func (fo FeatureOverride) Set(t FeatureOverride) bool {
	return (fo & t) == t
}

func (fo *FeatureOverride) Update(t FeatureOverride) {
	*fo = *fo | t
}
