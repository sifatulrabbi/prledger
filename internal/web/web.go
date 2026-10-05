// Package web holds the HTML frontend: one self-contained page that renders a
// Snapshot, either fetched from `prledger serve` or embedded by
// `prledger export html`.
package web

import _ "embed"

//go:embed index.html
var page []byte

// Page returns the page with no snapshot embedded; it fetches api/snapshot.
func Page() []byte { return page }
