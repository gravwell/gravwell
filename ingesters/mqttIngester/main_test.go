/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bytes"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// main() parses the global flag set and can call os.Exit, so these tests run it in a
// child copy of the test binary. The child is selected with this environment variable
// and receives its command line, separated by newlines, in mainArgsEnv.
const (
	mainHelperEnv = `MQTT_INGESTER_MAIN_HELPER`
	mainArgsEnv   = `MQTT_INGESTER_MAIN_ARGS`
)

func TestMainHelperProcess(t *testing.T) {
	if os.Getenv(mainHelperEnv) != `1` {
		t.Skip("helper process for the main() tests")
	}
	args := []string{ingesterName}
	if v := os.Getenv(mainArgsEnv); v != `` {
		args = append(args, strings.Split(v, "\n")...)
	}
	os.Args = args
	flag.CommandLine = flag.NewFlagSet(ingesterName, flag.ExitOnError)
	main()
	os.Exit(0)
}

// runMain executes main() in a child process with args and returns its output and exit code.
func runMain(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(os.Args[0], `-test.run=^TestMainHelperProcess$`)
	cmd.Env = append(os.Environ(), mainHelperEnv+`=1`, mainArgsEnv+`=`+strings.Join(args, "\n"))
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else {
		require.NoError(t, err)
	}
	return so.String(), se.String(), code
}

const mainTestConfig = `
[Global]
Ingest-Secret = IngestSecrets
Pipe-Backend-Target=/nonexistent/gravwell/pipe
Log-Level=INFO

[Subscription "default"]
	Broker="mqtt://127.0.0.1:1883"
	Topic="gravwell/#"
	Default-Tag=mqtt
`

func TestMainVersion(t *testing.T) {
	stdout, _, code := runMain(t, `-version`)
	require.Zero(t, code)
	require.NotEmpty(t, strings.TrimSpace(stdout), "-version should print version information")
}

func TestMainMissingConfig(t *testing.T) {
	missing := filepath.Join(t.TempDir(), `does-not-exist.conf`)
	_, stderr, code := runMain(t, `-config-file`, missing, `-config-overlays`, t.TempDir())
	require.NotZero(t, code, "configuration errors must exit non-zero")
	require.Contains(t, stderr, `failed to get configuration`)
}

func TestMainInvalidConfig(t *testing.T) {
	// a subscription without a Broker is rejected by GetConfig
	cfgPath := writeTempConfig(t, strings.Replace(mainTestConfig, "\tBroker=\"mqtt://127.0.0.1:1883\"\n", ``, 1))
	_, stderr, code := runMain(t, `-config-file`, cfgPath, `-config-overlays`, t.TempDir())
	require.NotZero(t, code)
	require.Contains(t, stderr, `failed to get configuration`)
	require.Contains(t, stderr, `Broker`)
}

func TestMainValidateUUIDConfig(t *testing.T) {
	cfgPath := writeTempConfig(t, mainTestConfig)
	stdout, stderr, code := runMain(t, `-config-file`, cfgPath, `-config-overlays`, t.TempDir(), `-validate-uuid-config`)
	require.Zero(t, code, "stderr: %s", stderr)
	require.Contains(t, stdout, `configuration is valid`)

	// the UUID written back must be picked up by the ingester's own config loader
	cfg, err := GetConfig(cfgPath, ``)
	require.NoError(t, err)
	_, ok := cfg.IngesterUUID()
	require.True(t, ok, "Ingester-UUID should have been written into the config file")
}

func TestDefaultConfigLocations(t *testing.T) {
	require.Equal(t, `mqtt.conf`, filepath.Base(defaultConfigLoc))
	require.Equal(t, defaultConfigLoc+`.d`, defaultConfigDLoc)
	require.True(t, filepath.IsAbs(defaultConfigLoc))
}

func TestDefaultClientID(t *testing.T) {
	const uuid = `3f2504e0-4f89-11d3-9a0c-0305e82c3301`
	tests := []struct {
		name string
		sub  string
		want string
	}{
		{name: `simple`, sub: `sensors`, want: `gravwell-mqtt-3f2504e0-sensors`},
		{name: `spaces are replaced`, sub: `my sensors`, want: `gravwell-mqtt-3f2504e0-my_sensors`},
		{name: `punctuation is replaced`, sub: `a/b.c`, want: `gravwell-mqtt-3f2504e0-a_b_c`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, defaultClientID(uuid, tt.sub))
		})
	}
	// a short uuid must not panic
	require.Equal(t, `gravwell-mqtt-abc-s`, defaultClientID(`abc`, `s`))
}

func TestDebugout(t *testing.T) {
	old, oldDebug := os.Stdout, debugOn
	t.Cleanup(func() { os.Stdout, debugOn = old, oldDebug })
	capture := func(on bool) string {
		r, w, err := os.Pipe()
		require.NoError(t, err)
		os.Stdout, debugOn = w, on
		debugout("hello %s %d\n", "mqtt", 42)
		w.Close()
		os.Stdout = old
		b, err := io.ReadAll(r)
		require.NoError(t, err)
		return string(b)
	}
	require.Equal(t, "hello mqtt 42\n", capture(true))
	require.Empty(t, capture(false))
}
