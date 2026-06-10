// Package mentions extracts @<worker-name> tokens from a comment
// body and resolves them against a candidate agent list. Used by
// V0.8's squad leader → worker delegation: when an agent posts a
// comment that mentions a squad worker, CommentService.PostAgent
// uses this to decide which workers (if any) to wake.
//
// Resolution rules:
//   - Match is case-insensitive on agent.Name.
//   - Whitespace, beginning-of-string, or simple punctuation must
//     precede the @ to count as a mention (avoids matching email
//     addresses or in-prose mid-word @s).
//   - Names with spaces are NOT supported in V0.8 -- mention syntax
//     greedily captures `[A-Za-z0-9_.-]+`. Squad members should be
//     given short single-token names. Multi-word names still work as
//     agents; they just can't be mentioned. Documented for the user
//     in the Squads UI.
//   - When two agents in the candidate set share a name (case-
//     insensitive), the user must disambiguate with the `#<shortid>`
//     suffix Multica also uses: `@coder#a1b2c3d4`. Without a suffix
//     the ambiguous mention is dropped (not silently routed to the
//     first match -- that would surprise the user).
//   - Self-mentions are NOT filtered here -- the caller (the
//     self-trigger guard in CommentService) makes that decision
//     using lifecycle context this package doesn't have.
package mentions

import (
	"regexp"
	"strings"
)

// Candidate is the minimum agent shape this package needs. Caller
// passes a slice of these so we don't import the store package
// (keeps the parser pure and unit-testable in isolation).
type Candidate struct {
	ID   string
	Name string
}

// mentionRE matches an at-sign that is at the start of input or
// preceded by whitespace / common punctuation, followed by a
// name token, optionally followed by `#<shortid>` for
// disambiguation.
//
// Examples that match:
//
//	"@coder do X"                    -> coder
//	"hi @coder, please do X"         -> coder
//	"plan: @coder#a1b2c3d4 owns this" -> coder#a1b2c3d4
//
// Examples that DON'T match:
//
//	"contact me@example.com"   -> "me" preceded by no separator
//	"prices like $100@5units"  -> "5units" preceded by digit
//
// The leading non-capturing group eats the preceding separator
// (or nothing, when the mention is at the start of the string).
var mentionRE = regexp.MustCompile(`(?:^|[\s,.;:!?(\[{])@([A-Za-z0-9_.\-]+)(?:#([A-Za-z0-9]{4,12}))?`)

// Parse returns the set of candidate agent IDs mentioned in body
// that resolve unambiguously. Order matches first-occurrence in
// the body; duplicates collapse to the first hit.
func Parse(body string, candidates []Candidate) []string {
	if body == "" || len(candidates) == 0 {
		return nil
	}

	// Build a case-insensitive name -> []candidate index so we can
	// detect ambiguous bare-name mentions vs uniquely resolvable.
	byName := make(map[string][]Candidate, len(candidates))
	for _, c := range candidates {
		key := strings.ToLower(c.Name)
		byName[key] = append(byName[key], c)
	}

	seen := make(map[string]bool, 4)
	var out []string

	matches := mentionRE.FindAllStringSubmatch(body, -1)
	for _, m := range matches {
		name := strings.ToLower(m[1])
		shortID := m[2] // may be empty

		// With a shortID suffix we match exactly: name AND id-prefix
		// must both line up. This is how the user breaks a tie.
		if shortID != "" {
			for _, c := range byName[name] {
				if strings.HasPrefix(c.ID, shortID) {
					if !seen[c.ID] {
						seen[c.ID] = true
						out = append(out, c.ID)
					}
					break
				}
			}
			continue
		}

		// Bare mention: only resolve when exactly one candidate has
		// the name. Multiple matches are dropped because we'd be
		// guessing which one the user meant.
		cs := byName[name]
		if len(cs) != 1 {
			continue
		}
		c := cs[0]
		if !seen[c.ID] {
			seen[c.ID] = true
			out = append(out, c.ID)
		}
	}
	return out
}
