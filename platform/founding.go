package main

import (
	"fmt"
	"strings"
)

// founding.go implements the "first 10 founding operators" offer: the first N REAL external signups
// get plan="founding" — entitled with no expiry (free during beta) — and the landing banner shows
// live scarcity. Our own test/dogfood/founder accounts are excluded so they don't burn the slots.

const foundingCap = 10

// isInternalEmail reports whether an email is one of ours (test / dogfood / founder) and must NOT
// consume a founding slot. (15 such accounts already exist on the live DB.)
func isInternalEmail(email string) bool {
	e := strings.ToLower(strings.TrimSpace(email))
	for _, suf := range []string{".test", "@example.com", "@intrane.fr"} {
		if strings.HasSuffix(e, suf) {
			return true
		}
	}
	return false
}

// FoundingCount is how many founding slots are claimed. Founding is only ever granted to real
// external accounts, so this equals the number of real founding operators.
func (s *Store) FoundingCount() int {
	var n int
	s.db.QueryRow("SELECT COUNT(*) FROM users WHERE plan = 'founding'").Scan(&n)
	return n
}

// FoundingSlotsLeft is the remaining founding slots, clamped to [0, foundingCap].
func (s *Store) FoundingSlotsLeft() int {
	if left := foundingCap - s.FoundingCount(); left > 0 {
		return left
	}
	return 0
}

// foundingBanner renders the landing CTA. With slots left it pitches the founding offer + live
// scarcity; when full it falls back to the standard 48h-trial pitch.
func foundingBanner(left int) string {
	if left <= 0 {
		return `<div class=banner>
  <div class=flag>mago — autonomous agents that ship code over GitHub</div>
  <div class=sub>The founding cohort is full. Start with a 48-hour free trial, no card.</div>
  <code>curl -fsSL mago.intrane.fr/install.sh | sh</code>
</div>`
	}
	return fmt.Sprintf(`<div class=banner>
  <div class=flag>🏁 Founding operators — %d of %d slots left</div>
  <div class=sub>Run your autonomous company <b>free during beta</b>, with a direct line to the founder. Shape the product that runs your company.</div>
  <code>curl -fsSL mago.intrane.fr/install.sh | sh</code>
</div>`, left, foundingCap)
}
