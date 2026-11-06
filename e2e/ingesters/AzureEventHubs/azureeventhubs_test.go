package AzureEventHubs

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"gravwell/e2e"

	eventhubs "github.com/Azure/azure-sdk-for-go/sdk/messaging/azeventhubs/v2"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// These must match testdata/azureeventhubs.conf and testdata/eventhubs-config.json.
const (
	eventHubName = "test"
	tokenName    = "RootManageSharedAccessKey"
	tokenKey     = "emulatorSasKeyNotForProduction=="
	tag          = "azureeventhubs"
)

// startEmulator brings up Azurite (the emulator's required metadata/blob backend) and the
// Event Hubs emulator itself, mirroring ingesters/examples/azureeventhubs/docker-compose.yaml.
// The emulator's AMQP port is published to the host so the test can act as a producer
// directly; the ingester instead reaches both containers by their in-network alias
// ("azurite"/"emulator"), same as any other container-to-container traffic in this framework.
func startEmulator(t *testing.T) *tc.DockerContainer {
	t.Helper()

	azurite, err := tc.Run(t.Context(), "",
		e2e.WithDefaults(t, "azurite",
			tc.WithImage("mcr.microsoft.com/azure-storage/azurite:latest"),
			tc.WithWaitStrategyAndDeadline(30*time.Second,
				wait.ForListeningPort("10000/tcp").SkipExternalCheck().WithPollInterval(time.Second),
			),
		)...,
	)
	t.Cleanup(func() { e2e.Terminate(t, azurite) })
	if err != nil {
		e2e.Fatal(t, err)
	}

	emulator, err := tc.Run(t.Context(), "",
		e2e.WithDefaults(t, "emulator",
			tc.WithImage("mcr.microsoft.com/azure-messaging/eventhubs-emulator:latest"),
			tc.WithExposedPorts("5672/tcp"),
			tc.WithEnv(map[string]string{
				"BLOB_SERVER":     "azurite",
				"METADATA_SERVER": "azurite",
				"ACCEPT_EULA":     "Y",
			}),
			tc.WithFiles(tc.ContainerFile{
				HostFilePath:      "testdata/eventhubs-config.json",
				ContainerFilePath: "/Eventhubs_Emulator/ConfigFiles/Config.json",
				FileMode:          0o644,
			}),
			// The emulator retries its own startup health check internally until Azurite
			// is reachable, so this can take a bit longer than a typical container.
			tc.WithWaitStrategyAndDeadline(90*time.Second,
				wait.ForLog("Emulator Service is Successfully Up").WithPollInterval(time.Second),
			),
		)...,
	)
	t.Cleanup(func() { e2e.Terminate(t, emulator) })
	if err != nil {
		e2e.Fatal(t, err)
	}

	return emulator
}

// sendEvents connects directly to the emulator (over the host-mapped AMQP port) and sends
// one event per message to the "test" hub.
func sendEvents(t *testing.T, endpoint string, messages []string) {
	t.Helper()
	ctx := context.Background()

	connStr := fmt.Sprintf(
		"Endpoint=sb://%s;SharedAccessKeyName=%s;SharedAccessKey=%s;UseDevelopmentEmulator=true;",
		endpoint, tokenName, tokenKey,
	)
	client, err := eventhubs.NewProducerClientFromConnectionString(connStr, eventHubName, nil)
	if err != nil {
		e2e.Fatalf(t, "failed to create producer client: %v", err)
	}
	defer client.Close(context.Background())

	batch, err := client.NewEventDataBatch(ctx, nil)
	if err != nil {
		e2e.Fatalf(t, "failed to create event batch: %v", err)
	}
	for _, m := range messages {
		if err := batch.AddEventData(&eventhubs.EventData{Body: []byte(m)}, nil); err != nil {
			e2e.Fatalf(t, "failed to add event to batch: %v", err)
		}
	}
	if err := client.SendEventDataBatch(ctx, batch, nil); err != nil {
		e2e.Fatalf(t, "failed to send event batch: %v", err)
	}
}

// TestIngest covers the happy path end to end: the ingester connects to a local Event Hubs
// emulator via the Event-Hubs-Endpoint override, and events produced into the hub show up in
// Gravwell under the configured tag.
//
// Follow-ups for more thorough coverage, not attempted here:
//   - Initial-Checkpoint=end (only reading events produced after the ingester starts)
//   - checkpoint persistence across an ingester restart (no duplicate/lost events)
//   - multiple partitions and/or multiple consumer groups
//   - preprocessor wiring
//   - malformed/oversized events and ingester error handling
func TestIngest(t *testing.T) {
	emulator := startEmulator(t)

	ingester, err := tc.Run(t.Context(), "",
		e2e.Ingester(t, "azureeventhubs", "AzureEventHubs",
			e2e.WithConfig(t, "testdata/azureeventhubs.conf", "azure_event_hubs.conf", e2e.DefaultConfig),
		)...,
	)
	t.Cleanup(func() {
		e2e.SaveTestFiles(t, ingester, e2e.Log, []string{
			"/opt/gravwell/log/azure_event_hubs.log",
		})
		e2e.Terminate(t, ingester)
	})
	if err != nil {
		e2e.Fatal(t, err)
	}

	endpoint, err := emulator.PortEndpoint(t.Context(), "5672", "")
	if err != nil {
		e2e.Fatal(t, err)
	}

	want := []string{"hello from the e2e test", "a second event"}
	sendEvents(t, endpoint, want)

	c := e2e.GetClient(t)
	ents := e2e.WaitForEntries(t, c, "tag="+tag, time.Minute, len(want), 30*time.Second)
	if len(ents) != len(want) {
		e2e.Fatalf(t, "got %d entries, want %d", len(ents), len(want))
	}

	for _, msg := range want {
		found := false
		for _, e := range ents {
			if strings.Contains(e.String(), msg) {
				found = true
				break
			}
		}
		if !found {
			e2e.Fatalf(t, "did not find expected event %q in results: %+v", msg, ents)
		}
	}
}
