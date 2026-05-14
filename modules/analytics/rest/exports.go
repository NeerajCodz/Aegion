package rest

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// DefaultExportBuilder implements ExportBuilder
type DefaultExportBuilder struct {
	db Database
}

// NewExportBuilder creates a new export builder
func NewExportBuilder(db Database) *DefaultExportBuilder {
	return &DefaultExportBuilder{db: db}
}

// ExportCSV exports results as CSV
func (eb *DefaultExportBuilder) ExportCSV(ctx context.Context, sql string, w http.ResponseWriter) error {
	rows, err := eb.db.Query(ctx, sql)
	if err != nil {
		return err
	}

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write headers
	if len(rows) > 0 {
		headers := make([]string, 0)
		for key := range rows[0] {
			headers = append(headers, key)
		}
		if err := writer.Write(headers); err != nil {
			return err
		}

		// Write data
		for _, row := range rows {
			record := make([]string, 0)
			for _, header := range headers {
				value := fmt.Sprintf("%v", row[header])
				record = append(record, value)
			}
			if err := writer.Write(record); err != nil {
				return err
			}
		}
	}

	return nil
}

// ExportJSON exports results as JSON array
func (eb *DefaultExportBuilder) ExportJSON(ctx context.Context, sql string, w http.ResponseWriter) error {
	rows, err := eb.db.Query(ctx, sql)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(rows)
}

// ExportParquet exports results as Parquet
// This is a simplified implementation - production should use actual Parquet library
func (eb *DefaultExportBuilder) ExportParquet(ctx context.Context, sql string, w http.ResponseWriter) error {
	rows, err := eb.db.Query(ctx, sql)
	if err != nil {
		return err
	}

	// In production, use github.com/xiaojiaoyu100/parquet or parquet-go
	// For now, write as JSON which can be converted to Parquet by external tools
	encoder := json.NewEncoder(w)
	return encoder.Encode(rows)
}

// StreamExportCSV streams CSV export (for large datasets)
func (eb *DefaultExportBuilder) StreamExportCSV(ctx context.Context, sql string, w http.ResponseWriter, chunkSize int) error {
	// This would implement streaming with pagination to handle large datasets
	// For now, use the standard export
	return eb.ExportCSV(ctx, sql, w)
}

// StreamExportJSON streams JSON export (for large datasets)
func (eb *DefaultExportBuilder) StreamExportJSON(ctx context.Context, sql string, w http.ResponseWriter, chunkSize int) error {
	rows, err := eb.db.Query(ctx, sql)
	if err != nil {
		return err
	}

	_, _ = io.WriteString(w, "[\n")
	for i, row := range rows {
		if i > 0 {
			_, _ = io.WriteString(w, ",\n")
		}

		data, err := json.MarshalIndent(row, "  ", "  ")
		if err != nil {
			return err
		}

		_, err = w.Write(data)
		if err != nil {
			return err
		}
	}
	_, _ = io.WriteString(w, "\n]")

	return nil
}
