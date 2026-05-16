package parser

import (
	"fmt"
	"io"

	"github.com/lead/services/lead-import/internal/model"
	"github.com/xuri/excelize/v2"
)

// ParseXLSX reads the first sheet of an XLSX file and yields normalized rows.
func ParseXLSX(r io.Reader, mapping model.ColumnMapping, onRow func(rowNum int, row model.ParsedRow)) ([]model.RowError, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("xlsx has no sheets")
	}

	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, fmt.Errorf("read xlsx rows: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	headers := rows[0]
	var rowErrs []model.RowError
	for i, record := range rows[1:] {
		rowNum := i + 2
		fields := make(map[string]string, len(headers))
		for j, h := range headers {
			val := ""
			if j < len(record) {
				val = record[j]
			}
			canonical := mapping[h]
			if canonical == "" {
				canonical = h
			}
			fields[canonical] = val
		}
		onRow(rowNum, model.ParsedRow{Fields: fields})
	}
	return rowErrs, nil
}
