package pdf

import (
	"bytes"
	"fmt"
	"time"
)

// ExportData contains the data to embed in the consent-trail PDF.
type ExportData struct {
	LeadID      string
	ExportedAt  time.Time
	RecordCount int
}

// Generate produces a minimal valid PDF/1.4 document containing the consent trail.
func Generate(data ExportData) ([]byte, error) {
	line := fmt.Sprintf("Consent Trail  Lead: %s  Exported: %s  Records: %d",
		data.LeadID, data.ExportedAt.UTC().Format("2006-01-02T15:04:05Z"), data.RecordCount)
	stream := fmt.Sprintf("BT /F1 10 Tf 50 750 Td (%s) Tj ET", escapePDF(line))

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")

	offsets := make([]int, 5)

	offsets[0] = buf.Len()
	buf.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")

	offsets[1] = buf.Len()
	buf.WriteString("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")

	offsets[2] = buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" +
		" /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>\nendobj\n")

	offsets[3] = buf.Len()
	buf.WriteString("4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")

	offsets[4] = buf.Len()
	fmt.Fprintf(&buf, "5 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n",
		len(stream), stream)

	xrefPos := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 6\n0000000000 65535 f \n")
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefPos)

	return buf.Bytes(), nil
}

func escapePDF(s string) string {
	var b bytes.Buffer
	for _, c := range s {
		switch c {
		case '(', ')':
			b.WriteByte('\\')
			b.WriteRune(c)
		case '\\':
			b.WriteString("\\\\")
		default:
			if c >= 32 && c < 127 {
				b.WriteRune(c)
			}
		}
	}
	return b.String()
}
