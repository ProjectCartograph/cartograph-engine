package main

import (
	"context"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/printer"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/printer/chromium"
)

// newPrinter picks the PDF printer a one-shot command uses: the browser
// named by hint (CARTOGRAPH_CHROMIUM, typically), else the vault's own
// settings, else whatever the chromium adapter resolves. printer.None when
// there is nothing to print with.
func newPrinter(ctx context.Context, e *engine.Engine, hint string) printer.Printer {
	if hint == "" {
		if settings, err := e.GetSettings(ctx); err == nil {
			hint = settings.Chromium
		}
	}
	path, err := chromium.Resolve(hint)
	if err != nil {
		return printer.None{}
	}
	return chromium.New(path)
}
