package payments

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type fakeSource struct {
	mu        sync.Mutex
	calls     int
	sinceSeen time.Time
	untilSeen time.Time
	toReturn  []ShopifyTransaction
	err       error
}

func (f *fakeSource) FetchOrdersInWindow(ctx context.Context, since, until time.Time) ([]ShopifyTransaction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.sinceSeen = since
	f.untilSeen = until
	if f.err != nil {
		return nil, f.err
	}
	return f.toReturn, nil
}

type fakeSender struct {
	mu          sync.Mutex
	calls       int
	lastSubject string
	lastBody    string
	lastAtt     *Attachment
	err         error
}

func (f *fakeSender) Send(ctx context.Context, to []string, subject, body string, att *Attachment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastSubject = subject
	f.lastBody = body
	f.lastAtt = att
	return f.err
}

func newDailyTestReporter(t *testing.T, src TransactionSource, send EmailSender) *DailyReporter {
	t.Helper()
	r, err := NewDailyReporter(DailyReporterConfig{
		Source:     src,
		Mailer:     send,
		Recipients: []string{"creditcontrol@example.com"},
		StoreName:  "Barbequick",
		Hour:       7,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewDailyReporter: %v", err)
	}
	return r
}

func TestDailyReporter_SendForDate_HappyPath(t *testing.T) {
	src := &fakeSource{
		toReturn: []ShopifyTransaction{
			{
				ID: 1, OrderNumber: "#BBQ1001",
				Gross: 125.00, Fee: 3.75, Net: 121.25,
				Currency: "GBP", PaymentGateway: "shopify_payments",
				ProcessedAt: time.Date(2026, 4, 14, 10, 30, 0, 0, time.UTC),
			},
		},
	}
	send := &fakeSender{}
	r := newDailyTestReporter(t, src, send)

	date := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	if err := r.SendForDate(context.Background(), date); err != nil {
		t.Fatalf("SendForDate: %v", err)
	}
	if src.calls != 1 {
		t.Errorf("source calls = %d, want 1", src.calls)
	}
	wantSince := time.Date(2026, 4, 14, 0, 0, 0, 0, time.UTC)
	wantUntil := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
	if !src.sinceSeen.Equal(wantSince) {
		t.Errorf("since = %v, want %v", src.sinceSeen, wantSince)
	}
	if !src.untilSeen.Equal(wantUntil) {
		t.Errorf("until = %v, want %v", src.untilSeen, wantUntil)
	}
	if send.calls != 1 {
		t.Errorf("sender calls = %d, want 1", send.calls)
	}
	if send.lastAtt == nil {
		t.Fatal("attachment not sent")
	}
	if send.lastAtt.Filename != "cash-receipts-2026-04-14.csv" {
		t.Errorf("attachment filename = %q", send.lastAtt.Filename)
	}
}

func TestDailyReporter_SendForDate_ZeroTransactions(t *testing.T) {
	src := &fakeSource{toReturn: nil}
	send := &fakeSender{}
	r := newDailyTestReporter(t, src, send)
	if err := r.SendForDate(context.Background(), time.Now()); err != nil {
		t.Fatalf("SendForDate: %v", err)
	}
	if send.calls != 1 {
		t.Error("zero-transaction day should still send the email (proof of life)")
	}
}

func TestDailyReporter_SendForDate_FetchError(t *testing.T) {
	src := &fakeSource{err: errors.New("shopify 500")}
	send := &fakeSender{}
	r := newDailyTestReporter(t, src, send)
	err := r.SendForDate(context.Background(), time.Now())
	if err == nil {
		t.Fatal("want error")
	}
	if send.calls != 0 {
		t.Error("should not send if fetch fails")
	}
}

func TestNewDailyReporter_Validation(t *testing.T) {
	cases := []DailyReporterConfig{
		{Mailer: &fakeSender{}, Recipients: []string{"a@b"}, Hour: 7},
		{Source: &fakeSource{}, Recipients: []string{"a@b"}, Hour: 7},
		{Source: &fakeSource{}, Mailer: &fakeSender{}, Hour: 7},
		{Source: &fakeSource{}, Mailer: &fakeSender{}, Recipients: []string{"a@b"}, Hour: 25},
	}
	for i, cfg := range cases {
		if _, err := NewDailyReporter(cfg); err == nil {
			t.Errorf("case %d: want error", i)
		}
	}
}
