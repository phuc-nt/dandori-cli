package intent

import (
	"strings"
	"testing"
)

// BenchmarkExtractDecisions measures the performance of decision pattern
// extraction on realistic reasoning block arrays. Tests all 5 decision patterns
// and the follow-up confirmation heuristic.
//
// Baseline expectation (pre-optimization):
//   - 10 blocks @ 1KB avg: <1ms, <10 allocs
//   - 100 blocks @ 1KB avg: <10ms, <100 allocs
//
// Post quick-win #2 (followUpRe package-level):
//   - Regex should not recompile per call — CPU profile will confirm.
func BenchmarkExtractDecisions(b *testing.B) {
	// Build a realistic array of reasoning blocks that exercise all 5 patterns
	blocks := []ReasoningBlock{
		{Source: "thinking", Text: "I'll go with bcrypt because it has a tunable cost factor."},
		{Source: "thinking", Text: "using Redis over memcached for persistence"},
		{Source: "thinking", Text: "better to use async/await than promise chains"},
		{Source: "thinking", Text: "decided to implement batch operations."},
		{Source: "thinking", Text: "could either use webhook or polling."},
		{Source: "thinking", Text: "we decided to use polling for simplicity."},
		{Source: "thinking", Text: "no clear pattern here, just reasoning"},
		{Source: "thinking", Text: "I'll go with PostgreSQL because it's ACID-compliant and has jsonb support."},
		{Source: "thinking", Text: "using Docker over VMs for consistency across environments"},
		{Source: "thinking", Text: "could either cache locally or via Redis."},
		{Source: "thinking", Text: "chose Redis for the caching layer."},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = ExtractDecisions(blocks)
	}
}

// BenchmarkExtractDecisionsLarge scales the test to 100 blocks to simulate
// longer reasoning sessions. Useful for detecting allocation bloat or O(n²) patterns.
func BenchmarkExtractDecisionsLarge(b *testing.B) {
	// Build 100 blocks, cycling through the patterns
	var blocks []ReasoningBlock
	patterns := []string{
		"I'll go with bcrypt because it has a tunable cost factor.",
		"using Redis over memcached for persistence",
		"better to use async/await than promise chains",
		"decided to implement batch operations.",
		"could either use webhook or polling.",
		"no clear pattern here, just reasoning",
	}

	for i := 0; i < 100; i++ {
		blocks = append(blocks, ReasoningBlock{
			Source: "thinking",
			Text:   patterns[i%len(patterns)],
		})
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = ExtractDecisions(blocks)
	}
}

// BenchmarkFollowUpRegex isolates the followUpRe regex performance.
// This directly tests whether the package-level regex (fix #2) is cached.
//
// If regex is recompiled per call: ~1000+ ns/op
// If regex is cached at package-level: ~100–200 ns/op
func BenchmarkFollowUpRegex(b *testing.B) {
	// Create realistic follow-up phrases
	phrases := []string{
		"we decided to use polling for simplicity.",
		"I'll go with webhook then.",
		"chose Redis for caching.",
		"using async/await instead.",
		"no clear decision here",
		"decided to implement cache invalidation.",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, phrase := range phrases {
			_ = followUpRe.FindStringSubmatch(phrase)
		}
	}
}

// BenchmarkRejectCandidates measures the helper function that builds rejected
// candidate lists for the "could either X or Y" decision pattern.
func BenchmarkRejectCandidates(b *testing.B) {
	candidates := []struct{ chosen, a, b string }{
		{"Redis", "Redis", "memcached"},
		{"async/await", "async/await", "promise chains"},
		{"bcrypt", "bcrypt", "argon2"},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, c := range candidates {
			_ = rejectCandidates(c.chosen, c.a, c.b)
		}
	}
}

// BenchmarkCapField measures field capping and truncation, which affects
// every decision extraction as the final step before returning.
func BenchmarkCapField(b *testing.B) {
	// Realistic field values: chosen option names, rationale text
	inputs := []string{
		"bcrypt with cost 12",
		strings.Repeat("a", maxDecisionFieldBytes+100), // intentionally over-cap
		"Redis because it's fast",
		"  spaces  everywhere  ",
		"",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, input := range inputs {
			_ = capField(input)
		}
	}
}
