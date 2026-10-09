// Package jsonl keeps people's acts as JSON lines appended to a file, one
// act a line, for any tool that reads JSON lines (and for cartograph ux)
// to analyse. Several servers may append to one file on a shared volume:
// each line is written whole in one call.
package jsonl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/activity"
)

// Recorder appends acts to a file.
type Recorder struct {
	mu sync.Mutex
	f  *os.File
}

// Open appends to the file at path, creating it.
func Open(path string) (*Recorder, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open activity file: %w", err)
	}
	return &Recorder{f: f}, nil
}

// Record appends one act. A write that fails is dropped: a trace never
// fails the act it records.
func (r *Recorder) Record(e activity.Event) {
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = r.f.Write(append(b, '\n'))
}

// Close closes the file.
func (r *Recorder) Close() error { return r.f.Close() }

// Read reads every act in a JSON lines stream, skipping lines that are
// not acts.
func Read(in io.Reader) ([]activity.Event, error) {
	var out []activity.Event
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var e activity.Event
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Name != "" {
			out = append(out, e)
		}
	}
	return out, sc.Err()
}

// Files reads the acts in one or more files a Recorder appended to.
type Files struct{ paths []string }

var (
	_ activity.Recorder = (*Recorder)(nil)
	_ activity.Reader   = Files{}
)

// NewReader reads the files at paths.
func NewReader(paths ...string) Files { return Files{paths: paths} }

// Read returns the acts q asks for from every file, in the order they
// happened.
func (f Files) Read(_ context.Context, q activity.Query) ([]activity.Event, error) {
	var out []activity.Event
	for _, path := range f.paths {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		acts, err := Read(file)
		_ = file.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		for _, a := range acts {
			if q.Matches(a) {
				out = append(out, a)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
}
