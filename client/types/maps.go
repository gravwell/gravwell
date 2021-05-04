/*************************************************************************
 * Copyright 2021 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package types

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
)

var (
	ErrInvalidGeofence = errors.New("Invalid geofence")
)

type PointmapKV struct {
	Key   string
	Value string
}

type Location struct {
	Lat  float64
	Long float64
}

type PointmapValue struct {
	Loc      Location
	Metadata []PointmapKV `json:",omitempty"`
}

type PointmapRequest struct {
	BaseRequest
	Fence Geofence `json:",omitempty"`
}

type PointmapResponse struct {
	BaseResponse
	Entries []PointmapValue `json:",omitempty"`
}

type HeatmapRequest struct {
	BaseRequest
	Fence Geofence `json:",omitempty"`
}

type HeatmapResponse struct {
	BaseResponse
	Entries []HeatmapValue `json:",omitempty"`
}

type HeatmapValue struct {
	Location
	Magnitude float64 `json:",omitempty"`
}

type P2PValue struct {
	Src       Location
	Dst       Location
	Magnitude float64  `json:",omitempty"`
	Values    []string `json:",omitempty"`
}

type P2PRequest struct {
	BaseRequest
	Fence Geofence `json:",omitempty"`
}

type P2PResponse struct {
	BaseResponse
	ValueNames []string
	Entries    []P2PValue `json:",omitempty"`
}

type Geofence struct {
	SouthWest Location `json:",omitempty"`
	NorthEast Location `json:",omitempty"`
	enabled   bool
}

func (pkv PointmapKV) IsEmpty() bool {
	return len(pkv.Key) == 0 || len(pkv.Value) == 0
}

func (gf *Geofence) CrossesAntimeridian() bool {
	return gf.SouthWest.Long > gf.NorthEast.Long
}

func (gf *Geofence) Validate() error {
	if gf.SouthWest.Lat == 0.0 && gf.SouthWest.Long == 0.0 && gf.NorthEast.Lat == 0.0 && gf.NorthEast.Long == 0.0 {
		return nil
	}

	// Check bounds of Lat / Long
	if !gf.SouthWest.Valid() || !gf.NorthEast.Valid() {
		return ErrInvalidGeofence
	}

	// Ensure that "SouthWest" is not North of "NorthEast"
	if gf.SouthWest.Lat > gf.NorthEast.Lat {
		return ErrInvalidGeofence
	}

	// SouthWest.Long can be greater than NorthEast.Long (A bounds that crosses the antimeridian)
	// Or
	// SouthWest.Long can be less than NorthEast.Long (A bounds that DOES NOT cross the antimeridian)

	gf.enabled = true
	return nil
}

func (gf *Geofence) InFence(loc Location) bool {
	if !loc.Valid() {
		return false
	}
	if !gf.enabled {
		return true //if its not enabled, everything is always in the fence
	}

	if loc.Lat > gf.NorthEast.Lat || loc.Lat < gf.SouthWest.Lat {
		return false
	}

	if gf.CrossesAntimeridian() {
		if loc.Long > gf.NorthEast.Long && loc.Long < gf.SouthWest.Long {
			return false
		}

	} else {
		if loc.Long > gf.NorthEast.Long || loc.Long < gf.SouthWest.Long {
			return false
		}
	}

	return true
}

func (loc Location) Encode() (v []byte) {
	v = make([]byte, 16)
	binary.LittleEndian.PutUint64(v, math.Float64bits(loc.Lat))
	binary.LittleEndian.PutUint64(v[8:], math.Float64bits(loc.Long))
	return
}

func (loc *Location) Decode(v []byte) bool {
	if len(v) < 16 {
		return false
	}
	loc.Lat = math.Float64frombits(binary.LittleEndian.Uint64(v))
	loc.Long = math.Float64frombits(binary.LittleEndian.Uint64(v[8:]))
	return true
}

func (loc Location) String() string {
	return fmt.Sprintf("%f %f", loc.Lat, loc.Long)
}

func (loc Location) Valid() bool {
	// Latitude is bound -90 to 90
	if loc.Lat > 90.0 || loc.Lat < -90.0 {
		return false
	}
	// Longitude is bound -180 to 180
	if loc.Long > 180.0 || loc.Long < -180.0 {
		return false
	}
	return true
}

func (pkv PointmapKV) MarshalJSON() (r []byte, err error) {
	r = []byte(fmt.Sprintf(`{"%s":%s}`, pkv.Key, strconv.Quote(pkv.Value)))
	return
}

func (hv HeatmapValue) MarshalJSON() ([]byte, error) {
	if hv.Magnitude == 0.0 {
		return []byte(fmt.Sprintf(`[%f, %f]`, hv.Lat, hv.Long)), nil
	}
	return []byte(fmt.Sprintf(`[%f, %f, %f]`, hv.Lat, hv.Long, hv.Magnitude)), nil
}

func (hv *HeatmapValue) UnmarshalJSON(data []byte) error {
	var a []float64
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	if len(a) != 3 && len(a) != 2 {
		return fmt.Errorf("Expected 2 or 3 values, got %v", len(a))
	}
	hv.Lat = a[0]
	hv.Long = a[1]
	if len(a) == 3 {
		hv.Magnitude = a[2]
	}
	return nil
}
