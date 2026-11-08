package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSaveBudget(t *testing.T) {
	now := time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)
	b := &saveBudget{now: func() time.Time { return now }}

	assert.True(t, b.allow("alice", saveBudgetBytes), "a fresh budget allows the full amount")
	assert.False(t, b.allow("alice", saveBudgetBytes+1), "nothing over the budget")

	b.add("alice", saveBudgetBytes-10)
	assert.True(t, b.allow("alice", 10))
	assert.False(t, b.allow("alice", 11), "the budget is used up")
	assert.True(t, b.allow("bob", saveBudgetBytes), "each person has their own budget")

	now = now.Add(saveBudgetWindow - time.Second)
	assert.False(t, b.allow("alice", 11), "still the same window")

	now = now.Add(time.Second)
	assert.True(t, b.allow("alice", saveBudgetBytes), "a new window starts fresh")

	b.add("bob", 1)
	assert.NotContains(t, b.used, "alice", "expired windows are swept")
	assert.Contains(t, b.used, "bob")
}

func TestSaveBudgetZeroValue(t *testing.T) {
	var b saveBudget
	assert.True(t, b.allow("alice", 1))
	b.add("alice", 1)
	assert.True(t, b.allow("alice", saveBudgetBytes-1))
	assert.False(t, b.allow("alice", saveBudgetBytes))
}
