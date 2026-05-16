package parser

import (
	"encoding/csv"
	"fmt"
	"io"

	"github.com/lead/services/lead-import/internal/model"
)

// ParseCSV streams a CSV reader and yields normalized rows via the callback.
// Row-level errors are collected and returned; they are non-fatal.
func ParseCSV(r io.Reader, mapping model.ColumnMapping, onRow func(rowNum int, row model.ParsedRow)) ([]model.RowError, error) {
	cr := csv.NewReader(r)
	cr.LazyQuotes = true
	cr.TrimLeadingSpace = true

	headers, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("read CSV header: %w", err)
	}

	var rowErrs []model.RowError
	rowNum := 1
	for {
		record, err := cr.Read()
		if err == io.EOF {
			break
		}
		rowNum++
		if err != nil {
			rowErrs = append(rowErrs, model.RowError{Row: rowNum, Message: err.Error()})
			continue
		}

		fields := make(map[string]string, len(headers))
		for i, h := range headers {
			if i < len(record) {
				canonical := mapping[h]
				if canonical == "" {
					canonical = h
				}
				fields[canonical] = record[i]
			}
		}
		onRow(rowNum, model.ParsedRow{Fields: fields})
	}
	return rowErrs, nil
}
