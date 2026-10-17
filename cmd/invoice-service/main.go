package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	pdfapi "example.com/fintech-invoice-service/internal/infrai"
	"example.com/fintech-invoice-service/internal/invoice"
)

func main() {
	apiKey := os.Getenv("INFRAI_API_KEY")
	if apiKey == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}

	workflow := invoice.Workflow{PDF: pdfapi.NewClient(apiKey), Now: time.Now}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /invoices", invoiceHandler(workflow))

	server := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("invoice service listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}

func invoiceHandler(workflow invoice.Workflow) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var order invoice.Order
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&order); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid order payload"})
			return
		}

		result, err := workflow.Process(r.Context(), order)
		if err != nil {
			var apiErr *pdfapi.APIError
			if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
				writeJSON(w, apiErr.HTTPStatus, map[string]any{"error": json.RawMessage(apiErr.Details)})
				return
			}
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "invoice generation did not complete"})
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}
