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
	"errors"
	"fmt"
	"strings"

	azidentity "github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	msgraphsdkgo "github.com/microsoftgraph/msgraph-sdk-go"
	"github.com/microsoftgraph/msgraph-sdk-go/models"
	modelsecurity "github.com/microsoftgraph/msgraph-sdk-go/models/security"
	graphsecurity "github.com/microsoftgraph/msgraph-sdk-go/security"
)

type msGraphFetcher interface {
	ListAlerts(ctx context.Context, filter string) ([]modelsecurity.Alertable, error)
	ListSecureScores(ctx context.Context) ([]models.SecureScoreable, error)
	ListSecureScoreControlProfiles(ctx context.Context) ([]models.SecureScoreControlProfileable, error)
}

type msGraphConfig struct {
	clientID     string
	clientSecret string
	tenantDomain string
	tenantID     string
}

type msGraphClient struct {
	client *msgraphsdkgo.GraphServiceClient
}

func newGraphClient(ctx context.Context, cfg msGraphConfig) (*msGraphClient, error) {
	tenant := cmp.Or(strings.TrimSpace(cfg.tenantID), strings.TrimSpace(cfg.tenantDomain))
	if tenant == "" {
		return nil, errors.New("either Tenant-ID or Tenant-Domain must be provided")
	}

	cred, err := azidentity.NewClientSecretCredential(tenant, cfg.clientID, cfg.clientSecret, nil)
	if err != nil {
		return nil, fmt.Errorf("create credentials: %w", err)
	}

	sdkClient, err := msgraphsdkgo.NewGraphServiceClientWithCredentials(
		cred,
		[]string{"https://graph.microsoft.com/.default"},
	)
	if err != nil {
		return nil, fmt.Errorf("create msgraph client: %w", err)
	}

	return &msGraphClient{client: sdkClient}, nil
}

var _ msGraphFetcher = (*msGraphClient)(nil)

func (g *msGraphClient) ListAlerts(
	ctx context.Context,
	filter string,
) ([]modelsecurity.Alertable, error) {
	var requestConfig *graphsecurity.Alerts_v2RequestBuilderGetRequestConfiguration
	if filter != "" {
		requestConfig = &graphsecurity.Alerts_v2RequestBuilderGetRequestConfiguration{
			QueryParameters: &graphsecurity.Alerts_v2RequestBuilderGetQueryParameters{
				Filter: &filter,
			},
		}
	}

	resp, err := g.client.Security().Alerts_v2().Get(ctx, requestConfig)
	if err != nil {
		return nil, fmt.Errorf("get initial alerts: %w", err)
	}

	alerts := resp.GetValue()
	for resp.GetOdataNextLink() != nil && *resp.GetOdataNextLink() != "" {
		resp, err = g.client.Security().Alerts_v2().WithUrl(*resp.GetOdataNextLink()).Get(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("get alerts page: %w", err)
		}
		alerts = append(alerts, resp.GetValue()...)
	}

	return alerts, nil
}

func (g *msGraphClient) ListSecureScores(ctx context.Context) ([]models.SecureScoreable, error) {
	resp, err := g.client.Security().SecureScores().Get(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("get initial secure scores: %w", err)
	}

	scores := resp.GetValue()
	for resp.GetOdataNextLink() != nil && *resp.GetOdataNextLink() != "" {
		resp, err = g.client.Security().SecureScores().WithUrl(*resp.GetOdataNextLink()).Get(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("get secure scores page: %w", err)
		}
		scores = append(scores, resp.GetValue()...)
	}

	return scores, nil
}

func (g *msGraphClient) ListSecureScoreControlProfiles(ctx context.Context) ([]models.SecureScoreControlProfileable, error) {
	resp, err := g.client.Security().SecureScoreControlProfiles().Get(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("get initial secure score control profiles: %w", err)
	}

	profiles := resp.GetValue()
	for resp.GetOdataNextLink() != nil && *resp.GetOdataNextLink() != "" {
		resp, err = g.client.Security().SecureScoreControlProfiles().WithUrl(*resp.GetOdataNextLink()).Get(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("get secure score control profiles page: %w", err)
		}
		profiles = append(profiles, resp.GetValue()...)
	}

	return profiles, nil
}
