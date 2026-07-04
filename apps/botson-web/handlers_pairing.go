package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"botson/internal/auth"
)

// handleGetPairings returns the list of pending pairing requests.
func handleGetPairings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pairings, err := auth.GetPendingPairings()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pairings)
}

// ApprovePairingReq is the request body for the /api/pairings/approve endpoint.
type ApprovePairingReq struct {
	Gateway string `json:"gateway"`
	Code    string `json:"code"`
}

// handleApprovePairings approves a pending pairing identified by gateway and code.
func handleApprovePairings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ApprovePairingReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	username, err := auth.ApprovePairing(req.Gateway, req.Code)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "message": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   "success",
		"username": username,
		"message":  fmt.Sprintf("Successfully approved pairing for %s!", username),
	})
}
