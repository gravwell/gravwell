package HttpIngester

import (
	"bytes"
	"gravwell/e2e"
	"net/http"
	"testing"
	"time"

	"github.com/gravwell/gravwell/v3/ingesters/utils"
	clogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	cv1 "go.opentelemetry.io/proto/otlp/common/v1"
	lpb "go.opentelemetry.io/proto/otlp/logs/v1"
	rv1 "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestOtelLogs(t *testing.T) {
	_, endpoint := setup(t, "otel_logs")

	t.Run("json encoded ingest", func(t *testing.T) {
		data := WrapOtelLogs(nil, nil, []*lpb.LogRecord{
			{
				Body: &cv1.AnyValue{Value: &cv1.AnyValue_StringValue{StringValue: "json ingest"}},
			},
		})
		SendOtelLogsJson(t, endpoint, data)

		c := e2e.GetClient(t)
		assert(t, e2e.WaitForEntries(t, c, `tag=otel-logs words json`, time.Minute, 1, 30*time.Second), 1, "json ingest")
	})

	t.Run("protobuf encoded ingest", func(t *testing.T) {
		data := WrapOtelLogs(nil, nil, []*lpb.LogRecord{
			{
				Body: &cv1.AnyValue{Value: &cv1.AnyValue_StringValue{StringValue: "proto ingest"}},
			},
		})
		SendOtelLogsProtobuf(t, endpoint, data)

		c := e2e.GetClient(t)
		assert(t, e2e.WaitForEntries(t, c, `tag=otel-logs words proto`, time.Minute, 1, 30*time.Second), 1, "proto ingest")
	})
}

func WrapOtelLogs(r *rv1.Resource, s *cv1.InstrumentationScope, l []*lpb.LogRecord) *clogsv1.ExportLogsServiceRequest {
	return &clogsv1.ExportLogsServiceRequest{
		ResourceLogs: []*lpb.ResourceLogs{
			{
				Resource: r,
				ScopeLogs: []*lpb.ScopeLogs{
					{
						Scope:      s,
						LogRecords: l,
					},
				},
			},
		},
	}
}

func SendOtelLogsJson(t *testing.T, endpoint string, el *clogsv1.ExportLogsServiceRequest) {
	body, err := protojson.Marshal(el)
	if err != nil {
		t.Fatal(err)
	}
	sendOtelLogs(t, endpoint, body, "application/json")
}

func SendOtelLogsProtobuf(t *testing.T, endpoint string, el *clogsv1.ExportLogsServiceRequest) {
	body, err := proto.Marshal(el)
	if err != nil {
		t.Fatal(err)
	}
	sendOtelLogs(t, endpoint, body, "application/protobuf")
}

func sendOtelLogs(t *testing.T, endpoint string, body []byte, ct string) {
	t.Helper()
	req, err := http.NewRequest("POST", endpoint+"/v1/logs", bytes.NewBuffer(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", ct)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer utils.DrainResponse(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want %d", resp.StatusCode, http.StatusOK)
	}

}
