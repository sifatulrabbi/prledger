package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptrace"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sifatulrabbi/prledger/internal/core"
	"github.com/sifatulrabbi/prledger/internal/server"
)

// syncBuffer is a bytes.Buffer safe to read while serve writes to it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

var urlInOutput = regexp.MustCompile(`http://127\.0\.0\.1:\d+/`)

// startServe runs `prledger serve args...` until the test ends and returns
// the URL it serves on.
func startServe(t *testing.T, h *harness, args ...string) (url string, opened func() []string) {
	t.Helper()
	out := &syncBuffer{}
	h.deps.Stdout = out
	var mu sync.Mutex
	var opens []string
	h.deps.OpenBrowser = func(u string) error {
		mu.Lock()
		defer mu.Unlock()
		opens = append(opens, u)
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		root := NewRoot(h.deps)
		root.SetArgs(append([]string{"serve"}, args...))
		done <- root.ExecuteContext(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("serve returned %v after Ctrl-C, want nil", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("serve did not stop after Ctrl-C")
		}
	})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if u := urlInOutput.FindString(out.String()); u != "" {
			return u, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), opens...) }
		}
		select {
		case err := <-done:
			t.Fatalf("serve exited early: %v\noutput: %s", err, out.String())
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatalf("serve printed no URL: %q", out.String())
	return "", nil
}

func TestServeServesTheSnapshotAndOpensTheBrowser(t *testing.T) {
	h := newHarness(t)
	url, opened := startServe(t, h)

	res, err := http.Get(url + "api/snapshot")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var snap core.Snapshot
	if err := json.NewDecoder(res.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	if snap.Repo != "octo/hello-world" || len(snap.Groups) == 0 {
		t.Fatalf("snapshot = %+v", snap)
	}

	page, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	page.Body.Close()
	if page.StatusCode != 200 {
		t.Fatalf("GET / = %d", page.StatusCode)
	}
	if got := opened(); len(got) != 1 || got[0] != url {
		t.Fatalf("opened = %q, want [%q]", got, url)
	}
}

func TestServeNoOpenLeavesTheBrowserAlone(t *testing.T) {
	h := newHarness(t)
	_, opened := startServe(t, h, "--no-open")
	if got := opened(); len(got) != 0 {
		t.Fatalf("opened = %q, want nothing", got)
	}
}

// Regression: Ctrl-C while the page waited on a slow refresh made serve exit
// with "context deadline exceeded" after the 5s shutdown timeout.
func TestCtrlCDuringASlowRefreshExitsCleanly(t *testing.T) {
	h := newHarness(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) }) // runs after serve has stopped
	h.gh.respond = func(argv []string) ([]byte, error) {
		<-release // gh hangs until the test ends
		return nil, errors.New("gh: released")
	}
	url, _ := startServe(t, h, "--no-open")

	sent := make(chan struct{})
	go func() {
		trace := &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) { close(sent) }}
		req, _ := http.NewRequestWithContext(httptrace.WithClientTrace(context.Background(), trace), "POST", url+"api/refresh", nil)
		req.Header.Set(server.RequestHeader, "1")
		if res, err := http.DefaultClient.Do(req); err == nil {
			res.Body.Close()
		}
	}()
	<-sent
	// The request is on the wire and its handler is waiting on the hung gh.
	// startServe's cleanup now presses Ctrl-C and requires a nil error within 5s.
}

func TestServeFailsFastOnBadConfig(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(t, "defaults:\n  limit: -1\n")
	err := h.run("serve", "--no-open")
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("err = %v, want the config error before serving", err)
	}
}
