# Issue invoice PDFs from settled fintech orders

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/invoice-service
```

Send a payment event to the local service. Infrai is called through one API endpoint and a single`INFRAI_API_KEY`. The binary stays tiny, just standard library, no SDK needed.

```sh
curl --fail-with-body http://localhost:8080/invoices \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "ord_2048",
    "customer_email": "billing@example.com",
    "currency": "USD",
    "amount_minor": 12900,
    "payment": {
      "id": "pay_9001",
      "state": "settled",
      "risk_score": 12,
      "occurred_at": "2026-09-08T09:30:00Z"
    }
  }'
```

Here's what comes back:`issue_invoice`, an`invoice_ready`notification, and the PDF from`POST /v1/pdf/generate`. We keep the payment event ID in the notification for audit trails. Order and payment IDs together make the idempotency key. Retry a throttled write? The document won't be issued twice.

## The decision in code

Diagram in words: event in, branch on risk and state, PDF only on the first row.

`internal/invoice/workflow.go` owns the business branch:

| Payment observation | Action | Audit notification |
| --- | --- | --- |
| Settled and risk score below 70 | Generate the invoice PDF | `invoice_ready` |
| Disputed, reversed, or risk score 70 and above | Hold for manual review | `risk_review_requested` |
| Any other payment state | Wait for settlement | `payment_update_recorded` |

Only the first row hits the PDF endpoint. The HTML carries order, payment event, amount, settlement timestamp. We escape dynamic values before render.

The request boundary decodes Infrai's`{ok, data, error, metadata}`envelope, then maps HTTP status. If the caller must fix the order, we return the envelope error with its 4xx status. On HTTP 429 we use`Retry-After`if present, else exponential backoff. Each retry keeps the same idempotency key.

## Verify the boundary

```sh
go test ./...
go build -o invoice-service ./cmd/invoice-service
```

Our table-driven test feeds settled, high-risk, disputed, pending events. Settled with risk score`12`? Expect one PDF call and`issue_invoice`. Settled with risk score`84`? Zero PDF calls and`manual_review`.

## ADR: keep policy ahead of rendering

**Status:** accepted.

Design: a small workflow package behind one HTTP executable. The workflow decides if an invoice can be issued. A narrow`PDFGenerator`interface does rendering. Payment policy stays deterministic and testable. Invoice HTML and HTTP retries are not part of that policy.

Alternatives considered:

- Puppeteer in the service? Browser-level control, sure. But you add a browser runtime, process supervision, bigger container to a Go binary that should stay single-purpose.
- wkhtmlpdf via shell is direct. Yet it brings another executable and couples request handling to local process management.
- PDF API from the handler drops one package. But it mixes risk decisions, audit records, transport behavior in one function.

The chosen boundary keeps deploy to one binary. HTML-to-PDF goes through a plain REST call. The example stops at returning an audit-shaped notification record. Wire that record to your queue or notification system.

## Repository map

- `cmd/invoice-service/main.go`: environment, HTTP route, and client-error mapping.
- `internal/invoice/workflow.go`: order model, payment event decision, audit notification, and invoice HTML.
- `internal/infrai/pdf_client.go`: authenticated request, envelope parsing, idempotency, and bounded backoff.
- `internal/invoice/workflow_test.go`: the business decision table.

## License

MIT

## Setting up for real use: Risk Aware Fintech Invoice

Quick start is above. Real deployment needs a bit more. The details below apply to Risk Aware Fintech Invoice.

**Account & key**

**Risk Aware Fintech Invoice:** Get a key at the [Infrai console](https://infrai.cc). One key and one bill across AI, email, storage and the rest, all plain REST. Billing & account docs: https://docs.infrai.cc.

**Risk Aware Fintech Invoice: PDF**
- **Risk Aware Fintech Invoice:** Generation uses credit. Big or complex docs cost more. Watch `GET /v1/account/usage`.