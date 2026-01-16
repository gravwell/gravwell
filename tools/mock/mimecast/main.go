package main

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingesters/hosted/plugins/mimecast"
)

var (
	client_id     = flag.String("id", "id", "client id used for auth")
	client_secret = flag.String("secret", "secret", "client secret used for auth")
	port          = flag.Int("port", 8080, "server port")
)

// timeRange stores the start and end times for a storage ID
type timeRange struct {
	start time.Time
	end   time.Time
}

// storageData maps storage IDs to their time ranges
var (
	storageData = make(map[string]timeRange)
	storageMtx  sync.RWMutex
)

// generateID creates a unique ID for storage
func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func main() {
	flag.Parse()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth/token", auth)
	mux.HandleFunc("GET /siem/v1/batch/events/cg", siem)
	mux.HandleFunc("POST /api/audit/get-audit-events", audit)
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
	startStr := query.Get("dateRangeStartsAt")
	endStr := query.Get("dateRangeEndsAt")
	if startStr == "" || endStr == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Parse the time range
	startTime, err := time.Parse(mimecast.MTATimeFormat, startStr)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	endTime, err := time.Parse(mimecast.MTATimeFormat, endStr)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Generate unique ID for this request
	id := generateID()

	// Store the time range for this ID
	storageMtx.Lock()
	storageData[id] = timeRange{start: startTime, end: endTime}
	storageMtx.Unlock()

	fmt.Printf("SIEM: Generated ID %s for range %s to %s\n", id, startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))

	// Build storage URL with the unique ID
	storageURL := fmt.Sprintf("http://localhost:%d/storage/%s/json.gz", *port, id)

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

	// Parse the request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var req mimecast.Request
	if err := json.Unmarshal(body, &req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Extract time range from request
	var startTime, endTime time.Time
	if len(req.Data) > 0 {
		startTime, err = time.Parse(mimecast.AuditTimeFormat, req.Data[0].StartDateTime)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		endTime, err = time.Parse(mimecast.AuditTimeFormat, req.Data[0].EndDateTime)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	} else {
		// Default time range if not provided
		endTime = time.Now()
		startTime = endTime.Add(-24 * time.Hour)
	}

	// Generate multiple audit events with jittered timestamps
	numEvents := 20
	events := make([]json.RawMessage, 0, numEvents)
	duration := endTime.Sub(startTime)

	categories := []string{"account_protection", "email_security", "policy_compliance", "user_login", "admin_action"}
	messages := []string{
		"User authentication successful",
		"Security policy updated",
		"Email threat detected and blocked",
		"Account settings modified",
		"Administrative action performed",
	}
	users := []string{"admin@example.com", "user1@example.com", "security@example.com", "ops@example.com", "test@example.com"}

	for i := 0; i < numEvents; i++ {
		// Add jitter within the time range
		jitter := time.Duration(float64(duration) * (float64(i) / float64(numEvents)))
		eventTime := startTime.Add(jitter)

		auditData := map[string]interface{}{
			"eventTime": eventTime.Format(mimecast.AuditTimeFormat),
			"message":   messages[i%len(messages)],
			"user":      users[i%len(users)],
			"category":  categories[i%len(categories)],
			"eventId":   fmt.Sprintf("audit-event-%d", i+1),
		}

		dataBytes, err := json.Marshal(auditData)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		events = append(events, dataBytes)
	}

	response := mimecast.Response{
		Meta: mimecast.ResponseMeta{},
		Data: events,
	}

	responseBody, err := json.Marshal(response)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(responseBody)
}

// storage should return a gziped file (NOT a gzipped http response) of multiline json blobs.
// each of the json blobs should be a mimecast.MtaEventData with an extra field much like the audit function.
func storage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Extract ID from path
	id := r.PathValue("id")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Look up the time range for this ID
	storageMtx.RLock()
	tr, ok := storageData[id]
	storageMtx.RUnlock()

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Generate multiple MTA events with jittered timestamps within the time range
	numEvents := 5
	events := make([]map[string]interface{}, 0, numEvents)
	duration := tr.end.Sub(tr.start)

	messages := []string{
		"Email delivered successfully",
		"Email received",
		"Email processed",
		"Spam detected and blocked",
		"Attachment scanned",
	}
	froms := []string{"sender@example.com", "another@example.com", "system@example.com", "user@example.com", "admin@example.com"}
	tos := []string{"recipient@example.com", "user@example.com", "admin@example.com", "team@example.com", "support@example.com"}
	subjects := []string{"Important Update", "Monthly Report", "Action Required", "System Notification", "Security Alert"}

	for i := 0; i < numEvents; i++ {
		// Add jitter within the time range, distributing events from start to end
		// Use (i+1)/(numEvents+1) to avoid bunching at the boundaries
		jitterFactor := float64(i+1) / float64(numEvents+1)
		jitter := time.Duration(float64(duration) * jitterFactor)
		eventTime := tr.start.Add(jitter)

		fmt.Printf("Storage[%s] Event %d: start=%s end=%s duration=%s jitterFactor=%f eventTime=%s\n",
			id, i, tr.start.Format(time.RFC3339), tr.end.Format(time.RFC3339), duration, jitterFactor, eventTime.Format(time.RFC3339))

		event := map[string]interface{}{
			"timestamp": eventTime.UnixMilli(),
			"message":   messages[i%len(messages)],
			"from":      froms[i%len(froms)],
			"to":        tos[i%len(tos)],
			"subject":   subjects[i%len(subjects)],
			"eventId":   fmt.Sprintf("mta-event-%d", i+1),
		}
		events = append(events, event)
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
