package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"runtime"
	rtmetrics "runtime/metrics"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/syncserver"
)

// metrics are what an operator scales and alerts on, served in the
// Prometheus text exposition format (version 0.0.4), because that is what
// KEDA's built-in Prometheus scaler and the Kubernetes autoscaling
// adapters read, and what an OpenTelemetry Collector's Prometheus
// receiver scrapes. The format is a few lines of text, so it is written
// here with the standard library rather than with a client library and
// its dependencies. The engine and the sync server know nothing of it:
// they keep plain counters, and this file, in the composition root,
// reads them.
type metrics struct {
	mu       sync.Mutex
	families []family
	requests map[[3]string]uint64 // handler, method, code
	duration map[string]*histogram
	start    time.Time
}

// family is one metric: its help, its type, and how to read its samples.
type family struct {
	name, help, kind string
	read             func() []sample
}

type sample struct {
	suffix string // "", "_bucket", "_sum" or "_count"
	labels [][2]string
	value  float64
}

// buckets are the request-duration histogram's upper bounds, in seconds:
// Prometheus's usual defaults.
var buckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}

type histogram struct {
	counts []uint64 // per bucket, not cumulative
	sum    float64
	count  uint64
}

func newMetrics() *metrics {
	m := &metrics{requests: map[[3]string]uint64{}, duration: map[string]*histogram{}, start: time.Now()}
	m.add("cartograph_http_requests_total", "HTTP requests answered, by handler (api, sync, ui, health), method and status code.", "counter", m.readRequests)
	m.add("cartograph_http_request_duration_seconds", "Time to answer an HTTP request, by handler. A sync socket counts until its upgrade.", "histogram", m.readDuration)
	m.runtime()
	return m
}

func (m *metrics) add(name, help, kind string, read func() []sample) {
	m.families = append(m.families, family{name: name, help: help, kind: kind, read: read})
}

// gauge and counter register a single, unlabelled value read on scrape.
func (m *metrics) gauge(name, help string, f func() float64) {
	m.add(name, help, "gauge", func() []sample { return []sample{{value: f()}} })
}

func (m *metrics) counter(name, help string, f func() float64) {
	m.add(name, help, "counter", func() []sample { return []sample{{value: f()}} })
}

// observe records one answered request.
func (m *metrics) observe(path, method string, code int, took time.Duration) {
	if m == nil {
		return
	}
	h := handlerOf(path)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests[[3]string{h, method, strconv.Itoa(code)}]++
	hist := m.duration[h]
	if hist == nil {
		hist = &histogram{counts: make([]uint64, len(buckets))}
		m.duration[h] = hist
	}
	s := took.Seconds()
	for i, b := range buckets {
		if s <= b {
			hist.counts[i]++
			break
		}
	}
	hist.sum += s
	hist.count++
}

func (m *metrics) readRequests() []sample {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]sample, 0, len(m.requests))
	for k, v := range m.requests {
		out = append(out, sample{labels: [][2]string{{"code", k[2]}, {"handler", k[0]}, {"method", k[1]}}, value: float64(v)})
	}
	return out
}

func (m *metrics) readDuration() []sample {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []sample
	for h, hist := range m.duration {
		var cum uint64
		for i, b := range buckets {
			cum += hist.counts[i]
			out = append(out, sample{"_bucket", [][2]string{{"handler", h}, {"le", strconv.FormatFloat(b, 'g', -1, 64)}}, float64(cum)})
		}
		out = append(out,
			sample{"_bucket", [][2]string{{"handler", h}, {"le", "+Inf"}}, float64(hist.count)},
			sample{"_sum", [][2]string{{"handler", h}}, hist.sum},
			sample{"_count", [][2]string{{"handler", h}}, float64(hist.count)})
	}
	return out
}

// runtime registers the process and Go runtime figures an operator looks
// at first, read from runtime/metrics.
func (m *metrics) runtime() {
	read := func(name string) float64 {
		s := []rtmetrics.Sample{{Name: name}}
		rtmetrics.Read(s)
		switch s[0].Value.Kind() {
		case rtmetrics.KindUint64:
			return float64(s[0].Value.Uint64())
		case rtmetrics.KindFloat64:
			return s[0].Value.Float64()
		}
		return math.NaN()
	}
	m.gauge("go_goroutines", "Goroutines that currently exist.", func() float64 { return float64(runtime.NumGoroutine()) })
	m.gauge("go_memstats_heap_alloc_bytes", "Bytes of heap objects in use.", func() float64 { return read("/memory/classes/heap/objects:bytes") })
	m.gauge("go_memstats_sys_bytes", "Bytes of memory the Go runtime obtained from the system.", func() float64 { return read("/memory/classes/total:bytes") })
	m.counter("go_gc_cycles_total", "Garbage collections completed.", func() float64 { return read("/gc/cycles/total:gc-cycles") })
	m.gauge("process_start_time_seconds", "When the process started, in Unix seconds.", func() float64 { return float64(m.start.Unix()) })
	if _, err := os.Stat("/proc/self/statm"); err == nil {
		page := float64(os.Getpagesize())
		m.gauge("process_resident_memory_bytes", "Resident memory of the process.", func() float64 {
			b, err := os.ReadFile("/proc/self/statm")
			if err != nil {
				return math.NaN()
			}
			f := strings.Fields(string(b))
			if len(f) < 2 {
				return math.NaN()
			}
			pages, _ := strconv.ParseFloat(f[1], 64)
			return pages * page
		})
	}
}

// handlerOf groups paths into a handful of handlers, so the label stays
// small whatever ids appear in paths.
func handlerOf(path string) string {
	switch {
	case path == "/api/v1/sync":
		return "sync"
	case strings.HasPrefix(path, "/api/"):
		return "api"
	case path == "/healthz" || path == "/readyz":
		return "health"
	}
	return "ui"
}

// watchShared and watchSync read the engine's and the sync server's
// counters when Prometheus scrapes.
func (m *metrics) watchShared(sh *engine.Shared) {
	if m == nil || sh == nil {
		return
	}
	m.gauge("cartograph_shared_documents_cached", "Shared documents this replica holds in memory.", func() float64 { return float64(sh.Stats().Cached) })
	m.counter("cartograph_shared_changes_stored_total", "Changes this replica appended to the document store.", func() float64 { return float64(sh.Stats().Stored) })
	m.counter("cartograph_shared_compactions_total", "Snapshots this replica wrote.", func() float64 { return float64(sh.Stats().Compactions) })
	m.counter("cartograph_shared_store_errors_total", "Appends to the document store that failed; the peer sends its change again.", func() float64 { return float64(sh.Stats().StoreErrors) })
}

func (m *metrics) watchSync(s *syncserver.Server) {
	if m == nil || s == nil {
		return
	}
	m.gauge("cartograph_sync_connections", "Peers connected to this replica's sync socket. Sum it across replicas to scale on.", func() float64 { return float64(s.Stats().Connections) })
	m.gauge("cartograph_sync_documents_open", "Documents at least one peer on this replica has open.", func() float64 { return float64(s.Stats().Documents) })
	m.counter("cartograph_sync_messages_received_total", "Sync messages received from peers.", func() float64 { return float64(s.Stats().Received) })
	m.counter("cartograph_sync_messages_sent_total", "Sync messages sent to peers.", func() float64 { return float64(s.Stats().Sent) })
	m.counter("cartograph_sync_refused_total", "Messages refused: a change from a principal who may only read.", func() float64 { return float64(s.Stats().Refused) })
}

// countedBus counts what goes out on the fan-out, so a deployment can see
// hints failing before anyone notices the latency.
type countedBus struct {
	fanout.Bus
	published, failed atomic.Int64
}

func (b *countedBus) Publish(ctx context.Context, topic string, data []byte) error {
	err := b.Bus.Publish(ctx, topic, data)
	if err != nil {
		b.failed.Add(1)
	} else {
		b.published.Add(1)
	}
	return err
}

func (m *metrics) watchFanout(b *countedBus) {
	if m == nil || b == nil {
		return
	}
	m.counter("cartograph_fanout_published_total", "Hints and presence published to other replicas.", func() float64 { return float64(b.published.Load()) })
	m.counter("cartograph_fanout_publish_errors_total", "Publishes the fan-out refused; peers elsewhere catch up at the next periodic offer.", func() float64 { return float64(b.failed.Load()) })
}

// handler serves the metrics at /metrics.
func (m *metrics) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		m.write(w)
	})
	return mux
}

// write renders every family in the text exposition format, with series
// sorted so two scrapes of the same state are byte-identical.
func (m *metrics) write(w io.Writer) {
	for _, f := range m.families {
		samples := f.read()
		sort.Slice(samples, func(i, j int) bool { return seriesKey(samples[i]) < seriesKey(samples[j]) })
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", f.name, escapeHelp(f.help), f.name, f.kind)
		for _, s := range samples {
			fmt.Fprintf(w, "%s%s%s %s\n", f.name, s.suffix, labelText(s.labels), formatValue(s.value))
		}
	}
}

func seriesKey(s sample) string {
	// Buckets sort by their bound as a number, not as text.
	key := s.suffix + labelText(s.labels)
	for _, l := range s.labels {
		if l[0] == "le" {
			v, err := strconv.ParseFloat(l[1], 64)
			if err != nil {
				v = math.Inf(1)
			}
			key = s.suffix + labelText(withoutLe(s.labels)) + fmt.Sprintf("%030.10f", v)
		}
	}
	return key
}

func withoutLe(labels [][2]string) [][2]string {
	var out [][2]string
	for _, l := range labels {
		if l[0] != "le" {
			out = append(out, l)
		}
	}
	return out
}

func labelText(labels [][2]string) string {
	if len(labels) == 0 {
		return ""
	}
	parts := make([]string, len(labels))
	for i, l := range labels {
		parts[i] = l[0] + `="` + escapeLabel(l[1]) + `"`
	}
	return "{" + strings.Join(parts, ",") + "}"
}

var (
	labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	helpEscaper  = strings.NewReplacer(`\`, `\\`, "\n", `\n`)
)

func escapeLabel(s string) string { return labelEscaper.Replace(s) }
func escapeHelp(s string) string  { return helpEscaper.Replace(s) }

func formatValue(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	case math.IsNaN(v):
		return "NaN"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}
