// Command logincoded is the single binary behind `mytool login`: it issues a
// phone code, checks the one the user types back, and reports delivery.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"

	"github.com/example/cli-login-sms-codes/internal/infrai"
	"github.com/example/cli-login-sms-codes/internal/logincode"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address")
	flag.Parse()

	api, err := infrai.New()
	if err != nil {
		log.Fatal(err)
	}
	svc := &logincode.Service{API: api}

	mux := http.NewServeMux()

	// POST /login/start {"phone":"+1555...","request_id":"login-abc"}
	mux.HandleFunc("/login/start", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Phone     string `json:"phone"`
			RequestID string `json:"request_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Phone == "" {
			http.Error(w, "phone is required", http.StatusBadRequest)
			return
		}
		ch, err := svc.Issue(r.Context(), in.Phone, in.RequestID)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, ch)
	})

	// POST /login/verify {"phone":"+1555...","code":"314159"}
	mux.HandleFunc("/login/verify", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Phone string `json:"phone"`
			Code  string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Phone == "" || in.Code == "" {
			http.Error(w, "phone and code are required", http.StatusBadRequest)
			return
		}
		outcome, err := svc.Check(r.Context(), in.Phone, in.Code)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, outcome.HTTPStatus(), map[string]string{"outcome": string(outcome)})
	})

	// GET /login/delivery?message_id=sms_42 — what happened to the text.
	mux.HandleFunc("/login/delivery", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("message_id")
		if id == "" {
			http.Error(w, "message_id is required", http.StatusBadRequest)
			return
		}
		status, err := svc.Delivery(r.Context(), id)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})

	log.Printf("logincoded listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// An answer from the API keeps its own shape; only a transport problem becomes
// a 502 from us.
func writeErr(w http.ResponseWriter, err error) {
	if apiErr, ok := err.(*infrai.APIError); ok {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": apiErr.Code})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
}
