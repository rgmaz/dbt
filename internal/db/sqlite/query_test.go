package sqlite

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/rgmaz/dbt/internal/db"
)

func TestFormatCell(t *testing.T) {
	epoch := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		value any
		want  db.Cell
	}{
		{name: "null", value: nil, want: db.Cell{Null: true}},
		{name: "string", value: "hi", want: db.Cell{Text: "hi"}},
		{name: "empty string", value: "", want: db.Cell{Text: ""}},
		{name: "int", value: int64(42), want: db.Cell{Text: "42"}},
		{name: "float", value: 1.5, want: db.Cell{Text: "1.5"}},
		{name: "bool", value: true, want: db.Cell{Text: "true"}},
		{name: "time", value: epoch, want: db.Cell{Text: "2026-01-01T00:00:00Z"}},
		{name: "text bytes", value: []byte("hi"), want: db.Cell{Text: "hi"}},
		{name: "binary bytes", value: []byte{0x00, 0xFF}, want: db.Cell{Text: "0x00ff"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatCell(tt.value); got != tt.want {
				t.Errorf("formatCell(%v) = %+v, want %+v", tt.value, got, tt.want)
			}
		})
	}
}

func TestFormatBytesLargeBinary(t *testing.T) {
	value := bytes.Repeat([]byte{0xFF}, maxBinaryPreview+1)
	got := formatBytes(value)
	if !strings.HasPrefix(got, "0x") {
		t.Errorf("formatBytes() = %q, want hex prefix", got)
	}
	if !strings.HasSuffix(got, "(65 bytes)") {
		t.Errorf("formatBytes() = %q, want length suffix", got)
	}
}

func TestFormatBytesValidUTF8(t *testing.T) {
	if got := formatBytes([]byte("héllo")); got != "héllo" {
		t.Errorf("formatBytes() = %q, want text", got)
	}
}
