package inventory

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func newTestLister(t *testing.T) (*SQLServerLister, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	lister := newSQLServerListerWithDB(db, "SELECT StockCode FROM bq_WEBS_Whs_QoH WHERE StockCode IS NOT NULL", logger)
	return lister, mock, func() { _ = db.Close() }
}

func TestSQLServerLister_HappyPath(t *testing.T) {
	lister, mock, cleanup := newTestLister(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"StockCode"}).
		AddRow("CBBQ0001").
		AddRow("CBBQ0002").
		AddRow("LUMP0148")
	mock.ExpectQuery("SELECT StockCode FROM bq_WEBS_Whs_QoH WHERE StockCode IS NOT NULL").
		WillReturnRows(rows)

	skus, err := lister.ListAllSKUs(context.Background())
	if err != nil {
		t.Fatalf("ListAllSKUs: %v", err)
	}
	if len(skus) != 3 {
		t.Fatalf("want 3 skus, got %d: %v", len(skus), skus)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestSQLServerLister_EmptyResult(t *testing.T) {
	lister, mock, cleanup := newTestLister(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"StockCode"})
	mock.ExpectQuery("SELECT StockCode FROM bq_WEBS_Whs_QoH WHERE StockCode IS NOT NULL").
		WillReturnRows(rows)

	skus, err := lister.ListAllSKUs(context.Background())
	if err != nil {
		t.Fatalf("ListAllSKUs: %v", err)
	}
	if len(skus) != 0 {
		t.Fatalf("want 0 skus, got %d", len(skus))
	}
}

func TestSQLServerLister_SkipsBlankAndDedupes(t *testing.T) {
	lister, mock, cleanup := newTestLister(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"StockCode"}).
		AddRow("CBBQ0001").
		AddRow(" CBBQ0001 "). // duplicate with whitespace
		AddRow("").            // blank
		AddRow("   ").         // whitespace only
		AddRow("CBBQ0002")
	mock.ExpectQuery("SELECT StockCode FROM bq_WEBS_Whs_QoH WHERE StockCode IS NOT NULL").
		WillReturnRows(rows)

	skus, err := lister.ListAllSKUs(context.Background())
	if err != nil {
		t.Fatalf("ListAllSKUs: %v", err)
	}
	if len(skus) != 2 {
		t.Fatalf("want 2 unique skus, got %d: %v", len(skus), skus)
	}
}

func TestSQLServerLister_DriverError(t *testing.T) {
	lister, mock, cleanup := newTestLister(t)
	defer cleanup()

	mock.ExpectQuery("SELECT StockCode FROM bq_WEBS_Whs_QoH WHERE StockCode IS NOT NULL").
		WillReturnError(errors.New("connection refused"))

	_, err := lister.ListAllSKUs(context.Background())
	if err == nil {
		t.Fatal("want error, got nil")
	}
}

func TestSQLServerLister_ContextCancel(t *testing.T) {
	lister, mock, cleanup := newTestLister(t)
	defer cleanup()

	rows := sqlmock.NewRows([]string{"StockCode"}).AddRow("CBBQ0001")
	mock.ExpectQuery("SELECT StockCode FROM bq_WEBS_Whs_QoH WHERE StockCode IS NOT NULL").
		WillDelayFor(200 * time.Millisecond).
		WillReturnRows(rows)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := lister.ListAllSKUs(ctx)
	if err == nil {
		t.Fatal("want context error, got nil")
	}
}

func TestNewSQLServerLister_EmptyDSN(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	lister, err := NewSQLServerLister("", logger)
	if err != nil {
		t.Fatalf("want nil error for empty DSN, got %v", err)
	}
	if lister != nil {
		t.Fatal("want nil lister for empty DSN")
	}
}
