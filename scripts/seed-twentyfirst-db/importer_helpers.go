package main

import (
	"compress/gzip"
	"encoding/csv"
	"io"
	"log"
	"os"
)

func streamCSV(path string) (<-chan map[string]string, error) {
	ch := make(chan map[string]string, 1000)

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	gr, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}

	go func() {
		defer f.Close()
		defer gr.Close()
		defer close(ch)

		r := csv.NewReader(gr)

		headers, err := r.Read()
		if err != nil {
			log.Printf("Error reading headers from %s: %v", path, err)
			return
		}

		for {
			record, err := r.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Printf("Error reading row from %s: %v", path, err)
				continue
			}

			row := make(map[string]string, len(headers))
			for i, header := range headers {
				if i < len(record) {
					row[header] = record[i]
				}
			}
			ch <- row
		}
	}()

	return ch, nil
}
