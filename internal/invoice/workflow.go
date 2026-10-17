package invoice

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"time"
)

type PaymentState string

const (
	PaymentSettled  PaymentState = "settled"
	PaymentDisputed PaymentState = "disputed"
	PaymentReversed PaymentState = "reversed"
)

type Order struct {
	ID            string       `json:"id"`
	CustomerEmail string       `json:"customer_email"`
	Currency      string       `json:"currency"`
	AmountMinor   int64        `json:"amount_minor"`
	Payment       PaymentEvent `json:"payment"`
}

type PaymentEvent struct {
	ID         string       `json:"id"`
	State      PaymentState `json:"state"`
	RiskScore  int          `json:"risk_score"`
	OccurredAt time.Time    `json:"occurred_at"`
}

type Decision string

const (
	DecisionIssueInvoice Decision = "issue_invoice"
	DecisionManualReview Decision = "manual_review"
	DecisionWait         Decision = "wait_for_settlement"
)

type Notification struct {
	Kind      string    `json:"kind"`
	Recipient string    `json:"recipient"`
	EventID   string    `json:"event_id"`
	CreatedAt time.Time `json:"created_at"`
}

type Result struct {
	OrderID      string          `json:"order_id"`
	Decision     Decision        `json:"decision"`
	Notification Notification    `json:"notification"`
	PDF          json.RawMessage `json:"pdf,omitempty"`
}

type PDFGenerator interface {
	Generate(ctx context.Context, idempotencyKey, documentHTML string) (json.RawMessage, error)
}

type Clock func() time.Time

type Workflow struct {
	PDF PDFGenerator
	Now Clock
}

func (w Workflow) Process(ctx context.Context, order Order) (Result, error) {
	if err := validate(order); err != nil {
		return Result{}, err
	}

	decision, kind := decide(order.Payment)
	result := Result{
		OrderID:  order.ID,
		Decision: decision,
		Notification: Notification{
			Kind:      kind,
			Recipient: order.CustomerEmail,
			EventID:   order.Payment.ID,
			CreatedAt: w.Now().UTC(),
		},
	}
	if decision != DecisionIssueInvoice {
		return result, nil
	}

	pdf, err := w.PDF.Generate(ctx, "invoice:"+order.ID+":"+order.Payment.ID, renderInvoice(order))
	if err != nil {
		return Result{}, err
	}
	result.PDF = pdf
	return result, nil
}

func decide(event PaymentEvent) (Decision, string) {
	if event.State == PaymentDisputed || event.State == PaymentReversed || event.RiskScore >= 70 {
		return DecisionManualReview, "risk_review_requested"
	}
	if event.State == PaymentSettled {
		return DecisionIssueInvoice, "invoice_ready"
	}
	return DecisionWait, "payment_update_recorded"
}

func validate(order Order) error {
	if order.ID == "" || order.CustomerEmail == "" || order.Currency == "" || order.AmountMinor <= 0 {
		return fmt.Errorf("order id, customer email, currency, and positive amount are required")
	}
	if order.Payment.ID == "" || order.Payment.OccurredAt.IsZero() {
		return fmt.Errorf("payment event id and occurrence time are required")
	}
	if order.Payment.RiskScore < 0 || order.Payment.RiskScore > 100 {
		return fmt.Errorf("risk score must be between 0 and 100")
	}
	return nil
}

func renderInvoice(order Order) string {
	return fmt.Sprintf(`<!doctype html><html><body><h1>Invoice %s</h1><dl><dt>Order</dt><dd>%s</dd><dt>Payment event</dt><dd>%s</dd><dt>Total</dt><dd>%s %.2f</dd><dt>Settled at</dt><dd>%s</dd></dl></body></html>`,
		html.EscapeString(order.ID), html.EscapeString(order.ID), html.EscapeString(order.Payment.ID),
		html.EscapeString(order.Currency), float64(order.AmountMinor)/100,
		html.EscapeString(order.Payment.OccurredAt.UTC().Format(time.RFC3339)))
}
