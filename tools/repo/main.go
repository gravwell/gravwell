package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// mods is a list of all folder paths with a go.mod.
// Paths are relative to repo root.
var mods = []string{
	".",
	"./e2e",
	"./tools/mock/mimecast",
}

var (
	ErrUnknownCommand = errors.New("unknown command")
	ErrBadArgs        = errors.New("bad arguments")
)

// main is expected to be run relative to the repo root.
// this should only be run via go tool repo [cmd] [args]
func main() {
	command := os.Args[1]
	var err error
	switch command {
	case "tidy":
		err = tidy(os.Args[2:])
	case "bump-runtime":
		err = bump(os.Args[2:])
	default:
		err = ErrUnknownCommand
	}
	if err != nil {
		fmt.Printf("error running command %q: %v\n", command, err)
		os.Exit(1)
	}
}

func tidy(args []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	for _, mod := range mods {
		cmd := exec.Command("go", "mod", "tidy")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Dir = filepath.Clean(wd + "/" + mod)
		err = cmd.Run()
		if err != nil {
			return err
		}
	}
	return nil
}

var runtimes = []string{
	"./e2e/hosted/Dockerfile",
	"./e2e/hosted/mimecast_test.go",
	"./e2e/Dockerfile",
	"./tools/mock/mimecast/Dockerfile",
}

func bump(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("%w: expected 2 args: [from] [to]", ErrBadArgs)
	}

	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	from := args[0]
	to := args[1]

	for _, mod := range mods {
		file := filepath.Clean(fmt.Sprintf("%s/%s/go.mod", wd, mod))
		if err := replace(file, from, to); err != nil {
			return err
		}
	}
	for _, ref := range runtimes {
		file := filepath.Clean(fmt.Sprintf("%s/%s", wd, ref))
		if err := replace(file, from, to); err != nil {
			return err
		}
	}
	return tidy(os.Args[2:])
}

func replace(path, from, to string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("could not read file %q: %v\n", path, err)
	}
	updated := strings.Replace(string(contents), from, to, 1)
	err = os.WriteFile(path, []byte(updated), 0644)
	if err != nil {
		return fmt.Errorf("could not update file %q: %v\n", path, err)
	}
	return nil
}
