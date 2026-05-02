package confluence

import (
	"strings"
	"testing"
)

// benchmarkStorageHTML generates realistic Confluence storage format HTML
// that exercises all 7 regex patterns promoted to package-level (fix #1).
func benchmarkStorageHTML() string {
	// Build a complex document with multiple elements
	html := `
<h1>Project Report</h1>
<p>This is the main content area.</p>
<h2>Section 1</h2>
<p>Some <strong>bold text</strong> and <em>italic text</em> here.</p>
<table>
  <tr>
    <th>Column 1</th>
    <th>Column 2</th>
  </tr>
  <tr>
    <td>Data 1</td>
    <td>Data 2</td>
  </tr>
  <tr>
    <td>Data 3</td>
    <td>Data 4</td>
  </tr>
</table>
<h3>Subsection</h3>
<ul>
  <li>Item 1</li>
  <li>Item 2</li>
  <li>Item 3</li>
</ul>
<p>Check out <a href="https://example.com">this link</a> for more info.</p>
<ac:structured-macro ac:name="code">
<ac:parameter ac:name="language">go</ac:parameter>
<ac:plain-text-body><![CDATA[
func Hello() {
  fmt.Println("Hello, World!")
}
]]></ac:plain-text-body>
</ac:structured-macro>
<h4>Details</h4>
<p>Final paragraph with more content.</p>
`
	return html
}

// BenchmarkStorageToMarkdown measures the full conversion pipeline from
// Confluence storage format to Markdown. Post quick-win #1, all 7 regex
// patterns should be cached at package-level, avoiding recompilation.
//
// Expected baseline:
//   - Without caching: ~50–100 µs per call (regex recompilation overhead)
//   - With package-level cache: ~20–30 µs per call (regex reuse)
func BenchmarkStorageToMarkdown(b *testing.B) {
	html := benchmarkStorageHTML()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = StorageToMarkdown(html)
	}
}

// BenchmarkStorageToMarkdownLarge tests conversion of a larger document
// to catch allocation bloat or quadratic behavior.
func BenchmarkStorageToMarkdownLarge(b *testing.B) {
	base := benchmarkStorageHTML()

	// Repeat the base pattern 10 times to create a larger document
	var html strings.Builder
	for i := 0; i < 10; i++ {
		html.WriteString(base)
	}

	htmlStr := html.String()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = StorageToMarkdown(htmlStr)
	}
}

// BenchmarkConvertTables isolates the table conversion function, which
// itself uses 4 regex patterns (reTablePattern, reRowPattern, reHeaderPattern,
// reDataPattern). Ensures table processing scales well.
func BenchmarkConvertTables(b *testing.B) {
	html := `<table><tr><th>H1</th><th>H2</th><th>H3</th></tr><tr><td>D1</td><td>D2</td><td>D3</td></tr></table>`

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = convertTables(html)
	}
}

// BenchmarkConvertTablesLarge tests a more complex table (50 rows × 10 cols)
// to ensure no O(n²) behavior or excessive allocations.
func BenchmarkConvertTablesLarge(b *testing.B) {
	// Build a 50-row × 10-col table
	var html strings.Builder
	html.WriteString("<table>")

	// Header
	html.WriteString("<tr>")
	for col := 0; col < 10; col++ {
		html.WriteString(`<th>Col` + string(rune('A'+col)) + `</th>`)
	}
	html.WriteString("</tr>")

	// 50 rows of data
	for row := 0; row < 50; row++ {
		html.WriteString("<tr>")
		for col := 0; col < 10; col++ {
			html.WriteString(`<td>R` + string(rune('A'+col)) + string(rune('0'+row%10)) + `</td>`)
		}
		html.WriteString("</tr>")
	}

	html.WriteString("</table>")

	htmlStr := html.String()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = convertTables(htmlStr)
	}
}

// BenchmarkConvertLists isolates list conversion, which uses 3 regex patterns
// (ulRe, olRe, liRe).
func BenchmarkConvertLists(b *testing.B) {
	html := `<ul><li>Item 1</li><li>Item 2</li><li>Item 3</li></ul><ol><li>First</li><li>Second</li></ol>`

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = convertLists(html)
	}
}

// BenchmarkMarkdownToStorage tests the reverse direction: Markdown to Storage.
// This path uses reBold and reItalic regex patterns (also promoted to package-level).
func BenchmarkMarkdownToStorage(b *testing.B) {
	md := `# Title
This is **bold** and this is *italic*.

## Section
- Item 1
- Item 2

Regular paragraph with [link](https://example.com).

` + "```go\n" + `func main() {}
` + "```" + `
`

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = MarkdownToStorage(md)
	}
}

// BenchmarkRoundTrip tests StorageToMarkdown → MarkdownToStorage fidelity.
// Useful for detecting regex pattern mismatches or idempotency issues.
func BenchmarkRoundTrip(b *testing.B) {
	originalHTML := benchmarkStorageHTML()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		md := StorageToMarkdown(originalHTML)
		_ = MarkdownToStorage(md)
	}
}
