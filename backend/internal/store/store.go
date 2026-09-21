// Package store persists every observed price point.
//
// Logging each search result is what later makes price history and drop alerts
// possible without any extra collection: the data already flowed through.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver: no cgo, so the image stays static

	"github.com/monalbarse/kahan-hai/backend/internal/adapter"
)

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS observations (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    observed_at DATETIME NOT NULL,
    query       TEXT    NOT NULL,
    lat         REAL    NOT NULL,
    lon         REAL    NOT NULL,
    platform    TEXT    NOT NULL,
    product_id  TEXT    NOT NULL,
    name        TEXT    NOT NULL,
    brand       TEXT,
    variant     TEXT,
    image_url   TEXT,
    price_paise INTEGER NOT NULL,
    mrp_paise   INTEGER,
    in_stock    BOOLEAN NOT NULL,
    store_id    TEXT,
    deep_link   TEXT
);
CREATE INDEX IF NOT EXISTS idx_obs_lookup  ON observations (platform, product_id, observed_at);
CREATE INDEX IF NOT EXISTS idx_obs_query   ON observations (query, observed_at);
`

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// RecordSearch stores every product from a fan-out as one timestamped batch.
func (s *Store) RecordSearch(ctx context.Context, query string, loc adapter.Location, results []adapter.Result) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
        INSERT INTO observations
            (observed_at, query, lat, lon, platform, product_id, name, brand,
             variant, image_url, price_paise, mrp_paise, in_stock, store_id, deep_link)
        VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().UTC()
	for _, r := range results {
		for _, p := range r.Products {
			if _, err := stmt.ExecContext(ctx,
				now, query, loc.Lat, loc.Lon, string(p.Platform), p.ID, p.Name, p.Brand,
				p.Variant, p.ImageURL, p.PricePaise, p.MRPPaise, p.InStock, p.StoreID, p.DeepLink,
			); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// PricePoint is one historical price for a product.
type PricePoint struct {
	ObservedAt time.Time `json:"observedAt"`
	PricePaise int       `json:"pricePaise"`
	InStock    bool      `json:"inStock"`
}

// History returns the price trail for one product on one platform.
func (s *Store) History(ctx context.Context, platform, productID string, limit int) ([]PricePoint, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT observed_at, price_paise, in_stock
        FROM observations
        WHERE platform = ? AND product_id = ?
        ORDER BY observed_at DESC
        LIMIT ?`, platform, productID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PricePoint
	for rows.Next() {
		var p PricePoint
		if err := rows.Scan(&p.ObservedAt, &p.PricePaise, &p.InStock); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
