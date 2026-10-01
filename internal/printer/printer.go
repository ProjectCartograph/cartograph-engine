// Package printer is the port through which a rendered document becomes a
// PDF. The engine and the API ask for a Printer and never for a browser;
// the chromium adapter beside this package is the one implementation
// today, and None is what runs where no browser is installed.
package printer

import (
	"context"
	"errors"
)

// ErrUnavailable is returned by a Printer that cannot print on this
// machine. Callers treat it as "no PDF", never as a failure of the
// document itself.
var ErrUnavailable = errors.New("no PDF printer is available")

// Printer prints HTML to PDF.
type Printer interface {
	// Print returns the PDF bytes for html, or ErrUnavailable (possibly
	// wrapped) when nothing on this machine can print.
	Print(ctx context.Context, html []byte) ([]byte, error)
}

// None is the Printer for a deployment without a browser: every call
// answers ErrUnavailable.
type None struct{}

// Print always returns ErrUnavailable.
func (None) Print(context.Context, []byte) ([]byte, error) {
	return nil, ErrUnavailable
}

// Func adapts a function to the Printer interface.
type Func func(ctx context.Context, html []byte) ([]byte, error)

// Print calls f.
func (f Func) Print(ctx context.Context, html []byte) ([]byte, error) {
	return f(ctx, html)
}
