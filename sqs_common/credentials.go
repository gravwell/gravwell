/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package sqs_common

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/gravwell/gravwell/v3/ingest/log"
)

// Credentials-Type values.
const (
	CredentialsTypeStatic      = "static"
	CredentialsTypeEnvironment = "environment"
	CredentialsTypeEC2Role     = "ec2role"
	CredentialsTypeAssumeRole  = "assumerole"
)

const (
	defaultRoleSessionName = "gravwell-ingester"
	// 1h is the STS cap for role chaining (e.g. ec2role source credentials)
	// and the minimum MaxSessionDuration of any role, so it is always accepted.
	assumeRoleDuration     = time.Hour
	assumeRoleExpiryWindow = 5 * time.Minute
	assumeRoleJitterFrac   = 0.5
	assumeRoleCheckTimeout = 15 * time.Second
	// below this, credentials are not reused after a failed refresh
	assumeRoleMinRemaining = 10 * time.Second
	// how often STS is retried while reused credentials are still valid
	assumeRoleRetryInterval = 30 * time.Second
	minExternalIDLen        = 2
	maxExternalIDLen        = 1224
	roleResourcePrefix      = "role/"
)

var (
	// Length is checked separately: Go's regexp caps repeat counts at 1000.
	externalIDRe  = regexp.MustCompile(`^[\w+=,.@:/-]+$`)
	sessionNameRe = regexp.MustCompile(`^[\w+=,.@-]{2,64}$`)
	accountIDRe   = regexp.MustCompile(`^\d{12}$`)
)

// CredentialsConfig describes how an AWS ingester obtains credentials.
type CredentialsConfig struct {
	Type        string // static (default when empty), environment, ec2role, assumerole
	ID          string
	Secret      string
	RoleARN     string // assumerole only
	ExternalID  string // assumerole only, optional
	SourceType  string // assumerole only, optional: environment (default), static, ec2role
	Region      string // region for the STS call (assumerole only)
	SessionName string // assumerole only; defaults to "gravwell-ingester"

	// Logger is optional (assumerole only): it warns when a refresh fails.
	Logger *log.Logger
}

// Validate checks the configuration without contacting AWS.
func (c CredentialsConfig) Validate() error {
	if c.Type != CredentialsTypeAssumeRole {
		// An unknown type (e.g. a mistyped assumerole) is reported as such by
		// GetCredentials; for a known type the role settings are the mistake.
		switch c.Type {
		case "", CredentialsTypeStatic, CredentialsTypeEnvironment, CredentialsTypeEC2Role:
			if err := c.checkNoRoleSettings(); err != nil {
				return err
			}
		}
		_, err := GetCredentials(c.Type, c.ID, c.Secret)
		return err
	}

	if c.RoleARN == "" {
		return errors.New("Credentials-Type=assumerole requires Role-ARN")
	}
	a, err := arn.Parse(c.RoleARN)
	if err != nil {
		return fmt.Errorf("invalid Role-ARN %q: %w", c.RoleARN, err)
	}
	if a.Service != "iam" || a.Region != "" || !accountIDRe.MatchString(a.AccountID) || !strings.HasPrefix(a.Resource, roleResourcePrefix) || len(a.Resource) == len(roleResourcePrefix) {
		return fmt.Errorf("invalid Role-ARN %q: not an IAM role ARN", c.RoleARN)
	}

	if c.ExternalID != "" {
		if l := len(c.ExternalID); l < minExternalIDLen || l > maxExternalIDLen || !externalIDRe.MatchString(c.ExternalID) {
			return fmt.Errorf("invalid External-ID: must be %d-%d characters of letters, digits and _+=,.@:/-", minExternalIDLen, maxExternalIDLen)
		}
	}

	switch c.SourceType {
	case "", CredentialsTypeStatic, CredentialsTypeEnvironment, CredentialsTypeEC2Role:
	default:
		return fmt.Errorf("invalid Source-Credentials-Type %q", c.SourceType)
	}
	// Keys are only used when asked for, so leftover or placeholder keys fail here and not at STS.
	if c.SourceType != CredentialsTypeStatic && (c.ID != "" || c.Secret != "") {
		return errors.New("access keys are set but not used with Credentials-Type=assumerole: set Source-Credentials-Type=static to call STS with them, or remove them")
	}
	if _, err := GetCredentials(c.sourceType(), c.ID, c.Secret); err != nil {
		return fmt.Errorf("assumerole source credentials: %w", err)
	}

	if c.SessionName != "" && !sessionNameRe.MatchString(c.SessionName) {
		return fmt.Errorf("invalid role session name %q", c.SessionName)
	}
	return nil
}

// CheckRegion reports at config load what Provider would at startup:
// assumerole needs a region for STS, from Region or the AWS environment.
// It is separate from Validate because it reads the host's AWS environment.
func (c CredentialsConfig) CheckRegion() error {
	if c.Type != CredentialsTypeAssumeRole || c.Region != "" {
		return nil
	}
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		return fmt.Errorf("load AWS config for assumerole: %w", err)
	}
	if cfg.Region == "" {
		return errors.New("Credentials-Type=assumerole requires Region (or AWS_REGION) for the STS AssumeRole call")
	}
	return nil
}

// Provider returns the credentials provider for c. A nil provider with a nil
// error means the caller should use the SDK default credential chain.
// For assumerole the STS call is deferred until credentials are first retrieved.
func (c CredentialsConfig) Provider(ctx context.Context) (aws.CredentialsProvider, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if c.Type != CredentialsTypeAssumeRole {
		return GetCredentials(c.Type, c.ID, c.Secret)
	}

	src, err := GetCredentials(c.sourceType(), c.ID, c.Secret)
	if err != nil {
		return nil, err
	}

	var opts []func(*config.LoadOptions) error
	if c.Region != "" {
		opts = append(opts, config.WithRegion(c.Region))
	}
	if src != nil {
		opts = append(opts, config.WithCredentialsProvider(src))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config for assumerole: %w", err)
	}
	if cfg.Region == "" {
		return nil, errors.New("assumerole requires a region for the STS AssumeRole call")
	}
	return c.assumeRoleProvider(sts.NewFromConfig(cfg)), nil
}

func (c CredentialsConfig) assumeRoleProvider(client stscreds.AssumeRoleAPIClient) *aws.CredentialsCache {
	p := stscreds.NewAssumeRoleProvider(client, c.RoleARN, func(o *stscreds.AssumeRoleOptions) {
		o.RoleSessionName = cmp.Or(c.SessionName, defaultRoleSessionName)
		o.Duration = assumeRoleDuration
		if c.ExternalID != "" {
			o.ExternalID = new(c.ExternalID)
		}
	})
	return aws.NewCredentialsCache(&assumeRoleRefresher{AssumeRoleProvider: p, lg: c.Logger, roleARN: c.RoleARN}, func(o *aws.CredentialsCacheOptions) {
		o.ExpiryWindow = assumeRoleExpiryWindow
		o.ExpiryWindowJitterFrac = assumeRoleJitterFrac
	})
}

// assumeRoleRefresher keeps the current role credentials in use when an early
// refresh fails. Without it, the cache's expiry window turns a short STS outage
// into failed requests while the credentials are still valid.
// The cache serializes its calls into the refresher, so the fields need no lock.
type assumeRoleRefresher struct {
	*stscreds.AssumeRoleProvider
	last     aws.Credentials
	fallback bool // the credentials about to be cached are a reuse of last
	lg       *log.Logger
	roleARN  string
}

func (p *assumeRoleRefresher) Retrieve(ctx context.Context) (aws.Credentials, error) {
	creds, err := p.AssumeRoleProvider.Retrieve(ctx)
	if err == nil {
		p.last = creds
		p.fallback = false
	}
	return creds, err
}

// HandleFailToRefresh implements aws.HandleFailRefreshCredentialsCacheStrategy.
func (p *assumeRoleRefresher) HandleFailToRefresh(_ context.Context, _ aws.Credentials, err error) (aws.Credentials, error) {
	if !p.last.HasKeys() || time.Until(p.last.Expires) <= assumeRoleMinRemaining {
		return aws.Credentials{}, err
	}
	fb := p.last
	// Try STS again shortly, but stop using the credentials before they really expire.
	fb.Expires = time.Now().Add(assumeRoleRetryInterval)
	if limit := p.last.Expires.Add(-assumeRoleMinRemaining); limit.Before(fb.Expires) {
		fb.Expires = limit
	}
	p.fallback = true
	if p.lg != nil {
		_ = p.lg.Warn("assume role refresh failed, reusing current credentials until they expire",
			log.KV("role", p.roleARN), log.KV("expires", p.last.Expires), log.KVErr(err))
	}
	return fb, nil
}

// AdjustExpiresBy implements aws.AdjustExpiresByCredentialsCacheStrategy.
// Fallback credentials already carry the time to retry STS, so the cache's
// expiry window is not taken off them a second time.
func (p *assumeRoleRefresher) AdjustExpiresBy(c aws.Credentials, d time.Duration) (aws.Credentials, error) {
	if p.fallback {
		p.fallback = false
		return c, nil
	}
	c.Expires = c.Expires.Add(d)
	return c, nil
}

// sourceType is the credential type used to call STS for assumerole.
func (c CredentialsConfig) sourceType() string {
	return cmp.Or(c.SourceType, CredentialsTypeEnvironment)
}

// CheckAssumeRole retrieves credentials from p once, so that a wrong Role-ARN,
// External-ID or trust policy is reported at startup and not on first use.
// It does nothing for other credential types.
func (c CredentialsConfig) CheckAssumeRole(ctx context.Context, p aws.CredentialsProvider) error {
	if c.Type != CredentialsTypeAssumeRole || p == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, assumeRoleCheckTimeout)
	defer cancel()
	if _, err := p.Retrieve(ctx); err != nil {
		return fmt.Errorf("assume role %s (check Role-ARN, External-ID and the role's trust policy): %w", c.RoleARN, err)
	}
	return nil
}

func (c CredentialsConfig) checkNoRoleSettings() error {
	switch {
	case c.RoleARN != "":
		return errors.New("Role-ARN requires Credentials-Type=assumerole")
	case c.ExternalID != "":
		return errors.New("External-ID requires Credentials-Type=assumerole")
	case c.SourceType != "":
		return errors.New("Source-Credentials-Type requires Credentials-Type=assumerole")
	}
	return nil
}
