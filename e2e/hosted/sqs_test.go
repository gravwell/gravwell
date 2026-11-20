package hosted

import (
	"fmt"
	"testing"
	"time"

	"gravwell/e2e"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	sqsQueueName = "test"
	sqsNumEvents = 25
)

// sqsQueueURL is the URL the fetcher container config points at (via the "kumo-e2e" network alias).
// Kumo identifies queues by the URL's path, not the connecting host, so this is safe to reuse from
// the test process even though it dials Kumo's host-mapped port rather than the alias below.
var sqsQueueURL = fmt.Sprintf("http://kumo-e2e:4566/000000000000/%s", sqsQueueName)

func TestSQSPlugin(t *testing.T) {
	kumo, err := tc.Run(t.Context(), "", e2e.Kumo(t, "kumo-e2e")...)
	t.Cleanup(func() {
		e2e.Terminate(t, kumo)
	})
	if err != nil {
		e2e.Fatal(t, err)
	}

	endpoint, err := kumo.PortEndpoint(t.Context(), e2e.KumoPort, "http")
	if err != nil {
		t.Fatal(err)
	}

	awsCfg, err := e2e.KumoAWSConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	client := sqs.NewFromConfig(awsCfg, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(endpoint)
	})

	if _, err := client.CreateQueue(t.Context(), &sqs.CreateQueueInput{
		QueueName: aws.String(sqsQueueName),
	}); err != nil {
		t.Fatal(err)
	}
	for i := range sqsNumEvents {
		body := fmt.Sprintf(`{"event":%d,"source":"e2e-sqs"}`, i)
		if _, err := client.SendMessage(t.Context(), &sqs.SendMessageInput{
			QueueUrl:    aws.String(sqsQueueURL),
			MessageBody: aws.String(body),
		}); err != nil {
			t.Fatal(err)
		}
	}

	fetcher, err := tc.Run(t.Context(), "gravwell/hosted:e2e",
		e2e.WithDefaults(t, "hosted-sqs",
			tc.WithWaitStrategyAndDeadline(
				35*time.Second,
				wait.ForLog("Successfully connected to ingesters").WithPollInterval(time.Second),
			),
			e2e.WithConfig(t, "testdata/sqs.conf", "hosted_runner.conf", e2e.DefaultConfig),
		)...,
	)
	t.Cleanup(func() {
		e2e.SaveTestFiles(t, fetcher, e2e.Log, []string{
			"/opt/gravwell/log/hosted_runner.log",
		})
		e2e.Terminate(t, fetcher)
	})
	if err != nil {
		e2e.Fatal(t, err)
	}

	c := e2e.GetClient(t)
	if ent := e2e.WaitForEntries(t, c, "tag=sqs", time.Hour, sqsNumEvents, 60*time.Second); len(ent) < sqsNumEvents {
		e2e.Fatalf(t, "got %d entries, less than expected %d", len(ent), sqsNumEvents)
	}

	t.Run("Messages deleted after ingest", func(t *testing.T) {
		out, err := client.ReceiveMessage(t.Context(), &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(sqsQueueURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     2,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Messages) != 0 {
			t.Fatalf("expected queue to be drained after ingest, found %d leftover message(s)", len(out.Messages))
		}
	})
}
