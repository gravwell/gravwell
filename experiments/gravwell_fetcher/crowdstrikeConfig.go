/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

/*
Constants used by the CrowdStrike handlers
*/
const (
	crowdstrikeEmptySleepDur = 15 * time.Second
)

/*
CrowdStrikeConf defines configuration for the CrowdStrike Falcon integrations
*/
type CrowdStrikeConf struct {
	StartTime    time.Time
	Domain       string
	Key          string
	Secret       string
	APIType      string
	AppID        string // REQUIRED for APIType=stream (alphanumeric, <=20 chars)
	Tag_Name     string
	Preprocessor []string
	RateLimit    int
}

/*
Verify CrowdStrike configuration values
*/
func (c cfgType) CrowdStrikeVerify() error {
	// Validate global preprocessor configuration once
	if err := c.Preprocessor.Validate(); err != nil {
		return err
	}

	for k, v := range c.CrowdStrikeConf {
		// Per-listener preprocessor list
		if err := c.Preprocessor.CheckProcessors(v.Preprocessor); err != nil {
			return fmt.Errorf("listener %s preprocessor invalid: %v", k, err)
		}

		// Required fields
		if strings.TrimSpace(v.Tag_Name) == "" {
			return errors.New("Tag-Name not specified")
		}
		if strings.TrimSpace(v.Domain) == "" {
			return errors.New("CrowdStrike Domain not specified")
		}
		if strings.TrimSpace(v.Key) == "" {
			return errors.New("CrowdStrike Key not specified")
		}
		if strings.TrimSpace(v.Secret) == "" {
			return errors.New("CrowdStrike Secret not specified")
		}
		if strings.TrimSpace(v.APIType) == "" {
			return errors.New("CrowdStrike APIType not specified")
		}
		if !IsValidCrowdStrikeAPI(v.APIType) {
			return fmt.Errorf("CrowdStrike APIType %q is not valid (valid: stream|detections|incidents|audit|hosts)", v.APIType)
		}

		// Stream specific requirements
		if strings.EqualFold(v.APIType, "stream") {
			if strings.TrimSpace(v.AppID) == "" {
				return fmt.Errorf("CrowdStrike AppID must be set for APIType=stream (used with /sensors/entities/datafeed/v2)")
			}
			if !validAppID(v.AppID) {
				return fmt.Errorf("CrowdStrike AppID %q invalid (must be alphanumeric, length 1–20)", v.AppID)
			}
		}

		// RateLimit
		if v.RateLimit < 0 {
			return fmt.Errorf("CrowdStrike listener %s: RateLimit must be >= 0", k)
		}
	}
	return nil
}

/*
IsValidCrowdStrikeAPI ensures APIType matches one of the supported Falcon endpoints
*/
func IsValidCrowdStrikeAPI(api string) bool {
	switch strings.ToLower(strings.TrimSpace(api)) {
	case "stream", "detections", "incidents", "audit", "hosts":
		return true
	}
	return false
}

// validAppID enforces CS guidance: alphanumeric only; 20 chars
var appIDRe = regexp.MustCompile(`^[A-Za-z0-9]{1,20}$`)

func validAppID(s string) bool {
	return appIDRe.MatchString(strings.TrimSpace(s))
}
