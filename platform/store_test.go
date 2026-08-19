package main

import (
	"testing"
	"time"
)

func TestUserTrialActive(t *testing.T) {
	future := time.Now().Add(time.Hour).Unix()
	past := time.Now().Add(-time.Hour).Unix()

	cases := []struct {
		plan      string
		trialEnds int64
		want      bool
	}{
		{"trial", future, true},
		{"trial", past, false},
		{"trial", 0, false},
		{"mago", future, false},
		{"free", future, false},
	}
	for _, c := range cases {
		u := &User{Plan: c.plan, TrialEnds: c.trialEnds}
		if got := u.trialActive(); got != c.want {
			t.Errorf("trialActive(plan=%q, trialEnds=%d) = %v, want %v", c.plan, c.trialEnds, got, c.want)
		}
	}
}

func TestUserEntitled(t *testing.T) {
	future := time.Now().Add(time.Hour).Unix()
	past := time.Now().Add(-time.Hour).Unix()

	cases := []struct {
		plan      string
		trialEnds int64
		want      bool
	}{
		{"mago", 0, true},
		{"founding", 0, true},
		{"trial", future, true},
		{"trial", past, false},
		{"free", 0, false},
		{"free", future, false},
	}
	for _, c := range cases {
		u := &User{Plan: c.plan, TrialEnds: c.trialEnds}
		if got := u.entitled(); got != c.want {
			t.Errorf("entitled(plan=%q, trialEnds=%d) = %v, want %v", c.plan, c.trialEnds, got, c.want)
		}
	}
}
