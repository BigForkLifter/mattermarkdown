package main

import (
	"sync"
	"time"

	"github.com/pkg/errors"
)

const (
	// saveBudgetBytes is how much document content one person may store per saveBudgetWindow,
	// counting new documents and saves together. Every save keeps a full copy as a version in
	// Mattermost's own database, so without a limit one person could fill its disk. Normal
	// editing uses a small fraction of this.
	saveBudgetBytes  = 20 << 20
	saveBudgetWindow = time.Hour
)

var errSaveBudget = errors.New("you've saved a lot in the last hour; wait a few minutes and try again (your changes are still in the editor)")

// saveBudget counts the bytes each person has stored in the current window. The zero value
// is ready to use. It is in memory only, so a restart resets it.
type saveBudget struct {
	mu        sync.Mutex
	used      map[string]*budgetUse
	lastSweep time.Time

	// now is time.Now outside tests.
	now func() time.Time
}

type budgetUse struct {
	start time.Time
	bytes int64
}

func (b *saveBudget) clock() time.Time {
	if b.now != nil {
		return b.now()
	}
	return time.Now()
}

// allow reports whether userID may store n more bytes now. It doesn't count them: call add
// once the content is stored, so failed saves cost nothing.
func (b *saveBudget) allow(userID string, n int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	var used int64
	if u := b.current(userID, b.clock()); u != nil {
		used = u.bytes
	}
	return used+n <= saveBudgetBytes
}

// add counts n stored bytes against userID.
func (b *saveBudget) add(userID string, n int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clock()
	if b.used == nil {
		b.used = map[string]*budgetUse{}
	}
	if now.Sub(b.lastSweep) >= saveBudgetWindow {
		for id, u := range b.used {
			if now.Sub(u.start) >= saveBudgetWindow {
				delete(b.used, id)
			}
		}
		b.lastSweep = now
	}
	u := b.current(userID, now)
	if u == nil {
		u = &budgetUse{start: now}
		b.used[userID] = u
	}
	u.bytes += n
}

// current returns userID's use in the window running at now, or nil if none is running.
func (b *saveBudget) current(userID string, now time.Time) *budgetUse {
	u := b.used[userID]
	if u == nil || now.Sub(u.start) >= saveBudgetWindow {
		return nil
	}
	return u
}
