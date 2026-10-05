// Package web holds the HTML frontend: one self-contained page that renders a
// Snapshot, either fetched from `prledger serve` or embedded by
// `prledger export html`.
package web

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/sifatulrabbi/prledger/internal/core"
)

//go:embed index.html
var page []byte

const emptyDataTag = `<script id="prledger-data" type="application/json"></script>`

// Page returns the page with no snapshot embedded; it fetches api/snapshot.
func Page() []byte { return page }

// Export returns the page with snap embedded, for viewing without a server.
// encoding/json writes <, > and & as Unicode escapes (backslash-u003c and so
// on), so no text in the snapshot can close the script tag.
func Export(snap core.Snapshot) ([]byte, error) {
	if bytes.Count(page, []byte(emptyDataTag)) != 1 {
		return nil, errors.New("page template has lost its data tag")
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return nil, err
	}
	filled := `<script id="prledger-data" type="application/json">` + string(data) + `</script>`
	return bytes.Replace(page, []byte(emptyDataTag), []byte(filled), 1), nil
}
