package main

import (
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
	// audit endpoint
	// mta endpoint
	// mta storage
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
