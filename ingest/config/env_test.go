/*************************************************************************
 * Copyright 2023 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package config

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvLoadString(t *testing.T) {
	envId := `GRAVWELL_TEST`
	tval := `testing123`
	def := `default stuff`
	var v string

	//attempt to load with nothing set
	if err := LoadEnvVar(&v, envId, def); err != nil {
		t.Fatal(err)
	} else if v != def {
		t.Fatalf("Did not load default value: %s != %s", v, def)
	}

	//load with something already there
	if err := LoadEnvVar(&v, envId, `ignore me`); err != nil {
		t.Fatal(err)
	} else if v != def {
		t.Fatalf("Did not leave existing value: %s %s", v, def)
	}

	//load something into the environment
	if err := os.Setenv(envId, tval); err != nil {
		t.Fatal(err)
	}

	//try again with something there
	if err := LoadEnvVar(&v, envId, `ignore me`); err != nil {
		t.Fatal(err)
	} else if v != def {
		t.Fatalf("Did not leave existing value: %s %s", v, def)
	}
	//wipe out the existing and check that we load from the env
	v = ``
	if err := LoadEnvVar(&v, envId, `ignore me`); err != nil {
		t.Fatal(err)
	} else if v != tval {
		t.Fatalf("Did not pull value from environment: %s != %s", v, tval)
	}
}

func TestEnvLoadInt64(t *testing.T) {
	envId := `GRAVWELL_TEST_INT64`
	tval := `123`
	def := int64(99)
	var v int64

	//attempt to load with nothing set
	if err := LoadEnvVar(&v, envId, def); err != nil {
		t.Fatal(err)
	} else if v != def {
		t.Fatalf("Did not load default value: %v != %v", v, def)
	}

	//load with something already there
	if err := LoadEnvVar(&v, envId, int64(10000)); err != nil {
		t.Fatal(err)
	} else if v != def {
		t.Fatalf("Did not leave existing value: %v %v", v, def)
	}

	//load something into the environment
	if err := os.Setenv(envId, tval); err != nil {
		t.Fatal(err)
	}

	//try again with something there
	if err := LoadEnvVar(&v, envId, int64(22)); err != nil {
		t.Fatal(err)
	} else if v != def {
		t.Fatalf("Did not leave existing value: %v %v", v, def)
	}
	//wipe out the existing and check that we load from the env
	v = 0
	if err := LoadEnvVar(&v, envId, int64(-100)); err != nil {
		t.Fatal(err)
	} else if v != 123 {
		t.Fatalf("Did not pull value from environment: %v != %v", v, tval)
	}
}

func TestEnvLoadUInt64(t *testing.T) {
	envId := `GRAVWELL_TEST_UINT64`
	tval := `0x12345`
	def := uint64(9876)
	var v uint64

	//attempt to load with nothing set
	if err := LoadEnvVar(&v, envId, def); err != nil {
		t.Fatal(err)
	} else if v != def {
		t.Fatalf("Did not load default value: %v != %v", v, def)
	}

	//load with something already there
	if err := LoadEnvVar(&v, envId, uint64(10000)); err != nil {
		t.Fatal(err)
	} else if v != def {
		t.Fatalf("Did not leave existing value: %v %v", v, def)
	}

	//load something into the environment
	if err := os.Setenv(envId, tval); err != nil {
		t.Fatal(err)
	}

	//try again with something there
	if err := LoadEnvVar(&v, envId, uint64(22)); err != nil {
		t.Fatal(err)
	} else if v != def {
		t.Fatalf("Did not leave existing value: %v %v", v, def)
	}
	//wipe out the existing and check that we load from the env
	v = 0
	if err := LoadEnvVar(&v, envId, uint64(0xffffff)); err != nil {
		t.Fatal(err)
	} else if v != 0x12345 {
		t.Fatalf("Did not pull value from environment: %v != %v", v, tval)
	}
}

func TestEnvLoadUInt16(t *testing.T) {
	envId := `GRAVWELL_TEST_UINT16`
	tval := `0x1234`
	def := uint16(9876)
	var v uint16

	//attempt to load with nothing set
	if err := LoadEnvVar(&v, envId, def); err != nil {
		t.Fatal(err)
	} else if v != def {
		t.Fatalf("Did not load default value: %v != %v", v, def)
	}

	//load with something already there
	if err := LoadEnvVar(&v, envId, uint16(1000)); err != nil {
		t.Fatal(err)
	} else if v != def {
		t.Fatalf("Did not leave existing value: %v %v", v, def)
	}

	//load something into the environment
	if err := os.Setenv(envId, tval); err != nil {
		t.Fatal(err)
	}

	//try again with something there
	if err := LoadEnvVar(&v, envId, uint16(22)); err != nil {
		t.Fatal(err)
	} else if v != def {
		t.Fatalf("Did not leave existing value: %v %v", v, def)
	}
	//wipe out the existing and check that we load from the env
	v = 0
	if err := LoadEnvVar(&v, envId, uint16(0xffff)); err != nil {
		t.Fatal(err)
	} else if v != 0x1234 {
		t.Fatalf("Did not pull value from environment: %v != %v", v, tval)
	}

	//clear and set it to something that overflows
	envId = `GRAVWELL_TEST_UINT16_OVERFLOW`
	tval = `0x10000`
	v = 0
	if err := os.Setenv(envId, tval); err != nil {
		t.Fatal(err)
	}
	if err := LoadEnvVar(&v, envId, uint16(0xffff)); err == nil {
		t.Fatal("Failed to catch overflow")
	}
}

func TestEnvLoadInt16(t *testing.T) {
	envId := `GRAVWELL_TEST_INT16`
	tval := `0x1234`
	def := int16(9876)
	var v int16

	//attempt to load with nothing set
	if err := LoadEnvVar(&v, envId, def); err != nil {
		t.Fatal(err)
	} else if v != def {
		t.Fatalf("Did not load default value: %v != %v", v, def)
	}

	//load with something already there
	if err := LoadEnvVar(&v, envId, int16(1000)); err != nil {
		t.Fatal(err)
	} else if v != def {
		t.Fatalf("Did not leave existing value: %v %v", v, def)
	}

	//load something into the environment
	if err := os.Setenv(envId, tval); err != nil {
		t.Fatal(err)
	}

	//try again with something there
	if err := LoadEnvVar(&v, envId, int16(22)); err != nil {
		t.Fatal(err)
	} else if v != def {
		t.Fatalf("Did not leave existing value: %v %v", v, def)
	}
	//wipe out the existing and check that we load from the env
	v = 0
	if err := LoadEnvVar(&v, envId, int16(0x7fff)); err != nil {
		t.Fatal(err)
	} else if v != 0x1234 {
		t.Fatalf("Did not pull value from environment: %v != %v", v, tval)
	}

	//clear and set it to something that overflows
	envId = `GRAVWELL_TEST_INT16_OVERFLOW`
	tval = `0x10000`
	v = 0
	if err := os.Setenv(envId, tval); err != nil {
		t.Fatal(err)
	}
	if err := LoadEnvVar(&v, envId, int16(0x7fff)); err == nil {
		t.Fatal("Failed to catch overflow")
	}
}

func TestEnvLoadBool(t *testing.T) {
	envId := `GRAVWELL_TEST_BOOL`
	tval := `TRUE`
	def := false
	var v bool

	//attempt to load with nothing set
	if err := LoadEnvVar(&v, envId, def); err != nil {
		t.Fatal(err)
	} else if v != def {
		t.Fatalf("Did not load default value: %v != %v", v, def)
	}

	//load with something already there
	if err := LoadEnvVar(&v, envId, false); err != nil {
		t.Fatal(err)
	} else if v != def {
		t.Fatalf("Did not leave existing value: %v %v", v, def)
	}

	//load something into the environment
	if err := os.Setenv(envId, tval); err != nil {
		t.Fatal(err)
	}

	//try again with something there
	v = true
	pre := v
	if err := LoadEnvVar(&v, envId, def); err != nil {
		t.Fatal(err)
	} else if v != pre {
		t.Fatalf("Did not leave existing value: %v %v", v, pre)
	}
	//wipe out the existing and check that we load from the env
	v = false
	if err := LoadEnvVar(&v, envId, false); err != nil {
		t.Fatal(err)
	} else if !v {
		t.Fatalf("Did not pull value from environment: %v != true", v)
	}
}

func TestEnvFileLoadString(t *testing.T) {
	envId := `GRAVWELL_STRING_TEST`
	envFileId := envId + `_FILE`
	tfile := filepath.Join(tempDir, envId+`_FILE`)
	tval := `testing123`
	def := `default values`
	var v string
	if err := ioutil.WriteFile(tfile, []byte(tval), 0660); err != nil {
		t.Fatal(err)
	}

	//attempt to load with nothing set
	if err := LoadEnvVar(&v, envId, def); err != nil {
		t.Fatal(err)
	} else if v != def {
		t.Fatalf("Did not load default value: %s != %s", v, def)
	}

	//load with something already there
	if err := LoadEnvVar(&v, envId, `ignore me`); err != nil {
		t.Fatal(err)
	} else if v != def {
		t.Fatalf("Did not leave existing value: %s %s", v, def)
	}

	//load the file in the file extension
	if err := os.Setenv(envFileId, tfile); err != nil {
		t.Fatal(err)
	}

	//try again with something there
	if err := LoadEnvVar(&v, envId, `ignore me`); err != nil {
		t.Fatal(err)
	} else if v != def {
		t.Fatalf("Did not leave existing value: %s %s", v, def)
	}
	//wipe out the existing and check that we load from the env
	v = ``
	if err := LoadEnvVar(&v, envId, `ignore me`); err != nil {
		t.Fatal(err)
	} else if v != tval {
		t.Fatalf("Did not pull value from environment: %s != %s", v, tval)
	}
}

func TestEnvFileLoadInt64(t *testing.T) {
	envId := `GRAVWELL_INT64_TEST`
	envFileId := envId + `_FILE`
	tfile := filepath.Join(tempDir, envId+`_FILE`)
	tval := `0x1234`
	def := int64(1000)
	var v int64
	if err := ioutil.WriteFile(tfile, []byte(tval), 0660); err != nil {
		t.Fatal(err)
	}

	//attempt to load with nothing set
	if err := LoadEnvVar(&v, envId, def); err != nil {
		t.Fatal(err)
	} else if v != def {
		t.Fatalf("Did not load default value: %v != %v", v, def)
	}

	//load with something already there
	if err := LoadEnvVar(&v, envId, int64(1)); err != nil {
		t.Fatal(err)
	} else if v != def {
		t.Fatalf("Did not leave existing value: %v %v", v, def)
	}

	//load the file in the file extension
	if err := os.Setenv(envFileId, tfile); err != nil {
		t.Fatal(err)
	}

	//try again with something there
	if err := LoadEnvVar(&v, envId, int64(1)); err != nil {
		t.Fatal(err)
	} else if v != def {
		t.Fatalf("Did not leave existing value: %v %v", v, def)
	}
	//wipe out the existing and check that we load from the env
	v = 0
	if err := LoadEnvVar(&v, envId, int64(1)); err != nil {
		t.Fatal(err)
	} else if v != 0x1234 {
		t.Fatalf("Did not pull value from environment: %v != %v", v, tval)
	}
}
