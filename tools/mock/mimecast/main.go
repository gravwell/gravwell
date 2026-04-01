package main

import (
	"bytes"
	"compress/gzip"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gravwell/gravwell/v3/ingesters/hosted/plugins/mimecast"
)

var (
	client_id     = flag.String("id", "id", "client id used for auth")
	client_secret = flag.String("secret", "secret", "client secret used for auth")
	port          = flag.Int("port", 8080, "server port")
)

const (
	SIEMBatchTimeFormat = "2006-01-02"
	AuditTimeFormat     = "2006-01-02T15:04:05-0700"
	SIEMTimeFormat      = "2006-01-02T15:04:05.000Z"
)

// gen stores the start and end times for a storage ID and how many pages it should have.
type gen struct {
	start    time.Time
	end      time.Time
	pages    int
	clientID string
}

// EndpointConfig defines behavior for a specific endpoint
type EndpointConfig struct {
	NumPages      int `json:"num_pages"`
	EventsPerPage int `json:"events_per_page"`
}

// ClientConfig holds configuration for all endpoints for a specific client
type ClientConfig struct {
	SiemBatch EndpointConfig `json:"siem_batch"`
	Siem      EndpointConfig `json:"siem"`
	Audit     EndpointConfig `json:"audit"`
}

// ConfigRequest is the structure for setting client configuration
type ConfigRequest struct {
	ClientID string       `json:"client_id"`
	Config   ClientConfig `json:"config"`
}

// clientSession tracks the bearer token for a client
type clientSession struct {
	token    string
	clientID string
}

// paginationState tracks pagination progress for a client+cursor
type paginationState struct {
	start       time.Time
	end         time.Time
	currentPage int
	totalPages  int
}

// storageData maps storage IDs to their time ranges
var (
	storageData = make(map[string]gen)
	storageMtx  sync.RWMutex
)

// clientConfigs maps client IDs to their configurations
var (
	clientConfigs = make(map[string]ClientConfig)
	configMtx     sync.RWMutex
)

// tokenSessions maps bearer tokens to client sessions
var (
	tokenSessions = make(map[string]clientSession)
	sessionMtx    sync.RWMutex
)

// paginationStates tracks pagination state per client+cursor
var (
	paginationStates = make(map[string]paginationState)
	paginationMtx    sync.RWMutex
)

// generateID creates a unique ID for storage
func generateID() string {
	b := make([]byte, 16)
	crand.Read(b)
	return hex.EncodeToString(b)
}

func main() {
	flag.Parse()
	fmt.Printf("starting server on port %d\n", *port)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth/token", auth)
	mux.HandleFunc("POST /config", configHandler)
	mux.HandleFunc("GET /siem/v1/batch/events/cg", siemBatch)
	mux.HandleFunc("GET /siem/v1/events/cg", siem)
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
	clientIDParam := r.Form.Get("client_id")

	if secret := r.Form.Get("client_secret"); secret != *client_secret {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Generate a bearer token for this client
	bearerToken := generateID()

	// Store the session
	sessionMtx.Lock()
	tokenSessions[bearerToken] = clientSession{
		token:    bearerToken,
		clientID: clientIDParam,
	}
	sessionMtx.Unlock()

	token := mimecast.AuthToken{
		AccessToken: bearerToken,
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

// getClientID extracts the client ID from the Authorization header
func getClientID(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}

	// Expected format: "Bearer <token>"
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || parts[0] != "Bearer" {
		return ""
	}

	token := parts[1]
	sessionMtx.RLock()
	session, ok := tokenSessions[token]
	sessionMtx.RUnlock()

	if !ok {
		return ""
	}

	return session.clientID
}

// configHandler handles POST /config to set client-specific mock behavior
func configHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to read request body"})
		return
	}

	var req ConfigRequest
	if err := json.Unmarshal(body, &req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if req.ClientID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "client_id is required"})
		return
	}

	// Store the configuration
	configMtx.Lock()
	clientConfigs[req.ClientID] = req.Config
	configMtx.Unlock()

	fmt.Printf("Config set for client %s: SiemBatch(pages=%d, events=%d), Siem(pages=%d, events=%d), Audit(pages=%d, events=%d)\n",
		req.ClientID,
		req.Config.SiemBatch.NumPages, req.Config.SiemBatch.EventsPerPage,
		req.Config.Siem.NumPages, req.Config.Siem.EventsPerPage,
		req.Config.Audit.NumPages, req.Config.Audit.EventsPerPage)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "success", "client_id": req.ClientID})
}

// siemBatch responds with a mimecast.SIEMBatchEventResponse.
// For the mock it points the URL to the storage endpoint.
// It validates the query params for dates.
func siemBatch(w http.ResponseWriter, r *http.Request) {
	clientID, ok := authed(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Validate query params exist
	query := r.URL.Query()
	cursor := query.Get("nextPage")
	startStr := query.Get("dateRangeStartsAt")
	endStr := query.Get("dateRangeEndsAt")
	if cursor == "" && (startStr == "" || endStr == "") {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if cursor == "" {
		var start, end time.Time
		var err error
		// Parse the time range
		if start, err = time.Parse(SIEMBatchTimeFormat, startStr); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if end, err = time.Parse(SIEMBatchTimeFormat, endStr); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		cursor = generateID()

		// Determine number of pages based on client config
		numPages := 0
		if clientID != "" {
			configMtx.RLock()
			if config, ok := clientConfigs[clientID]; ok {
				numPages = config.SiemBatch.NumPages
			}
			configMtx.RUnlock()
		}

		// If no config, use random behavior
		if numPages == 0 {
			numPages = rand.Intn(3) + 1 // 1-3 pages
		}

		storageMtx.Lock()
		storageData[cursor] = gen{start: start, end: end, pages: numPages, clientID: clientID}
		storageMtx.Unlock()

		// Initialize pagination state
		paginationMtx.Lock()
		paginationStates[cursor] = paginationState{start: start, end: end, currentPage: 1, totalPages: numPages}
		paginationMtx.Unlock()

		fmt.Printf("SIEM Batch: Generated ID %s for range %s to %s (client=%s, pages=%d)\n",
			cursor, start.Format(time.RFC3339), end.Format(time.RFC3339), clientID, numPages)
	}

	// Check pagination state
	paginationMtx.RLock()
	state, ok := paginationStates[cursor]
	paginationMtx.RUnlock()

	hasNextPage := false
	nextPage := ""

	if ok && state.currentPage < state.totalPages {
		hasNextPage = true
		nextPage = cursor

		// Update pagination state
		paginationMtx.Lock()
		state.currentPage++
		paginationStates[cursor] = state
		paginationMtx.Unlock()
	}

	response := mimecast.SIEMBatchEventResponse{
		Value: []mimecast.SIEMBatchEvent{
			{
				URL:  fmt.Sprintf("http://%s/storage/%s/json.gz", r.Host, cursor),
				Size: 1024,
			},
		},
		NextPage:   nextPage,
		IsCaughtUp: !hasNextPage,
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

// siem responds with a mimecast.SIEMEventResponse.
// It validates the query params for dates.
func siem(w http.ResponseWriter, r *http.Request) {
	clientID, ok := authed(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	start, end, cursor, valid := validateSiem(w, r)
	if !valid {
		return
	}

	// Check for config
	numPages := 0
	eventsPerPage := 20 // default
	if clientID != "" {
		configMtx.RLock()
		if config, ok := clientConfigs[clientID]; ok {
			numPages = config.Siem.NumPages
			if config.Siem.EventsPerPage > 0 {
				eventsPerPage = config.Siem.EventsPerPage
			}
		}
		configMtx.RUnlock()
	}

	if cursor == "" {
		cursor = generateID()

		// If no config, use random behavior
		if numPages == 0 {
			numPages = rand.Intn(3) + 1 // 1-3 pages
		}

		// Initialize pagination state
		paginationMtx.Lock()
		paginationStates[cursor] = paginationState{start: start, end: end, currentPage: 1, totalPages: numPages}
		paginationMtx.Unlock()

		fmt.Printf("SIEM: Generated cursor %s (client=%s, pages=%d, eventsPerPage=%d)\n",
			cursor, clientID, numPages, eventsPerPage)
	}

	// Check pagination state
	paginationMtx.RLock()
	state, ok := paginationStates[cursor]
	paginationMtx.RUnlock()

	hasNextPage := false
	nextPage := ""

	if ok && state.currentPage < state.totalPages {
		hasNextPage = true
		nextPage = cursor

		// Update pagination state
		paginationMtx.Lock()
		state.currentPage++
		paginationStates[cursor] = state
		paginationMtx.Unlock()
	}

	events, err := genSiemEvents(eventsPerPage, state.start, state.end)
	if err != nil {
		siemError(w, http.StatusInternalServerError, mimecast.Error{
			Code:    "Server Error",
			Message: "Error generation SIEM events: " + err.Error(),
		})
		return
	}

	response := mimecast.SIEMEventResponse{
		Value:      events,
		NextPage:   nextPage,
		IsCaughtUp: !hasNextPage,
	}

	body, err := json.Marshal(response)
	if err != nil {
		siemError(w, http.StatusInternalServerError, mimecast.Error{
			Code:    "Server Error",
			Message: "Error marshaling response: " + err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

func siemError(w http.ResponseWriter, status int, err mimecast.Error) {
	response := mimecast.SIEMErrorResponse{
		Error: err,
	}
	body, _ := json.Marshal(response)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// storageData maps storage IDs to their time ranges
var (
	auditData = make(map[string]gen)
	auditMtx  sync.RWMutex
)

// audit responds with a mimecast.Response where data is an encoded mimecast.AuditData
// this data should contain an additional field called 'message' and have content in it.
func audit(w http.ResponseWriter, r *http.Request) {
	clientID, ok := authed(w, r)
	if !ok {
		return
	}
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
		fmt.Printf("ERROR: failed to unmarshal request: %s\n", err)
		return
	}

	cursor := req.Meta.Pagination.PageToken
	var start, end time.Time

	// Check for config
	numPages := 0
	eventsPerPage := 20 // default
	if clientID != "" {
		configMtx.RLock()
		if config, ok := clientConfigs[clientID]; ok {
			numPages = config.Audit.NumPages
			if config.Audit.EventsPerPage > 0 {
				eventsPerPage = config.Audit.EventsPerPage
			}
		}
		configMtx.RUnlock()
	}

	if cursor == "" {
		// Extract time range from request
		if len(req.Data) > 0 {
			start, err = time.Parse(mimecast.AuditTimeFormat, req.Data[0].StartDateTime)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Printf("ERROR: failed to parse start date: %s\n", err)
				return
			}
			end, err = time.Parse(mimecast.AuditTimeFormat, req.Data[0].EndDateTime)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Printf("ERROR: failed to parse end date: %s\n", err)
				return
			}
		} else {
			// Default time range if not provided
			end = time.Now()
			start = end.Add(-24 * time.Hour)
		}
		cursor = generateID()
		auditMtx.Lock()
		auditData[cursor] = gen{start: start, end: end}
		auditMtx.Unlock()

		// If no config, use random behavior
		if numPages == 0 {
			numPages = rand.Intn(3) + 1 // 1-3 pages
		}

		// Initialize pagination state
		paginationMtx.Lock()
		paginationStates[cursor] = paginationState{currentPage: 1, totalPages: numPages}
		paginationMtx.Unlock()

		fmt.Printf("Audit: Generated cursor %s (client=%s, pages=%d, eventsPerPage=%d)\n",
			cursor, clientID, numPages, eventsPerPage)
	} else {
		auditMtx.RLock()
		dates, ok := auditData[cursor]
		auditMtx.RUnlock()
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		start = dates.start
		end = dates.end
	}

	// Generate audit events
	events := make([]json.RawMessage, 0, eventsPerPage)
	duration := end.Sub(start)

	categories := []string{"account_protection", "email_security", "policy_compliance", "user_login", "admin_action"}
	messages := []string{
		"User authentication successful",
		"Security policy updated",
		"Email threat detected and blocked",
		"Account settings modified",
		"Administrative action performed",
	}
	users := []string{"admin@example.com", "user1@example.com", "security@example.com", "ops@example.com", "test@example.com"}

	for i := 0; i < eventsPerPage; i++ {
		// Add jitter within the time range
		jitter := time.Duration(float64(duration) * (float64(i) / float64(eventsPerPage)))
		eventTime := start.Add(jitter)

		event := map[string]interface{}{
			"eventTime": eventTime.Format(mimecast.AuditTimeFormat),
			"message":   messages[i%len(messages)],
			"user":      users[i%len(users)],
			"category":  categories[i%len(categories)],
			"eventId":   fmt.Sprintf("audit-event-%d", i+1),
		}

		dataBytes, err := json.Marshal(event)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		events = append(events, dataBytes)
	}

	// Check pagination state
	paginationMtx.RLock()
	state, ok := paginationStates[cursor]
	paginationMtx.RUnlock()

	hasNextPage := false
	if ok && state.currentPage < state.totalPages {
		hasNextPage = true

		// Update pagination state
		paginationMtx.Lock()
		state.currentPage++
		paginationStates[cursor] = state
		paginationMtx.Unlock()
	}

	response := mimecast.Response{
		Meta: mimecast.ResponseMeta{},
		Data: events,
	}
	if hasNextPage {
		response.Meta.Pagination.Next = cursor
	}

	responseBody, err := json.Marshal(response)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

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

	// Get events per page from batch config
	eventsPerPage := 1000 // default
	if tr.clientID != "" {
		configMtx.RLock()
		if config, ok := clientConfigs[tr.clientID]; ok {
			if config.SiemBatch.EventsPerPage > 0 {
				eventsPerPage = config.SiemBatch.EventsPerPage
			}
		}
		configMtx.RUnlock()
	}

	events, err := genSiemEvents(eventsPerPage, tr.start, tr.end)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Build multiline JSON
	var buf bytes.Buffer
	for i, event := range events {
		buf.Write(event)
		if i < len(events)-1 {
			buf.WriteString("\n")
		}
	}

	// Gzip the data
	var gzBuf bytes.Buffer
	gzWriter := gzip.NewWriter(&gzBuf)
	if _, err := gzWriter.Write(buf.Bytes()); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = gzWriter.Close()
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

func validateSiem(w http.ResponseWriter, r *http.Request) (start time.Time, end time.Time, cursor string, valid bool) {
	// Validate query params exist
	query := r.URL.Query()
	cursor = query.Get("nextPage")
	// the cursor stores the start/end time so no need to pull from request
	if cursor != "none" {
		valid = true
		return
	}

	startStr := query.Get("dateRangeStartsAt")
	endStr := query.Get("dateRangeEndsAt")

	if startStr == "" || endStr == "" {
		siemError(w, http.StatusBadRequest, mimecast.Error{
			Code:    "Request Validation Failed",
			Message: "Need time range",
		})
		return
	}

	var err error
	// Parse the time range
	if start, err = time.Parse(SIEMTimeFormat, startStr); err != nil {
		siemError(w, http.StatusBadRequest, mimecast.Error{
			Code:    "Request Validation Failed",
			Message: "Invalid date: `dateRangeStartsAt` does not match format: " + SIEMTimeFormat,
		})
		return
	}
	if end, err = time.Parse(SIEMTimeFormat, endStr); err != nil {
		siemError(w, http.StatusBadRequest, mimecast.Error{
			Code:    "Request Validation Failed",
			Message: "Invalid date: `dateRangeEndsAt` does not match format: " + SIEMTimeFormat,
		})
		return
	}
	if start.Before(time.Now().Add(-24 * time.Hour)) {
		siemError(w, http.StatusBadRequest, mimecast.Error{
			Code:    "Request Validation Failed",
			Message: "Invalid date range: 'from' must be within the 24-hour retention period",
		})
		return
	}

	valid = true
	return
}

func genSiemEvents(amount int, start, end time.Time) ([]json.RawMessage, error) {
	// Generate multiple MTA events with jittered timestamps within the time range
	events := make([]map[string]interface{}, 0, amount)
	duration := end.Sub(start)

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

	for i := 0; i < amount; i++ {
		// Add jitter within the time range, distributing events from start to end
		// Use (i+1)/(amount+1) to avoid bunching at the boundaries
		jitterFactor := float64(i+1) / float64(amount+1)
		jitter := time.Duration(float64(duration) * jitterFactor)
		eventTime := start.Add(jitter)

		fmt.Printf("Generated SIEM event %d: start=%s end=%s duration=%s jitterFactor=%f eventTime=%s\n",
			i, start.Format(time.RFC3339), end.Format(time.RFC3339), duration, jitterFactor, eventTime.Format(time.RFC3339))

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

	var list []json.RawMessage
	for _, event := range events {
		eventBytes, err := json.Marshal(event)
		if err != nil {
			return nil, err
		}
		list = append(list, eventBytes)
	}

	return list, nil
}

func authed(w http.ResponseWriter, r *http.Request) (string, bool) {
	bearer := r.Header.Get("Authorization")
	if bearer == "" {
		authError(w, "Missing Authorization Header")
		return "", false
	}
	parts := strings.Split(bearer, " ")
	if len(parts) != 2 || parts[0] != "Bearer" {
		authError(w, "Invalid Authorization Header")
		return "", false
	}
	token := parts[1]

	sessionMtx.RLock()
	defer sessionMtx.RUnlock()
	id, ok := tokenSessions[token]
	if !ok {
		authError(w, "Invalid Token")
		return "", false
	}
	return id.clientID, ok
}

func authError(w http.ResponseWriter, message string) {
	response := mimecast.AuthFailureResponse{
		Fail: []mimecast.Error{
			{
				Code:    "Auth Failure",
				Message: message,
			},
		},
	}
	body, _ := json.Marshal(response)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write(body)
}
