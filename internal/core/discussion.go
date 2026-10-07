package core

import (
	"cmp"
	"slices"
	"time"
)

// Discussion is what people said on an open PR. Bots' comments never get
// here; the PR author's own are dropped when it is applied.
type Discussion struct {
	Number  int
	Notes   []Note // every comment: review texts, comments on the code, the conversation
	Threads []Thread
}

// Note is one comment and who wrote it.
type Note struct {
	Login string
	At    time.Time
}

// Thread is a review conversation on the code.
type Thread struct {
	Opener   string // who started it; "" for a bot or a deleted account
	Resolved bool
}

// Commenter is someone other than the author who commented on a PR.
type Commenter struct {
	Login      string    `json:"login"`
	Comments   int       `json:"comments"`
	Unresolved int       `json:"unresolved,omitempty"` // review threads they opened, still open
	LastAt     time.Time `json:"lastAt,omitzero"`      // zero when none of their comments came back
}

// apply adds who commented to p, newest first.
func (t Discussion) apply(p PR) PR {
	byLogin := map[string]*Commenter{}
	get := func(login string) *Commenter {
		c, ok := byLogin[login]
		if !ok {
			c = &Commenter{Login: login}
			byLogin[login] = c
		}
		return c
	}
	for _, n := range t.Notes {
		if n.Login == "" || n.Login == p.Author {
			continue
		}
		c := get(n.Login)
		c.Comments++
		if n.At.After(c.LastAt) {
			c.LastAt = n.At
		}
	}
	// An open thread is feedback only when a person other than the author
	// started it: the author's notes to self and bots' remarks ask nothing,
	// even when someone replies.
	for _, th := range t.Threads {
		if !th.Resolved && th.Opener != "" && th.Opener != p.Author {
			get(th.Opener).Unresolved++
		}
	}
	p.Commenters = nil
	for _, c := range byLogin {
		p.Commenters = append(p.Commenters, *c)
	}
	slices.SortFunc(p.Commenters, func(a, b Commenter) int {
		return cmp.Or(b.LastAt.Compare(a.LastAt), cmp.Compare(a.Login, b.Login))
	})
	return p
}

// Unresolved counts the review threads on p that others opened and nobody
// resolved yet.
func (p PR) Unresolved() int {
	n := 0
	for _, c := range p.Commenters {
		n += c.Unresolved
	}
	return n
}
