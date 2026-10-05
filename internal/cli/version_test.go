package cli

import (
	"bytes"
	"runtime/debug"
	"testing"
)

func TestVersionCommandPrintsVersion(t *testing.T) {
	var out bytes.Buffer
	root := NewRoot(Deps{Stdout: &out, Stderr: &out, Version: "v1.2.3"})
	root.SetArgs([]string{"version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got, want := out.String(), "prledger v1.2.3\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestResolveVersion(t *testing.T) {
	module := func(v string) *debug.BuildInfo { return &debug.BuildInfo{Main: debug.Module{Version: v}} }

	cases := []struct {
		name   string
		ldflag string
		info   *debug.BuildInfo
		want   string
	}{
		{"ldflag wins over module version", "v0.3.0", module("v0.2.0"), "v0.3.0"},
		{"go install @version uses module version", "", module("v0.2.0"), "v0.2.0"},
		{"local build without ldflag is dev", "", module("(devel)"), "dev"},
		{"no build info is dev", "", nil, "dev"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveVersion(c.ldflag, c.info); got != c.want {
				t.Fatalf("ResolveVersion = %q, want %q", got, c.want)
			}
		})
	}
}
