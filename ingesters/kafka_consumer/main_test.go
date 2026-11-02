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

	"github.com/gravwell/gravwell/v3/ingest/config"
	"github.com/stretchr/testify/require"
)

// main() parses the global flag set and can call os.Exit, so these tests run it in a
// child copy of the test binary. The child is selected with this environment variable
// and receives its command line, separated by newlines, in mainArgsEnv.
const (
	mainHelperEnv = `KAFKA_CONSUMER_MAIN_HELPER`
	mainArgsEnv   = `KAFKA_CONSUMER_MAIN_ARGS`
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

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), `kafka.conf`)
	require.NoError(t, os.WriteFile(p, []byte(body), 0600))
	return p
}

const mainTestConfig = `
[Global]
Ingest-Secret = IngestSecrets
Pipe-Backend-Target=/nonexistent/gravwell/pipe
Log-Level=INFO

[Consumer "default"]
	Leader="127.0.0.1"
	Topic="foo"
	Default-Tag=foo
	Tags=bar*
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
	// a consumer without a Leader is rejected by GetConfig
	cfgPath := writeTempConfig(t, strings.Replace(mainTestConfig, `Leader="127.0.0.1"`, ``, 1))
	_, stderr, code := runMain(t, `-config-file`, cfgPath, `-config-overlays`, t.TempDir())
	require.NotZero(t, code)
	require.Contains(t, stderr, `failed to get configuration`)
	require.Contains(t, stderr, `Leader`)
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
	require.Equal(t, `kafka.conf`, filepath.Base(defaultConfigLoc))
	require.Equal(t, defaultConfigLoc+`.d`, defaultConfigDLoc)
	require.True(t, filepath.IsAbs(defaultConfigLoc))
}

func TestCfgTypeSatisfiesBaseInterfaces(t *testing.T) {
	cfg, err := GetConfig(writeTempConfig(t, mainTestConfig), ``)
	require.NoError(t, err)
	var ic config.IngestConfig = cfg.IngestBaseConfig()
	require.Equal(t, `IngestSecrets`, ic.Secret())
	tgs, err := cfg.Tags()
	require.NoError(t, err)
	require.Equal(t, []string{`foo`}, tgs)
}

func TestDebugout(t *testing.T) {
	old, oldDebug := os.Stdout, debugOn
	t.Cleanup(func() { os.Stdout, debugOn = old, oldDebug })
	capture := func(on bool) string {
		r, w, err := os.Pipe()
		require.NoError(t, err)
		os.Stdout, debugOn = w, on
		debugout("hello %s %d\n", "kafka", 42)
		w.Close()
		os.Stdout = old
		out, err := io.ReadAll(r)
		require.NoError(t, err)
		return string(out)
	}
	require.Empty(t, capture(false))
	require.Equal(t, "hello kafka 42\n", capture(true))
}
