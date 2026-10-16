// Package fetch downloads pinned release assets for explicit install commands.
// Callers verify size and SHA-256 against frontend pins; this package only
// moves bytes. Builds and tests never call it.
package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

// Get copies the asset at rawURL into w. https:// and http:// use a plain
// HTTP GET (redirects followed, as GitHub release downloads require);
// file:// reads a local file, for mirrors and offline tests.
func Get(ctx context.Context, rawURL string, w io.Writer) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	switch u.Scheme {
	case "file":
		if u.Host != "" && u.Host != "localhost" {
			return fmt.Errorf("file URL %s must name a local path", rawURL)
		}
		f, err := os.Open(u.Path)
		if err != nil {
			return fmt.Errorf("download %s: %w", rawURL, err)
		}
		defer f.Close()
		_, err = io.Copy(w, contextReader{ctx, f})
		return err
	case "https", "http":
		req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
		if err != nil {
			return err
		}
		client := http.Client{Timeout: 30 * time.Minute}
		response, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("download %s: %w", rawURL, err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("download %s: HTTP %s", rawURL, response.Status)
		}
		_, err = io.Copy(w, response.Body)
		return err
	default:
		return fmt.Errorf("unsupported download URL %q (use https://, http:// or file://)", rawURL)
	}
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}
