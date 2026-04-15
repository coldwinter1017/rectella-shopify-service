package payments

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"sort"
	"time"
)

// BuildCSV serialises a slice of ShopifyTransaction into a credit-control
// friendly CSV. Columns match what Liz asked for in the daily email MVP:
// one row per settled payment, gross + fee + net so credit control can
// reconcile the bank deposit against the orders it came from.
//
// The `date` argument is used only for the header line — the caller is
// responsible for filtering txns to the correct window.
func BuildCSV(date time.Time, txns []ShopifyTransaction) ([]byte, error) {
	// Stable ordering: processed_at ascending, then transaction ID.
	sorted := make([]ShopifyTransaction, len(txns))
	copy(sorted, txns)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].ProcessedAt.Equal(sorted[j].ProcessedAt) {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].ProcessedAt.Before(sorted[j].ProcessedAt)
	})

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	header := []string{
		"date",
		"order_number",
		"customer_email",
		"gross",
		"fee",
		"net",
		"currency",
		"processed_at",
		"payment_gateway",
	}
	if err := w.Write(header); err != nil {
		return nil, fmt.Errorf("writing csv header: %w", err)
	}
	dateStr := date.Format("2006-01-02")
	for _, t := range sorted {
		row := []string{
			dateStr,
			t.OrderNumber,
			t.CustomerEmail,
			fmt.Sprintf("%.2f", t.Gross),
			fmt.Sprintf("%.2f", t.Fee),
			fmt.Sprintf("%.2f", t.Net),
			t.Currency,
			t.ProcessedAt.UTC().Format(time.RFC3339),
			t.PaymentGateway,
		}
		if err := w.Write(row); err != nil {
			return nil, fmt.Errorf("writing csv row: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("csv writer: %w", err)
	}
	return buf.Bytes(), nil
}

// SummariseTotals returns gross/fee/net sums for the txns slice. Used
// by the email body so credit control sees the totals in plain text
// without opening the attachment.
func SummariseTotals(txns []ShopifyTransaction) (gross, fee, net float64, count int) {
	for _, t := range txns {
		gross += t.Gross
		fee += t.Fee
		net += t.Net
	}
	return gross, fee, net, len(txns)
}
