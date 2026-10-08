// Package jsonl keeps traced calls as JSON lines appended to a file, one
// call a line, for any tool that reads JSON lines (and for cartograph
// traces) to analyse. Several servers may append to one file on a shared
// volume: each line is written whole in one call.
package jsonl

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/trace"
)

// Recorder appends calls to a file.
type Recorder struct {
	mu sync.Mutex
	f  *os.File
}

// Open appends to the file at path, creating it.
func Open(path string) (*Recorder, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open trace file: %w", err)
	}
	return &Recorder{f: f}, nil
}

// Record appends one call. A write that fails is dropped: a trace never
// fails the call it records.
func (r *Recorder) Record(c trace.Call) {
	b, err := json.Marshal(c)
	if err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = r.f.Write(append(b, '\n'))
}

// Close closes the file.
func (r *Recorder) Close() error { return r.f.Close() }

// Read reads every call in a JSON lines stream, skipping lines that are
// not calls.
func Read(in io.Reader) ([]trace.Call, error) {
	var out []trace.Call
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var c trace.Call
		if json.Unmarshal(sc.Bytes(), &c) == nil && c.Tool != "" {
			out = append(out, c)
		}
	}
	return out, sc.Err()
}
