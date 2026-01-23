/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"bufio"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/gravwell/gravwell/v4/client"
	"github.com/gravwell/gravwell/v4/client/objlog"
	"github.com/gravwell/gravwell/v4/client/types"
	"golang.org/x/term"
)

const (
	envHost           = `GRAVWELL_HOST`
	envToken          = `GRAVWELL_TOKEN`
	envKitId          = `GRAVWELL_KIT_ID`
	envKitDir         = `GRAVWELL_KIT_DIR`
	envKitGlobal      = `GRAVWELL_KIT_GLOBAL`
	envKitWriteGlobal = `GRAVWELL_KIT_WRITE_GLOBAL`
	envKitGroups      = `GRAVWELL_KIT_GROUPS`
	envKitWriteGroups = `GRAVWELL_KIT_WRITE_GROUPS`
	envKitLabels      = `GRAVWELL_KIT_LABELS`
	envKitCtl         = `GRAVWELL_KITCTL`

	commandsStr = `Available Commands:
  list         List available kits
  pull         Pull a kit from a remote Gravwell instance
  push         Push a kit built from a local directory`
)

var (
	// default these to the values that may or may not be in environment variables
	hostUrl        = os.Getenv(envHost)
	authToken      = os.Getenv(envToken)
	kitId          = os.Getenv(envKitId)
	kitDir         = os.Getenv(envKitDir)
	kitGlobal      = getBoolFromString(os.Getenv(envKitGlobal))
	kitWriteGlobal = getBoolFromString(os.Getenv(envKitWriteGlobal))
	kitGroups      = os.Getenv(envKitGroups)
	kitWriteGroups = os.Getenv(envKitWriteGroups)
	kitLabels      = os.Getenv(envKitLabels)
	kitCtl         = os.Getenv(envKitCtl)

	fHost           = flag.String("host", "", "URL of Gravwell system")
	fToken          = flag.String("token", "", "Authentication token for Gravwell system")
	fKitId          = flag.String("kit-id", "", "Kit ID")
	fKitDir         = flag.String("kit-dir", "", "Directory to store kits")
	fKitCtl         = flag.String("kitctl", "", "Path to kitctl binary")
	fKitGlobal      = flag.Bool("kit-global", false, "Set to true to deploy kits with global access")
	fKitWriteGlobal = flag.Bool("kit-write-global", false, "Set to true to deploy kits with global write access")
	fKitGroups      = flag.String("kit-groups", "", "Comma separated list of groups to deploy the kit to")
	fKitWriteGroups = flag.String("kit-write-groups", "", "Comma separated list of groups to deploy the kit with write access")
	fKitLabels      = flag.String("kit-labels", "", "Comma separated list of labels to deploy the kit to")
	fIgnoreCert     = flag.Bool("ignore-cert", false, "Ignore TLS certificate errors")
)

// initVars just ensures that the hostUrl, authToken, and kitId variables are set from environment variables
// or the command line flags, if not provided it will check if we are in interactive mode and prompt the user
func initVars(cmd string) (err error) {
	// override from flags if set
	if *fHost != "" {
		hostUrl = *fHost
	}
	if *fToken != "" {
		authToken = *fToken
	}
	if *fKitId != "" {
		kitId = *fKitId
	}
	if *fKitDir != "" {
		kitDir = *fKitDir
	}
	if *fKitCtl != "" {
		kitCtl = *fKitCtl
	}
	if *fKitLabels != "" {
		kitLabels = *fKitLabels
	}
	if *fKitGroups != "" {
		kitGroups = *fKitGroups
	}
	if *fKitWriteGroups != "" {
		kitWriteGroups = *fKitWriteGroups
	}

	// do some dumb loops to determine if the boolean flags are set
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "kit-global":
			kitGlobal = *fKitGlobal
		case "kit-write-global":
			kitWriteGlobal = *fKitWriteGlobal
		}
	})
	// if either hostUrl or authToken are still empty, ask for them on the command line
	if !isInteractive() {
		// if we are in non-interactive mode and any of the vars are missing, just error out
		if hostUrl == "" {
			err = errors.New("no host URL provided")
			return
		} else if authToken == "" {
			err = errors.New("no authentication token provided")
			return
		} else if kitId == "" {
			err = errors.New("no kit ID provided")
			return
		}
		if cmd != `list` && (kitDir == `` || kitCtl == ``) {
			err = errors.New("no kit directory or kitctl path provided")
			return
		}
	} else {
		// in interactive mode, prompt for any missing values
		if hostUrl == "" {
			var uri *url.URL
			if uri, err = getURLFromStdin(); err != nil {
				return
			}
			hostUrl = uri.String()
		}
		if authToken == "" {
			if authToken, err = getToken(); err != nil {
				return
			}
		}
		if kitId == "" {
			if kitId, err = getStringFromStdin("Kit ID"); err != nil {
				return
			}
		}
		if cmd != `list` {
			if kitDir == `` {
				if kitDir, err = getStringFromStdin("Kit Directory"); err != nil {
					return
				}
			}
			if kitCtl == `` {
				if kitCtl, err = getStringFromStdin("Path to kitctl binary"); err != nil {
					return
				}
			}
		}
	}

	// if kitCtl was set then verify it exists and is executable
	if kitCtl != `` {
		if fi, err := os.Stat(kitCtl); err != nil {
			return fmt.Errorf("error accessing kitctl binary '%s': %w", kitCtl, err)
		} else if fi.IsDir() {
			return fmt.Errorf("kitctl path '%s' is a directory, not a binary", kitCtl)
		} else if fi.Mode()&0111 == 0 {
			return fmt.Errorf("kitctl path '%s' is not executable", kitCtl)
		}
	}
	return
}

func ensureKitDir() error {
	// if kitDir does not exist attempt to make it
	if fi, err := os.Stat(kitDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("error accessing kit directory '%s': %w", kitDir, err)
	} else if os.IsNotExist(err) {
		if err := os.MkdirAll(kitDir, 0750); err != nil {
			return fmt.Errorf("error creating kit directory '%s': %w", kitDir, err)
		}
	} else if !fi.IsDir() {
		return fmt.Errorf("kit directory path '%s' exists but is not a directory", kitDir)
	}
	return nil
}

func getClient() (cli *client.Client, err error) {
	opts := client.Opts{
		InsecureNoEnforceCerts: *fIgnoreCert,
		ObjLogger:              &objlog.NilObjLogger{},
	}
	// parse the fHost as a URL to decide if we are going to use TLS
	var uri *url.URL
	if uri, err = url.Parse(hostUrl); err != nil {
		return
	}
	opts.Server = uri.Host
	switch uri.Scheme {
	case `http`:
		opts.UseHttps = false
	case `https`:
		opts.UseHttps = true
	case ``: // assume http if no scheme given
		opts.UseHttps = false
	default:
		err = errors.New("invalid URL scheme")
		return
	}

	cli, err = client.NewOpts(opts)
	if err != nil {
		cli = nil
		return
	}

	// login with the API token and check API versions
	var wrn string // warning message that comes back if we can get the API version but it is not compatible
	if err = cli.LoginWithAPIToken(authToken); err != nil {
		cli.Close()
		cli = nil
	} else if wrn, err = cli.CheckApiVersion(); err != nil {
		cli.Close()
		cli = nil
	} else if wrn != `` {
		cli.Close()
		cli = nil
		err = fmt.Errorf("API version mismatch: %s", wrn)
	}

	return
}

// isInteractive returns true if the program is running in an interactive shell
func isInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// getStringFromStdin reads a string from stdin
func getStringFromStdin(prompt string) (string, error) {
	fmt.Printf("Enter %s: ", prompt)
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return ``, err
	}
	if input = strings.TrimSpace(input); input == "" {
		err = errors.New("empty input")
	}
	return input, err // err may be nil here
}

// getURLFromStdin reads a URL from stdin and validates it
func getURLFromStdin() (*url.URL, error) {
	input, err := getStringFromStdin("Gravwell Host")
	if err != nil {
		return nil, err
	}
	return url.Parse(input)
}

// getToken reads a password from the shell without echoing
func getToken() (string, error) {
	fmt.Print("Enter Gravwell API Token: ")
	token, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	return string(token), nil
}

func printKitList(kbrs []types.KitBuildRequest) {
	if len(kbrs) == 0 {
		fmt.Println("No kits found")
		return
	}
	fmt.Printf("%-36s %-20s %-10s %-10s\n", "Kit ID", "Name", "Version", "Description")
	for _, kbr := range kbrs {
		fmt.Printf("%-36s %-20s %-10d %-10s\n", kbr.ID, kbr.Name, kbr.Version, kbr.Description)
	}
}

func usage() {
	fmt.Println("Usage:", os.Args[0], "<flags> <command>")
	flag.PrintDefaults()
	fmt.Println()
	fmt.Println(commandsStr)
	fmt.Println()
	fmt.Println("Environment Variables:")
	fmt.Printf("  %s\t\tGravwell host URL (-host)\n", envHost)
	fmt.Printf("  %s\tGravwell API token (-token)\n", envToken)
	fmt.Printf("  %s\tKit ID (-kit-id)\n", envKitId)
	fmt.Printf("  %s\tKit directory (-kit-dir)\n", envKitDir)
	fmt.Printf("  %s\tSet to true to deploy kits with global access (-kit-global)\n", envKitGlobal)
	fmt.Printf("  %s\tSet to true to deploy kits with global write access (-kit-write-global)\n", envKitWriteGlobal)
	fmt.Printf("  %s\tComma separated list of groups to deploy the kit to (-kit-groups)\n", envKitGroups)
	fmt.Printf("  %s\tComma separated list of groups to deploy the kit with write access (-kit-write-groups)\n", envKitWriteGroups)
	fmt.Printf("  %s\tComma separated list of labels to deploy the kit to (-kit-labels)\n", envKitLabels)
	fmt.Printf("  %s\tPath to kitctl binary (-kitctl)\n", envKitCtl)
}

func targetLabel(kitID string) string {
	return fmt.Sprintf("kit/%s", kitID)
}

func getInstallGroups(cli *client.Client) (groups []int32, err error) {
	if len(kitGroups) > 0 {
		groups, err = getGroupsFromList(cli, kitGroups)
	}
	return
}

func getInstallWriteGroups(cli *client.Client) (groups []int32, err error) {
	if len(kitWriteGroups) > 0 {
		groups, err = getGroupsFromList(cli, kitWriteGroups)
	}
	return
}

func getInstallLabels() (labels []string, err error) {
	if len(kitLabels) == 0 {
		return
	}
	// parse the labels using a CSV parser
	if labels, err = parseCSV(kitLabels); err != nil {
		err = fmt.Errorf("error parsing kit labels: %w", err)
	}
	return
}

func getGroupsFromList(cli *client.Client, kitGroups string) (groups []int32, err error) {
	var strs []string
	// parse the groups using a CSV parser
	if strs, err = parseCSV(kitGroups); err != nil {
		err = fmt.Errorf("error parsing kit groups: %w", err)
		return
	}
	if len(strs) == 0 {
		return
	}
	// go grab the group list from the remote server
	var remoteGroups []types.Group
	if remoteGroups, err = cli.GetGroups(); err != nil {
		err = fmt.Errorf("error getting remote group list: %w", err)
		return
	}

	// swing through our set of groups and lookup the gid for each group
	for _, gname := range strs {
		var gid int32
		if gid, err = getGidFromGroups(remoteGroups, gname); err != nil {
			return
		}
		groups = append(groups, gid)
	}
	return
}

// parseCSV uses a proper CSV parser to handle commas in group names
func parseCSV(v string) (groups []string, err error) {
	r := strings.NewReader(v)
	rdr := csv.NewReader(r)
	rdr.TrimLeadingSpace = true
	groups, err = rdr.Read()
	return
}

func getGidFromGroups(groups []types.Group, name string) (gid int32, err error) {
	for _, g := range groups {
		if g.Name == name {
			gid = g.ID
			return
		}
	}
	err = fmt.Errorf("group '%s' not found on remote system", name)
	return
}

func getBoolFromString(v string) bool {
	switch strings.ToLower(v) {
	case `1`, `true`, `t`, `yes`, `y`:
		return true
	default:
	}
	return false
}
