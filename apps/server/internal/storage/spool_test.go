package storage

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestSpoolToTemp(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{name: "small payload", data: []byte("streamed backup payload")},
		{name: "empty payload", data: []byte{}},
		{name: "binary payload", data: []byte{0x00, 0xff, 0x10, 0x00, 0x89}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Service{}
			f, n, err := s.spoolToTemp(bytes.NewReader(tt.data))
			if err != nil {
				t.Fatalf("spoolToTemp: %v", err)
			}
			defer func() {
				_ = f.Close()
				_ = os.Remove(f.Name())
			}()

			if n != int64(len(tt.data)) {
				t.Fatalf("size = %d, want %d", n, len(tt.data))
			}

			got, err := io.ReadAll(f)
			if err != nil {
				t.Fatalf("read spooled file: %v", err)
			}
			if !bytes.Equal(got, tt.data) {
				t.Fatalf("spooled bytes differ: got %x want %x", got, tt.data)
			}
		})
	}
}
