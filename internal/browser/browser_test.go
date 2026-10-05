package browser

import (
	"reflect"
	"testing"
)

func TestCommandPerOS(t *testing.T) {
	const url = "http://127.0.0.1:1234/"
	cases := map[string][]string{
		"darwin":  {"open", url},
		"linux":   {"xdg-open", url},
		"freebsd": {"xdg-open", url},
		"windows": {"rundll32", "url.dll,FileProtocolHandler", url},
	}
	for goos, want := range cases {
		if got := command(goos, url); !reflect.DeepEqual(got, want) {
			t.Errorf("command(%q) = %q, want %q", goos, got, want)
		}
	}
}
