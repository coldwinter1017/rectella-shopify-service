package inventory

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	_ "github.com/microsoft/go-mssqldb"
)

// SQLServerLister satisfies SKULister by querying a SQL Server view on
// RIL-DB01. Sarah's view `bq_WEBS_Whs_QoH` is the primary source of truth
// for the SKUs stocked in the WEBS warehouse — Shopify-first lister is
// kept as the fallback when this is nil or the query fails.
type SQLServerLister struct {
	db     *sql.DB
	query  string
	logger *slog.Logger
}

// NewSQLServerLister opens a pooled connection to SQL Server and returns a
// lister. The caller owns lifecycle — call Close on shutdown. If dsn is
// empty the constructor returns (nil, nil) so callers can trivially
// disable the lister without conditional logic at the call site.
func NewSQLServerLister(dsn string, logger *slog.Logger) (*SQLServerLister, error) {
	if dsn == "" {
		return nil, nil
	}
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlserver connection: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)
	return &SQLServerLister{
		db:     db,
		query:  "SELECT StockCode FROM bq_WEBS_Whs_QoH WHERE StockCode IS NOT NULL",
		logger: logger,
	}, nil
}

// newSQLServerListerWithDB is a test-only constructor that accepts a pre-built
// *sql.DB (typically a sqlmock). Keeps the production constructor free of
// injection surface.
func newSQLServerListerWithDB(db *sql.DB, query string, logger *slog.Logger) *SQLServerLister {
	return &SQLServerLister{db: db, query: query, logger: logger}
}

// Ping verifies the connection is usable. Call at boot for fail-fast.
func (l *SQLServerLister) Ping(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return l.db.PingContext(pingCtx)
}

// Close releases the underlying pool.
func (l *SQLServerLister) Close() error {
	if l.db == nil {
		return nil
	}
	return l.db.Close()
}

// ListAllSKUs returns deduplicated non-empty stock codes from the WEBS
// warehouse view. 10s timeout per call. Errors and empty results bubble
// up so the syncer can fall back to the Shopify lister.
func (l *SQLServerLister) ListAllSKUs(ctx context.Context) ([]string, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	rows, err := l.db.QueryContext(queryCtx, l.query)
	if err != nil {
		return nil, fmt.Errorf("querying sqlserver: %w", err)
	}
	defer func() { _ = rows.Close() }()

	seen := make(map[string]struct{})
	out := make([]string, 0, 64)
	for rows.Next() {
		var sku string
		if err := rows.Scan(&sku); err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}
		sku = strings.TrimSpace(sku)
		if sku == "" {
			continue
		}
		if _, ok := seen[sku]; ok {
			continue
		}
		seen[sku] = struct{}{}
		out = append(out, sku)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}
	return out, nil
}
