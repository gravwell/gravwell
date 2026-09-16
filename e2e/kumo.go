package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// KumoImage is the Kumo AWS emulator (S3, SQS, Kinesis) image, pinned to match
	// ingesters/examples/aws/docker-compose.yaml.
	KumoImage = "ghcr.io/sivchari/kumo@sha256:e63054fbe10eb17b0c9142e937e11b3f4ee2709ac1c80035f3220542f3e5b045" // v0.26.0

	// KumoPort is the port Kumo listens on for all emulated AWS services.
	KumoPort = "4566/tcp"

	// Kumo doesn't validate credentials, but the AWS SDK still requires something be set.
	KumoRegion    = "us-east-1"
	KumoAccessKey = "kumo_access_key"
	KumoSecretKey = "kumo_secret_key"
)

// Kumo returns container customizers for running the Kumo AWS emulator on the shared test
// network. Other containers on the same network reach it at "<name>:4566".
//
//	kumo, err := tc.Run(t.Context(), "", e2e.Kumo(t, "kumo")...)
func Kumo(t *testing.T, name string, extras ...tc.ContainerCustomizer) []tc.ContainerCustomizer {
	defaults := []tc.ContainerCustomizer{
		tc.WithImage(KumoImage),
		tc.WithExposedPorts(KumoPort),
		tc.WithWaitStrategyAndDeadline(
			30*time.Second,
			wait.ForListeningPort(KumoPort).WithPollInterval(time.Second),
		),
	}
	return WithDefaults(t, name, append(defaults, extras...)...)
}

// KumoAWSConfig returns an aws.Config using Kumo's fixed dummy credentials. Build whichever
// service client you need from it, applying the endpoint override yourself (obtained via
// container.PortEndpoint(ctx, e2e.KumoPort, "http") from the test process, or the "<name>:4566"
// network alias from another container), e.g.:
//
//	cfg, err := e2e.KumoAWSConfig(ctx)
//	client := sqs.NewFromConfig(cfg, func(o *sqs.Options) { o.BaseEndpoint = aws.String(endpoint) })
func KumoAWSConfig(ctx context.Context) (aws.Config, error) {
	return awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(KumoRegion),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(KumoAccessKey, KumoSecretKey, "")),
	)
}
