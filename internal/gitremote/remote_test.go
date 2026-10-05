package gitremote

import (
	"strings"
	"testing"

	"github.com/sifatulrabbi/prledger/internal/core"
)

func TestParseRemote(t *testing.T) {
	want := core.Repo{Owner: "octo", Name: "hello-world"}
	cases := []string{
		"https://github.com/octo/hello-world.git",
		"https://github.com/octo/hello-world",
		"https://github.com/octo/hello-world/",
		"https://someone@github.com/octo/hello-world.git",
		"git@github.com:octo/hello-world.git",
		"git@github.com:octo/hello-world",
		// SSH host aliases from ~/.ssh/config, used to pick an account.
		"git@personal.github.com:octo/hello-world.git",
		"ssh://git@github.com/octo/hello-world.git",
		"ssh://git@github.com:22/octo/hello-world",
		"  git@github.com:octo/hello-world.git\n",
	}
	for _, url := range cases {
		got, err := ParseRemote(url)
		if err != nil {
			t.Errorf("ParseRemote(%q) error: %v", url, err)
			continue
		}
		if got != want {
			t.Errorf("ParseRemote(%q) = %+v, want %+v", url, got, want)
		}
	}
}

func TestParseRemoteRejectsNonRepoURLs(t *testing.T) {
	for _, url := range []string{
		"",
		"not a url",
		"https://github.com/octo",
		"https://github.com/",
		"git@github.com:octo",
		"/local/path/to/repo",
		// A slash before the colon makes it a local path, not host:path.
		"/srv/git:octo/hello-world",
		"./mirror:octo/hello-world",
		"https://github.com/octo/hello-world/extra",
	} {
		if got, err := ParseRemote(url); err == nil {
			t.Errorf("ParseRemote(%q) = %+v, want an error", url, got)
		}
	}
}

func FuzzParseRemote(f *testing.F) {
	for _, seed := range []string{
		"https://github.com/octo/hello-world.git",
		"git@personal.github.com:octo/hello-world.git",
		"ssh://git@github.com:22/octo/hello-world",
		"",
		"::::",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, url string) {
		repo, err := ParseRemote(url)
		if err != nil {
			return
		}
		for _, part := range []string{repo.Owner, repo.Name} {
			if part == "" || strings.ContainsAny(part, "/: \t\n") {
				t.Fatalf("ParseRemote(%q) returned bad repo %+v", url, repo)
			}
		}
	})
}
