/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/gravwell/gravwell/v3/hosted/plugins/msgraph"
)

type msGraphFetcher interface {
	ListAlerts(ctx context.Context, filter string) ([]json.RawMessage, error)
	ListSecureScores(ctx context.Context) ([]json.RawMessage, error)
	ListSecureScoreControlProfiles(ctx context.Context) ([]json.RawMessage, error)
}

type msGraphConfig struct {
	clientID     string
	clientSecret string
	tenantDomain string
	tenantID     string
}

type msGraphClientWrapper struct {
	client *msgraph.Client
}

func newGraphClient(cfg msGraphConfig, httpClient msgraph.Doer) (*msGraphClientWrapper, error) {
	tenant := cmp.Or(strings.TrimSpace(cfg.tenantID), strings.TrimSpace(cfg.tenantDomain))
	if tenant == "" {
		return nil, errors.New("either Tenant-ID or Tenant-Domain must be provided")
	}

	client := msgraph.NewClient(
		"https://graph.microsoft.com",
		"https://login.microsoftonline.com",
		tenant,
		cfg.clientID,
		cfg.clientSecret,
		httpClient,
	)

	return &msGraphClientWrapper{client: client}, nil
}

var _ msGraphFetcher = (*msGraphClientWrapper)(nil)

func (api *msGraphClientWrapper) ListAlerts(ctx context.Context, filter string) ([]json.RawMessage, error) {
	var params url.Values
	if filter != "" {
		params = url.Values{}
		params.Set("$filter", filter)
	}

	return api.client.ListAll(ctx, msgraph.AlertsEndpoint, params)
}

func (api *msGraphClientWrapper) ListSecureScores(ctx context.Context) ([]json.RawMessage, error) {
	return api.client.ListAll(ctx, msgraph.SecureScoresEndpoint, nil)
}

func (api *msGraphClientWrapper) ListSecureScoreControlProfiles(ctx context.Context) ([]json.RawMessage, error) {
	return api.client.ListAll(ctx, msgraph.ControlProfilesEndpoint, nil)
}
