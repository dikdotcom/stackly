package report

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// RenderPDF converts an HTML string to PDF using a headless Chromium
// invocation. Returns the PDF bytes.
//
// Each call creates a fresh chromedp context — these are cheap (the
// browser binary is reused via the allocator) but not free. For high
// QPS, consider a worker pool with a shared allocator.
func RenderPDF(htmlContent string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.DisableGPU,
		chromedp.NoSandbox,
		chromedp.Flag("headless", true),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, opts...)
	defer allocCancel()

	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	// Wrap HTML in a data URL so we don't need a temp file.
	dataURL := "data:text/html;charset=utf-8," + escapeForDataURL(htmlContent)

	var buf []byte
	err := chromedp.Run(browserCtx,
		chromedp.Navigate(dataURL),
		// Wait for the body to render before printing.
		chromedp.WaitReady("body"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var perr error
			buf, _, perr = page.PrintToPDF().
				WithPrintBackground(true).
				WithPaperWidth(8.5). // Letter
				WithPaperHeight(11).
				WithMarginTop(0.4).
				WithMarginBottom(0.4).
				WithMarginLeft(0.4).
				WithMarginRight(0.4).
				Do(ctx)
			return perr
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("chromedp print: %w", err)
	}
	if len(buf) == 0 {
		return nil, fmt.Errorf("empty PDF buffer")
	}
	return buf, nil
}

// escapeForDataURL makes a string safe to embed in a data:text/html URL.
// Most chars are URL-safe after percent-encoding; we keep it minimal to
// avoid bloating the URL.
func escapeForDataURL(s string) string {
	// The simplest robust approach: write to a temp file and use file://.
	// But that's I/O we don't want. Instead we use the fact that most of
	// our HTML is ASCII-safe; only quotes, percent, and non-ASCII need
	// escaping.
	out := make([]byte, 0, len(s)*2)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '#' || c == '%' || c == '?' || c == '\n' || c == '\r':
			out = append(out, '%', hexDigit(c>>4), hexDigit(c&0xF))
		case c < 0x20 || c >= 0x7F:
			// Non-ASCII bytes — escape as %XX
			out = append(out, '%', hexDigit(c>>4), hexDigit(c&0xF))
		default:
			out = append(out, c)
		}
	}
	return string(out)
}

func hexDigit(b byte) byte {
	if b < 10 {
		return '0' + b
	}
	return 'A' + (b - 10)
}
