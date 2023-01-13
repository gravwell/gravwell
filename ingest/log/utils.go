/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package log

import (
	"fmt"
	"io"
	"runtime"

	"github.com/crewjam/rfc5424"
	"github.com/shirou/gopsutil/host"
)

func KV(name string, value interface{}) (r rfc5424.SDParam) {
	r.Name = name
	switch v := value.(type) {
	case string:
		r.Value = v
	default:
		r.Value = fmt.Sprintf("%v", value)
	}
	return
}

func KVErr(err error) rfc5424.SDParam {
	return KV("error", err)
}

func PrintOSInfo(wtr io.Writer) {
	if platform, _, version, err := host.PlatformInformation(); err == nil {
		fmt.Fprintf(wtr, "OS:\t\t%s %s [%s] (%s %s)\n", runtime.GOOS, runtime.GOARCH, kernelVersion, platform, version)
	} else {
		fmt.Fprintf(wtr, "OS:\t\tERROR %v\n", err)
	}
}
