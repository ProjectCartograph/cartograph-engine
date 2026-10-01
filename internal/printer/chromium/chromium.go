// Package chromium prints HTML to PDF with a headless Chromium. It is the
// one printer.Printer adapter today. The browser is found once, lazily, in
// this order: the path this adapter was built with, the CHROMIUM
// environment variable, then whatever the PATH calls chromium under any
// of the names the various builds use. A hard-coded /usr/bin/chromium was
// true on exactly one machine.
package chromium

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/printer"
)

// Printer prints with the browser at Path, or with one it resolves.
type Printer struct {
	// Path is the browser binary to use. Empty means resolve one.
	Path string

	once     sync.Once
	resolved string
	resolveE error
}

var _ printer.Printer = (*Printer)(nil)

// New returns a Printer that uses the given browser, or resolves one when
// path is empty.
func New(path string) *Printer {
	return &Printer{Path: path}
}

// Resolve finds the browser to print with. See the package comment for
// the order. Exported so a command line can report which browser it
// would use.
func Resolve(hint string) (string, error) {
	if hint != "" {
		return hint, nil
	}
	if fromEnv := os.Getenv("CHROMIUM"); fromEnv != "" {
		return fromEnv, nil
	}
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
		if found, err := exec.LookPath(name); err == nil {
			return found, nil
		}
	}
	return "", fmt.Errorf("%w: set CARTOGRAPH_CHROMIUM or CHROMIUM, name one in the vault's settings (spec.chromium), or put chromium on PATH", printer.ErrUnavailable)
}

// Print prints html to PDF. The document carries its own print
// stylesheet (A4 margins, sections kept together), so what comes out is
// the page a person would print themselves.
func (p *Printer) Print(ctx context.Context, html []byte) ([]byte, error) {
	p.once.Do(func() { p.resolved, p.resolveE = Resolve(p.Path) })
	if p.resolveE != nil {
		return nil, p.resolveE
	}
	dir, err := os.MkdirTemp("", "cartograph-pdf-")
	if err != nil {
		return nil, fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(dir)
	in := filepath.Join(dir, "document.html")
	out := filepath.Join(dir, "document.pdf")
	if err := os.WriteFile(in, html, 0o644); err != nil {
		return nil, fmt.Errorf("write html: %w", err)
	}
	cmd := exec.CommandContext(ctx, p.resolved, "--headless=new", "--no-sandbox", "--disable-gpu",
		"--no-pdf-header-footer", "--user-data-dir="+filepath.Join(dir, "profile"),
		"--print-to-pdf="+out, "file://"+in)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("chromium: %w: %s", err, stderr.String())
	}
	return os.ReadFile(out)
}
