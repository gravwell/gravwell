package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"

	"github.com/gravwell/gravwell/v3/ingesters/hosted/plugins/mimecast"
)

var (
	client_id     = flag.String("id", "id", "client id used for auth")
	client_secret = flag.String("secret", "secret", "client secret used for auth")
	port          = flag.Int("port", 8080, "server port")
)

func main() {
	flag.Parse()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth/token", auth)
	mux.HandleFunc("GET /siem/v1/batch/events/cg", siem)
	mux.HandleFunc("GET /api/audit/get-audit-events", audit)
	mux.HandleFunc("GET /storage/{id}/json.gz", storage)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", *port), mux); err != nil {
		panic(err)
	}
}

func auth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if id := r.Form.Get("client_id"); id != *client_id {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if secret := r.Form.Get("client_secret"); secret != *client_secret {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	token := mimecast.AuthToken{
		AccessToken: "token",
		ExpireIn:    300,
	}
	body, err := json.Marshal(token)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

// siem responds with a mimecast.SIEMBatchEventResponse.
// For the mock it points the URL to the storage endpoint.
// It validates the query params for dates.
func siem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Validate query params exist
	query := r.URL.Query()
	if query.Get("dateRangeStartsAt") == "" || query.Get("dateRangeEndsAt") == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Build storage URL
	storageURL := fmt.Sprintf("http://localhost:%d/storage/test-id/json.gz", *port)

	response := mimecast.SIEMBatchEventResponse{
		Value: []mimecast.SIEMEvent{
			{
				URL:  storageURL,
				Size: 1024,
			},
		},
		NextPage:   "",
		IsCaughtUp: true,
	}

	body, err := json.Marshal(response)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

// audit responds with a mimecast.Response where data is an encoded mimecast.AuditData
// this data should contain an additional field called 'message' and have content in it.
func audit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Create audit data with extra message field
	auditData := map[string]interface{}{
		"eventTime": "2026-01-15T10:00:00-0700",
		"message":   "Test audit event message",
		"user":      "test@example.com",
		"category":  "account_protection",
	}

	dataBytes, err := json.Marshal(auditData)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	response := mimecast.Response{
		Meta: mimecast.ResponseMeta{},
		Data: []json.RawMessage{dataBytes},
	}

	body, err := json.Marshal(response)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

// storage should return a gziped file (NOT a gzipped http response) of multiline json blobs.
// each of the json blobs should be a mimecast.MtaEventData with an extra field much like the audit function.
func storage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Create sample MTA event data with extra fields
	events := []map[string]interface{}{
		{
			"timestamp": 1736950800000, // Jan 15, 2026 in milliseconds
			"message":   "Email delivered successfully",
			"from":      "sender@example.com",
			"to":        "recipient@example.com",
			"subject":   "Test email 1",
		},
		{
			"timestamp": 1736950860000,
			"message":   "Email received",
			"from":      "another@example.com",
			"to":        "user@example.com",
			"subject":   "Test email 2",
		},
		{
			"timestamp": 1736950920000,
			"message":   "Email processed",
			"from":      "system@example.com",
			"to":        "admin@example.com",
			"subject":   "Test email 3",
		},
	}

	// Build multiline JSON
	var buf bytes.Buffer
	for i, event := range events {
		eventBytes, err := json.Marshal(event)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		buf.Write(eventBytes)
		if i < len(events)-1 {
			buf.WriteString("\n")
		}
	}

	// Gzip the data
	var gzBuf bytes.Buffer
	gzWriter := gzip.NewWriter(&gzBuf)
	defer gzWriter.Close()
	if _, err := gzWriter.Write(buf.Bytes()); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if err := gzWriter.Close(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/gzip")
	w.WriteHeader(http.StatusOK)
	w.Write(gzBuf.Bytes())
}
