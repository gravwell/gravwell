/*************************************************************************
 * Copyright 2018 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package processors

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gravwell/gravwell/v3/ingest/config"
)

const (
	pluginBasePath = `src/github.com/gravwell/gravwell/ingest/processors/plugins`
	basicTest      = `test/test`
)

func TestPluginLoadConfig(t *testing.T) {
	vars := map[string]string{
		`Upper`: `true`,
	}
	// resolve the testPluginPath for our hashicorp plugin system
	testPluginPath := getPluginPath(basicTest)
	if !pluginAvailable(testPluginPath) {
		return
	}

	vc, err := loadConfig(testPluginPath, vars)
	if err != nil {
		t.Fatal(err)
	}

	//and try to load the plugin config
	if pc, err := PluginLoadConfig(vc); err != nil {
		t.Fatal(err)
	} else if pc.Plugin_Path != testPluginPath {
		t.Fatalf("invalid plugin-path: %q != %q", pc.Plugin_Path, testPluginPath)
	} else if dp, ok := pc.Get(`Upper`); !ok || dp != `true` {
		t.Fatalf("invalid config value: %q != true", dp)
	}
}

func TestPluginLoadError(t *testing.T) {
	vars := map[string]string{
		`Upper`: `true`,
		`Lower`: `true`,
		`Error`: `testing`,
		//`Verbose`: `true`,
		//`Debug`: `true`,
	}
	// resolve the testPluginPath for our hashicorp plugin system
	testPluginPath := getPluginPath(basicTest)
	if !pluginAvailable(testPluginPath) {
		return
	}

	vc, err := loadConfig(testPluginPath, vars)
	if err != nil {
		t.Fatal(err)
	}

	//and try to load the plugin config
	if _, err := PluginLoadConfig(vc); err == nil {
		t.Fatal("Load config did not return an error")
	} else if err.Error() != `testing` {
		t.Fatalf("got a bad error returned: %q", err)
	}
}

func TestPluginFlush(t *testing.T) {
	vars := map[string]string{
		`Upper`: `true`,
		`Lower`: `true`,
		//`Verbose`: `true`,
		//`Debug`: `true`,
	}
	// resolve the testPluginPath for our hashicorp plugin system
	testPluginPath := getPluginPath(basicTest)
	if !pluginAvailable(testPluginPath) {
		return
	}

	vc, err := loadConfig(testPluginPath, vars)
	if err != nil {
		t.Fatal(err)
	}

	//and try to load the plugin config
	pc, err := PluginLoadConfig(vc)
	if err != nil {
		t.Fatal(err)
	}

	p, err := NewPluginProcessor(pc)
	if err != nil {
		t.Fatal(err)
	}

	if ents := p.Flush(); len(ents) != 0 {
		t.Fatal("UMM... flush returned stuff")
	}

	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
}

const skel = `
[global]
[preprocessor "test"]
	type = plugin
	Plugin-Path="%s"
`

func configBytes(pth string, vars map[string]string) []byte {
	bb := bytes.NewBuffer(nil)
	fmt.Fprintf(bb, skel, pth)
	for k, v := range vars {
		fmt.Fprintf(bb, "\n\t%s=%s", k, v)
	}
	return bb.Bytes()
}

func getPluginPath(pth string) string {
	if p, ok := os.LookupEnv(`PLUGIN_PATH_OVERRIDE`); ok {
		return filepath.Join(p, basicTest)
	}
	return filepath.Join(goPath, pluginBasePath, basicTest)
}

func loadConfig(pth string, vars map[string]string) (vc *config.VariableConfig, err error) {
	tc := struct {
		Global struct {
		}
		Item map[string]*struct {
			Name  string
			Value int
		}
		Preprocessor ProcessorConfig
	}{}
	if err = config.LoadConfigBytes(&tc, configBytes(pth, vars)); err != nil {
		return
	}
	var ok bool
	if vc, ok = tc.Preprocessor[`test`]; !ok {
		err = errors.New("Missing config")
	} else if vc == nil {
		err = errors.New("Variable config is empty")
	}

	return
}

var pluginMissHit bool

func pluginAvailable(pth string) (r bool) {
	if fi, err := os.Stat(pth); err == nil && fi.Mode().IsRegular() {
		r = true
	}
	if !r && !pluginMissHit {
		fmt.Println(pth, "is not available, did you build the test plugin")
		pluginMissHit = true
	}
	return
}
