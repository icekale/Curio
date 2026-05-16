package playdiag

import (
	"fmt"
	"log"
	"sync"
	"time"
)

const maxRecords = 400

type Record struct {
	Time    time.Time `json:"time"`
	Message string    `json:"message"`
}

var (
	mu      sync.Mutex
	records []Record
)

func Printf(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	log.Print(message)
	mu.Lock()
	defer mu.Unlock()
	records = append(records, Record{
		Time:    time.Now(),
		Message: message,
	})
	if len(records) > maxRecords {
		copy(records, records[len(records)-maxRecords:])
		records = records[:maxRecords]
	}
}

func Records(limit int) []Record {
	mu.Lock()
	defer mu.Unlock()
	if limit <= 0 || limit > len(records) {
		limit = len(records)
	}
	out := make([]Record, limit)
	copy(out, records[len(records)-limit:])
	return out
}
