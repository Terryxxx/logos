package mentions

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	// Agent IDs in production are UUIDs; the disambiguation suffix
	// is the first 8 chars of that UUID. Match that shape here so
	// the HasPrefix logic exercises the same path as runtime.
	cands := []Candidate{
		{ID: "1234abcd-1111-1111-1111-111111111111", Name: "coder"},
		{ID: "5678efgh-2222-2222-2222-222222222222", Name: "Coder"},  // case-insensitive duplicate
		{ID: "99ee88ff-3333-3333-3333-333333333333", Name: "reviewer"},
		{ID: "aabbccdd-4444-4444-4444-444444444444", Name: "planner"},
	}

	tests := []struct {
		name string
		body string
		want []string
	}{
		{"empty body", "", nil},
		{"no mentions", "just plain text", nil},
		{"bare unique mention", "@reviewer please take a look", []string{"99ee88ff-3333-3333-3333-333333333333"}},
		{"mid-sentence", "hi @reviewer, can you check?", []string{"99ee88ff-3333-3333-3333-333333333333"}},
		{"case-insensitive name", "@Reviewer pls", []string{"99ee88ff-3333-3333-3333-333333333333"}},
		{
			"two distinct mentions, first-occurrence order",
			"@planner draft it, then @reviewer audit",
			[]string{"aabbccdd-4444-4444-4444-444444444444", "99ee88ff-3333-3333-3333-333333333333"},
		},
		{"dupe collapses", "@reviewer @reviewer @reviewer", []string{"99ee88ff-3333-3333-3333-333333333333"}},
		{
			"ambiguous bare mention dropped",
			"@coder do X", // two 'coder' agents (different case) collide
			nil,
		},
		{
			"disambiguation via short id prefix",
			"@coder#1234abcd do X",
			[]string{"1234abcd-1111-1111-1111-111111111111"},
		},
		{
			"other short id prefix picks the other one",
			"@coder#5678efgh do Y",
			[]string{"5678efgh-2222-2222-2222-222222222222"},
		},
		{
			"short id with wrong name doesn't match",
			"@reviewer#1234abcd",
			nil,
		},
		{
			"email-like at is not a mention",
			"contact me@example.com",
			nil,
		},
		{
			"mid-word at is not a mention",
			"price is $100@5units",
			nil,
		},
		{
			"unknown name is not a mention",
			"@nobody",
			nil,
		},
		{
			"start of string",
			"@planner",
			[]string{"aabbccdd-4444-4444-4444-444444444444"},
		},
		{
			"start of line works",
			"\n@planner go",
			[]string{"aabbccdd-4444-4444-4444-444444444444"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse(tc.body, cands)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Parse(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}
