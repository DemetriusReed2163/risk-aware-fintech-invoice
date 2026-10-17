package invoice

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type recordingPDF struct {
	calls int
	key   string
}

func (p *recordingPDF) Generate(_ context.Context, key, _ string) (json.RawMessage, error) {
	p.calls++
	p.key = key
	return json.RawMessage(`{"job_id":"job_demo_42"}`), nil
}

func TestWorkflowRiskDecision(t *testing.T) {
	fixed := time.Date(2026, 9, 8, 9, 30, 0, 0, time.UTC)
	tests := []struct {
		name             string
		state            PaymentState
		risk             int
		wantDecision     Decision
		wantNotification string
		wantPDFCalls     int
	}{
		{name: "settled low risk issues invoice", state: PaymentSettled, risk: 12, wantDecision: DecisionIssueInvoice, wantNotification: "invoice_ready", wantPDFCalls: 1},
		{name: "settled high risk requests review", state: PaymentSettled, risk: 84, wantDecision: DecisionManualReview, wantNotification: "risk_review_requested", wantPDFCalls: 0},
		{name: "dispute requests review", state: PaymentDisputed, risk: 8, wantDecision: DecisionManualReview, wantNotification: "risk_review_requested", wantPDFCalls: 0},
		{name: "pending event waits", state: "authorized", risk: 20, wantDecision: DecisionWait, wantNotification: "payment_update_recorded", wantPDFCalls: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pdf := &recordingPDF{}
			workflow := Workflow{PDF: pdf, Now: func() time.Time { return fixed }}
			order := Order{
				ID: "ord_2048", CustomerEmail: "ops@example.com", Currency: "USD", AmountMinor: 12900,
				Payment: PaymentEvent{ID: "pay_9001", State: tt.state, RiskScore: tt.risk, OccurredAt: fixed},
			}

			got, err := workflow.Process(context.Background(), order)
			if err != nil {
				t.Fatalf("Process() error = %v", err)
			}
			if got.Decision != tt.wantDecision || got.Notification.Kind != tt.wantNotification {
				t.Fatalf("decision = %q, notification = %q", got.Decision, got.Notification.Kind)
			}
			if pdf.calls != tt.wantPDFCalls {
				t.Fatalf("PDF calls = %d, want %d", pdf.calls, tt.wantPDFCalls)
			}
			if pdf.calls == 1 && pdf.key != "invoice:ord_2048:pay_9001" {
				t.Fatalf("idempotency key = %q", pdf.key)
			}
		})
	}
}
