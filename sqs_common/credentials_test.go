/*************************************************************************
 * Copyright 2026 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package sqs_common

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testRoleARN = "arn:aws:iam::123456789012:role/gravwell-ingest"

func TestCredentialsConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		conf    CredentialsConfig
		wantErr string // empty means no error; otherwise a substring of the error
	}{
		// existing types behave as GetCredentials does
		{"static valid", CredentialsConfig{Type: "static", ID: "id", Secret: "secret"}, ""},
		{"static missing id", CredentialsConfig{Type: "static", Secret: "secret"}, "missing ID"},
		{"static missing secret", CredentialsConfig{Type: "static", ID: "id"}, "missing secret"},
		{"default (static) valid", CredentialsConfig{ID: "id", Secret: "secret"}, ""},
		{"default (static) missing both", CredentialsConfig{}, "missing ID"},
		{"environment", CredentialsConfig{Type: "environment"}, ""},
		{"environment ignores id/secret", CredentialsConfig{Type: "environment", ID: "id"}, ""},
		{"ec2role", CredentialsConfig{Type: "ec2role"}, ""},
		{"invalid type", CredentialsConfig{Type: "foobar"}, `invalid Credentials-Type "foobar"`},
		{"region and session name ignored for static", CredentialsConfig{Type: "static", ID: "id", Secret: "secret", Region: "us-east-1", SessionName: "gravwell-s3"}, ""},

		// new keys require assumerole
		{"static with Role-ARN", CredentialsConfig{Type: "static", ID: "id", Secret: "secret", RoleARN: testRoleARN}, "Role-ARN requires Credentials-Type=assumerole"},
		{"no type with Role-ARN and no keys", CredentialsConfig{RoleARN: testRoleARN}, "Role-ARN requires Credentials-Type=assumerole"},
		{"default with Role-ARN", CredentialsConfig{ID: "id", Secret: "secret", RoleARN: testRoleARN}, "Role-ARN requires Credentials-Type=assumerole"},
		{"environment with External-ID", CredentialsConfig{Type: "environment", ExternalID: "ext-id"}, "External-ID requires Credentials-Type=assumerole"},
		{"ec2role with Source-Credentials-Type", CredentialsConfig{Type: "ec2role", SourceType: "static"}, "Source-Credentials-Type requires Credentials-Type=assumerole"},

		// assumerole: Role-ARN
		{"assumerole minimal", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN}, ""},
		{"assumerole role path", CredentialsConfig{Type: "assumerole", RoleARN: "arn:aws:iam::123456789012:role/path/to/name"}, ""},
		{"assumerole govcloud partition", CredentialsConfig{Type: "assumerole", RoleARN: "arn:aws-us-gov:iam::123456789012:role/name"}, ""},
		{"assumerole missing Role-ARN", CredentialsConfig{Type: "assumerole"}, "requires Role-ARN"},
		{"assumerole garbage Role-ARN", CredentialsConfig{Type: "assumerole", RoleARN: "not-an-arn"}, "invalid Role-ARN"},
		{"assumerole wrong service", CredentialsConfig{Type: "assumerole", RoleARN: "arn:aws:s3:::my-bucket"}, "not an IAM role ARN"},
		{"assumerole user not role", CredentialsConfig{Type: "assumerole", RoleARN: "arn:aws:iam::123456789012:user/bob"}, "not an IAM role ARN"},
		{"assumerole missing account id", CredentialsConfig{Type: "assumerole", RoleARN: "arn:aws:iam:::role/gravwell-ingest"}, "not an IAM role ARN"},
		{"assumerole short account id", CredentialsConfig{Type: "assumerole", RoleARN: "arn:aws:iam::12345:role/gravwell-ingest"}, "not an IAM role ARN"},
		{"assumerole region in arn", CredentialsConfig{Type: "assumerole", RoleARN: "arn:aws:iam:us-east-1:123456789012:role/gravwell-ingest"}, "not an IAM role ARN"},
		{"mistyped assumerole", CredentialsConfig{Type: "AssumeRole", RoleARN: testRoleARN}, `invalid Credentials-Type "AssumeRole"`},
		{"assumerole empty role name", CredentialsConfig{Type: "assumerole", RoleARN: "arn:aws:iam::123456789012:role/"}, "not an IAM role ARN"},

		// assumerole: External-ID
		{"external id valid", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, ExternalID: "a_B9+=,.@:/-z"}, ""},
		{"external id min length", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, ExternalID: "ab"}, ""},
		{"external id max length", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, ExternalID: strings.Repeat("x", 1224)}, ""},
		{"external id too short", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, ExternalID: "a"}, "invalid External-ID"},
		{"external id too long", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, ExternalID: strings.Repeat("x", 1225)}, "invalid External-ID"},
		{"external id space", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, ExternalID: "has space"}, "invalid External-ID"},
		{"external id bad char", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, ExternalID: "bad!char"}, "invalid External-ID"},

		// assumerole: source credentials
		{"source static", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, SourceType: "static", ID: "id", Secret: "secret"}, ""},
		{"source static missing secret", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, SourceType: "static", ID: "id"}, "missing secret"},
		{"source static missing id", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, SourceType: "static", Secret: "secret"}, "missing ID"},
		{"source static missing keys", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, SourceType: "static"}, "missing ID"},
		{"source environment", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, SourceType: "environment"}, ""},
		{"source ec2role", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, SourceType: "ec2role"}, ""},
		{"keys without source type", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, ID: "id", Secret: "secret"}, "access keys are set but not used"},
		{"id only without source type", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, ID: "id"}, "access keys are set but not used"},
		{"secret only without source type", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, Secret: "secret"}, "access keys are set but not used"},
		{"environment source with id", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, SourceType: "environment", ID: "id"}, "access keys are set but not used"},
		{"ec2role source with keys", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, SourceType: "ec2role", ID: "id", Secret: "secret"}, "access keys are set but not used"},
		{"source assumerole rejected", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, SourceType: "assumerole"}, `invalid Source-Credentials-Type "assumerole"`},
		{"source invalid", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, SourceType: "foobar"}, `invalid Source-Credentials-Type "foobar"`},

		// assumerole: session name
		{"session name custom", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, SessionName: "gravwell-s3"}, ""},
		{"session name too short", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, SessionName: "a"}, "invalid role session name"},
		{"session name bad char", CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, SessionName: "bad/name"}, "invalid role session name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.conf.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestCredentialsConfig_sourceType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		conf CredentialsConfig
		want string
	}{
		{"default", CredentialsConfig{}, CredentialsTypeEnvironment},
		{"keys do not select static", CredentialsConfig{ID: "id", Secret: "secret"}, CredentialsTypeEnvironment},
		{"explicit static", CredentialsConfig{SourceType: "static", ID: "id", Secret: "secret"}, CredentialsTypeStatic},
		{"explicit ec2role", CredentialsConfig{SourceType: "ec2role"}, CredentialsTypeEC2Role},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.conf.sourceType())
		})
	}
}

func TestCredentialsConfig_Provider_MatchesGetCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		ctype  string
		akid   string
		secret string
	}{
		{"static valid", "static", "id", "secret"},
		{"static missing id", "static", "", "secret"},
		{"static missing secret", "static", "id", ""},
		{"environment", "environment", "", ""},
		{"ec2 role", "ec2role", "", ""},
		{"invalid type", "foobar", "", ""},
		{"default (static)", "", "id", "secret"},
		{"default (static) missing both", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			want, wantErr := GetCredentials(tt.ctype, tt.akid, tt.secret)
			got, gotErr := CredentialsConfig{
				Type:        tt.ctype,
				ID:          tt.akid,
				Secret:      tt.secret,
				Region:      "us-east-1",
				SessionName: "gravwell-test",
			}.Provider(t.Context())
			assert.Equal(t, wantErr, gotErr)
			assert.Equal(t, want == nil, got == nil)
		})
	}
}

func TestCredentialsConfig_Provider_RejectsRoleSettings(t *testing.T) {
	t.Parallel()
	p, err := CredentialsConfig{Type: "static", ID: "id", Secret: "secret", RoleARN: testRoleARN}.Provider(t.Context())
	require.Error(t, err)
	assert.Nil(t, p)
	assert.Contains(t, err.Error(), "Role-ARN requires Credentials-Type=assumerole")
}

type fakeSTS struct {
	mu     sync.Mutex
	ttl    time.Duration
	err    error
	inputs []*sts.AssumeRoleInput
}

var _ stscreds.AssumeRoleAPIClient = (*fakeSTS)(nil)

func (f *fakeSTS) AssumeRole(_ context.Context, in *sts.AssumeRoleInput, _ ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputs = append(f.inputs, in)
	if f.err != nil {
		return nil, f.err
	}
	return &sts.AssumeRoleOutput{
		Credentials: &ststypes.Credentials{
			AccessKeyId:     new("ASIAFAKE"),
			SecretAccessKey: new("fake-secret"),
			SessionToken:    new("fake-token"),
			Expiration:      new(time.Now().Add(f.ttl)),
		},
	}, nil
}

func (f *fakeSTS) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeSTS) calls() []*sts.AssumeRoleInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.inputs)
}

func TestCredentialsConfig_assumeRoleProvider_Input(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		conf            CredentialsConfig
		wantSessionName string
		wantExternalID  *string
	}{
		{
			name:            "defaults",
			conf:            CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN},
			wantSessionName: "gravwell-ingester",
			wantExternalID:  nil,
		},
		{
			name:            "custom session name and external id",
			conf:            CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, ExternalID: "some-id", SessionName: "gravwell-s3"},
			wantSessionName: "gravwell-s3",
			wantExternalID:  new("some-id"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeSTS{ttl: time.Hour}
			creds, err := tt.conf.assumeRoleProvider(fake).Retrieve(t.Context())
			require.NoError(t, err)
			assert.Equal(t, "ASIAFAKE", creds.AccessKeyID)
			assert.Equal(t, "fake-token", creds.SessionToken)

			calls := fake.calls()
			require.Len(t, calls, 1)
			in := calls[0]
			assert.Equal(t, testRoleARN, *in.RoleArn)
			assert.Equal(t, tt.wantSessionName, *in.RoleSessionName)
			assert.Equal(t, tt.wantExternalID, in.ExternalId)
			require.NotNil(t, in.DurationSeconds)
			assert.EqualValues(t, 3600, *in.DurationSeconds)
		})
	}
}

func TestCredentialsConfig_assumeRoleProvider_Refresh(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		ttl       time.Duration
		wantCalls int
	}{
		// The cache treats credentials as expired 2.5-5 minutes early.
		{"inside expiry window refreshes", time.Minute, 2},
		{"unexpired served from cache", time.Hour, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeSTS{ttl: tt.ttl}
			p := CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN}.assumeRoleProvider(fake)
			for range 2 {
				_, err := p.Retrieve(t.Context())
				require.NoError(t, err)
			}
			assert.Len(t, fake.calls(), tt.wantCalls)
		})
	}
}

func TestCredentialsConfig_assumeRoleProvider_RefreshFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ttl     time.Duration // both are inside the expiry window, so every Retrieve refreshes
		wantErr bool
	}{
		{"still valid credentials are kept", time.Minute, false},
		{"nearly expired credentials are not", time.Second, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeSTS{ttl: tt.ttl}
			p := CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN}.assumeRoleProvider(fake)
			first, err := p.Retrieve(t.Context())
			require.NoError(t, err)

			fake.setErr(errors.New("ServiceUnavailable"))
			got, err := p.Retrieve(t.Context())
			assert.Len(t, fake.calls(), 2)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "ServiceUnavailable")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, first.AccessKeyID, got.AccessKeyID)
			assert.Equal(t, first.SessionToken, got.SessionToken)

			// once STS is back the next call refreshes
			fake.setErr(nil)
			_, err = p.Retrieve(t.Context())
			require.NoError(t, err)
			assert.Len(t, fake.calls(), 3)
		})
	}
}

func TestCredentialsConfig_CheckAssumeRole(t *testing.T) {
	t.Parallel()

	conf := CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, ExternalID: "some-id"}

	t.Run("ok", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSTS{ttl: time.Hour}
		require.NoError(t, conf.CheckAssumeRole(t.Context(), conf.assumeRoleProvider(fake)))
		assert.Len(t, fake.calls(), 1)
	})

	t.Run("denied", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSTS{err: errors.New("AccessDenied")}
		err := conf.CheckAssumeRole(t.Context(), conf.assumeRoleProvider(fake))
		require.Error(t, err)
		assert.Contains(t, err.Error(), testRoleARN)
		assert.Contains(t, err.Error(), "AccessDenied")
		assert.NotContains(t, err.Error(), "some-id")
	})

	t.Run("other types make no call", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSTS{ttl: time.Hour}
		static := CredentialsConfig{Type: "static", ID: "id", Secret: "secret"}
		require.NoError(t, static.CheckAssumeRole(t.Context(), conf.assumeRoleProvider(fake)))
		require.NoError(t, CredentialsConfig{Type: "environment"}.CheckAssumeRole(t.Context(), nil))
		assert.Empty(t, fake.calls())
	})
}

// isolateAWSEnv keeps the developer's AWS environment and shared config out of the test.
func isolateAWSEnv(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config")
	credsFile := filepath.Join(dir, "credentials")
	require.NoError(t, os.WriteFile(cfgFile, nil, 0600))
	require.NoError(t, os.WriteFile(credsFile, nil, 0600))
	t.Setenv("AWS_CONFIG_FILE", cfgFile)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credsFile)
	for _, k := range []string{"AWS_REGION", "AWS_DEFAULT_REGION", "AWS_PROFILE", "AWS_DEFAULT_PROFILE"} {
		t.Setenv(k, "")
	}
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
}

// Not parallel: t.Setenv.
func TestCredentialsConfig_Provider_AssumeRole(t *testing.T) {
	isolateAWSEnv(t)

	base := CredentialsConfig{Type: "assumerole", RoleARN: testRoleARN, SourceType: "static", ID: "id", Secret: "secret"}

	t.Run("no region", func(t *testing.T) {
		p, err := base.Provider(t.Context())
		require.Error(t, err)
		assert.Nil(t, p)
		assert.Contains(t, err.Error(), "region")
	})

	t.Run("region set", func(t *testing.T) {
		c := base
		c.Region = "us-east-1"
		p, err := c.Provider(t.Context())
		require.NoError(t, err)
		assert.NotNil(t, p)
	})

	t.Run("invalid config", func(t *testing.T) {
		c := base
		c.Region = "us-east-1"
		c.RoleARN = ""
		p, err := c.Provider(t.Context())
		require.Error(t, err)
		assert.Nil(t, p)
	})
}
