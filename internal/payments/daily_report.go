package payments

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// TransactionSource is the narrow interface the daily report needs from
// the Shopify transactions fetcher. Satisfied by *TransactionsFetcher.
type TransactionSource interface {
	FetchOrdersInWindow(ctx context.Context, since, until time.Time) ([]ShopifyTransaction, error)
}

// EmailSender is the narrow interface the daily report needs from the
// mailer. Satisfied by *Mailer.
type EmailSender interface {
	Send(ctx context.Context, to []string, subject, textBody string, att *Attachment) error
}

// DailyReporter sends one CSV per day to credit control. It wakes at
// `hour` UTC each day, pulls yesterday's settled transactions from
// Shopify, builds the CSV, and emails it with a plaintext summary. If
// yesterday had zero transactions the email is still sent so credit
// control knows the job is alive.
type DailyReporter struct {
	source     TransactionSource
	mailer     EmailSender
	recipients []string
	storeName  string
	hour       int
	now        func() time.Time
	logger     *slog.Logger

	mu sync.Mutex
}

// DailyReporterConfig bundles inputs for NewDailyReporter.
type DailyReporterConfig struct {
	Source     TransactionSource
	Mailer     EmailSender
	Recipients []string
	StoreName  string // used in the email subject + filename
	Hour       int    // UTC hour, 0-23
	Logger     *slog.Logger
}

// NewDailyReporter validates inputs and returns a reporter. Returns an
// error if required fields are missing — callers that want graceful
// disablement should check config presence before calling.
func NewDailyReporter(cfg DailyReporterConfig) (*DailyReporter, error) {
	if cfg.Source == nil {
		return nil, fmt.Errorf("daily reporter: source required")
	}
	if cfg.Mailer == nil {
		return nil, fmt.Errorf("daily reporter: mailer required")
	}
	if len(cfg.Recipients) == 0 {
		return nil, fmt.Errorf("daily reporter: no recipients")
	}
	if cfg.Hour < 0 || cfg.Hour > 23 {
		return nil, fmt.Errorf("daily reporter: hour out of range")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &DailyReporter{
		source:     cfg.Source,
		mailer:     cfg.Mailer,
		recipients: cfg.Recipients,
		storeName:  cfg.StoreName,
		hour:       cfg.Hour,
		now:        time.Now,
		logger:     cfg.Logger,
	}, nil
}

// Run blocks until ctx is cancelled. Wakes once per hour, checks the
// current UTC hour against the configured send hour, and if the send
// hour matches and today's report has not yet been sent it runs
// SendForDate. The check cadence is deliberately coarse — the daily
// report is a once-per-day job, and a ~1h granularity avoids drift
// and makes the scheduler trivially testable.
func (r *DailyReporter) Run(ctx context.Context) {
	r.logger.Info("daily report scheduler started",
		"hour_utc", r.hour,
		"recipients", len(r.recipients),
	)
	var lastSent time.Time
	check := func() {
		now := r.now().UTC()
		if now.Hour() != r.hour {
			return
		}
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		if !lastSent.Before(today) {
			return // already sent today
		}
		yesterday := today.AddDate(0, 0, -1)
		if err := r.SendForDate(ctx, yesterday); err != nil {
			r.logger.Error("daily report send failed", "error", err, "date", yesterday.Format("2006-01-02"))
			return
		}
		lastSent = today
	}

	check()
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			r.logger.Info("daily report scheduler stopping")
			return
		case <-ticker.C:
			check()
		}
	}
}

// SendForDate pulls the transactions that settled on the given date
// (00:00 UTC inclusive to next 00:00 UTC exclusive), builds the CSV,
// and emails it. Public so operators can re-send a missed day via a
// one-shot command if the scheduler drops.
func (r *DailyReporter) SendForDate(ctx context.Context, date time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)

	fetchCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	txns, err := r.source.FetchOrdersInWindow(fetchCtx, start, end)
	if err != nil {
		return fmt.Errorf("fetching transactions: %w", err)
	}

	csv, err := BuildCSV(start, txns)
	if err != nil {
		return fmt.Errorf("building csv: %w", err)
	}

	gross, fee, net, count := SummariseTotals(txns)
	storeTag := r.storeName
	if storeTag == "" {
		storeTag = "Shopify"
	}
	subject := fmt.Sprintf("[%s] Daily cash receipts — %s (%d)", storeTag, start.Format("2006-01-02"), count)
	body := fmt.Sprintf(
		"Daily cash-receipt report for %s.\n\n"+
			"Transactions: %d\n"+
			"Gross:        %.2f\n"+
			"Fees:         %.2f\n"+
			"Net:          %.2f\n\n"+
			"Full breakdown attached.\n",
		start.Format("2006-01-02"), count, gross, fee, net,
	)
	att := &Attachment{
		Filename:    fmt.Sprintf("cash-receipts-%s.csv", start.Format("2006-01-02")),
		ContentType: "text/csv; charset=utf-8",
		Body:        csv,
	}
	sendCtx, sendCancel := context.WithTimeout(ctx, 60*time.Second)
	defer sendCancel()
	if err := r.mailer.Send(sendCtx, r.recipients, subject, body, att); err != nil {
		return fmt.Errorf("sending email: %w", err)
	}
	r.logger.Info("daily report sent",
		"date", start.Format("2006-01-02"),
		"count", count,
		"gross", gross,
		"net", net,
	)
	return nil
}
