package confluence

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

var (
	h1Re       = regexp.MustCompile(`<h1[^>]*>(.*?)</h1>`)
	h2Re       = regexp.MustCompile(`<h2[^>]*>(.*?)</h2>`)
	h3Re       = regexp.MustCompile(`<h3[^>]*>(.*?)</h3>`)
	h4Re       = regexp.MustCompile(`<h4[^>]*>(.*?)</h4>`)
	pRe        = regexp.MustCompile(`<p[^>]*>(.*?)</p>`)
	strongRe   = regexp.MustCompile(`<strong[^>]*>(.*?)</strong>`)
	emRe       = regexp.MustCompile(`<em[^>]*>(.*?)</em>`)
	linkRe     = regexp.MustCompile(`<a[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
	ulRe       = regexp.MustCompile(`<ul[^>]*>(.*?)</ul>`)
	olRe       = regexp.MustCompile(`<ol[^>]*>(.*?)</ol>`)
	liRe       = regexp.MustCompile(`<li[^>]*>(.*?)</li>`)
	tableRe    = regexp.MustCompile(`<table[^>]*>(.*?)</table>`)
	trRe       = regexp.MustCompile(`<tr[^>]*>(.*?)</tr>`)
	thRe       = regexp.MustCompile(`<th[^>]*>(.*?)</th>`)
	tdRe       = regexp.MustCompile(`<td[^>]*>(.*?)</td>`)
	codeRe     = regexp.MustCompile(`(?s)<ac:structured-macro[^>]*ac:name="code"[^>]*>.*?<ac:parameter[^>]*ac:name="language"[^>]*>([^<]*)</ac:parameter>.*?<ac:plain-text-body><!\[CDATA\[(.*?)\]\]></ac:plain-text-body>.*?</ac:structured-macro>`)
	codeSimple = regexp.MustCompile(`(?s)<ac:structured-macro[^>]*ac:name="code"[^>]*>.*?<ac:plain-text-body><!\[CDATA\[(.*?)\]\]></ac:plain-text-body>.*?</ac:structured-macro>`)
	stripTags  = regexp.MustCompile(`<[^>]*>`)

	// Promoted from function bodies to avoid per-call recompilation.
	reMultiNewline  = regexp.MustCompile(`\n{3,}`)
	reTablePattern  = regexp.MustCompile(`(?s)<table[^>]*>(.*?)</table>`)
	reRowPattern    = regexp.MustCompile(`(?s)<tr[^>]*>(.*?)</tr>`)
	reHeaderPattern = regexp.MustCompile(`(?s)<th[^>]*>(.*?)</th>`)
	reDataPattern   = regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`)
	reBold          = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	reItalic        = regexp.MustCompile(`\*([^*]+)\*`)
)

func StorageToMarkdown(storage string) string {
	if storage == "" {
		return ""
	}

	md := storage

	// Code blocks first (before stripping tags)
	md = codeRe.ReplaceAllString(md, "\n```$1\n$2\n```\n")
	md = codeSimple.ReplaceAllString(md, "\n```\n$1\n```\n")

	// Headings
	md = h1Re.ReplaceAllString(md, "\n# $1\n")
	md = h2Re.ReplaceAllString(md, "\n## $1\n")
	md = h3Re.ReplaceAllString(md, "\n### $1\n")
	md = h4Re.ReplaceAllString(md, "\n#### $1\n")

	// Bold and italic (before paragraphs)
	md = strongRe.ReplaceAllString(md, "**$1**")
	md = emRe.ReplaceAllString(md, "*$1*")

	// Links
	md = linkRe.ReplaceAllString(md, "[$2]($1)")

	// Tables
	md = convertTables(md)

	// Lists
	md = convertLists(md)

	// Paragraphs
	md = pRe.ReplaceAllString(md, "\n$1\n")

	// Strip remaining HTML
	md = stripTags.ReplaceAllString(md, "")

	// Decode HTML entities
	md = html.UnescapeString(md)

	// Clean up whitespace
	md = reMultiNewline.ReplaceAllString(md, "\n\n")
	md = strings.TrimSpace(md)

	return md
}

func convertLists(s string) string {
	// Unordered lists
	s = ulRe.ReplaceAllStringFunc(s, func(ul string) string {
		items := liRe.FindAllStringSubmatch(ul, -1)
		var lines []string
		for _, item := range items {
			if len(item) > 1 {
				content := stripTags.ReplaceAllString(item[1], "")
				lines = append(lines, "- "+strings.TrimSpace(content))
			}
		}
		return "\n" + strings.Join(lines, "\n") + "\n"
	})

	// Ordered lists
	s = olRe.ReplaceAllStringFunc(s, func(ol string) string {
		items := liRe.FindAllStringSubmatch(ol, -1)
		var lines []string
		for i, item := range items {
			if len(item) > 1 {
				content := stripTags.ReplaceAllString(item[1], "")
				lines = append(lines, strconv.Itoa(i+1)+". "+strings.TrimSpace(content))
			}
		}
		return "\n" + strings.Join(lines, "\n") + "\n"
	})

	return s
}

func convertTables(s string) string {
	return reTablePattern.ReplaceAllStringFunc(s, func(table string) string {
		rows := reRowPattern.FindAllStringSubmatch(table, -1)
		if len(rows) == 0 {
			return ""
		}

		var mdRows []string
		for i, row := range rows {
			if len(row) < 2 {
				continue
			}

			// Get cells (th or td)
			var cells []string
			headers := reHeaderPattern.FindAllStringSubmatch(row[1], -1)
			if len(headers) > 0 {
				for _, h := range headers {
					if len(h) > 1 {
						cells = append(cells, strings.TrimSpace(stripTags.ReplaceAllString(h[1], "")))
					}
				}
			} else {
				data := reDataPattern.FindAllStringSubmatch(row[1], -1)
				for _, d := range data {
					if len(d) > 1 {
						cells = append(cells, strings.TrimSpace(stripTags.ReplaceAllString(d[1], "")))
					}
				}
			}

			if len(cells) > 0 {
				mdRows = append(mdRows, "| "+strings.Join(cells, " | ")+" |")
				// Add separator after header
				if i == 0 {
					sep := make([]string, len(cells))
					for j := range sep {
						sep[j] = "---"
					}
					mdRows = append(mdRows, "| "+strings.Join(sep, " | ")+" |")
				}
			}
		}

		return "\n" + strings.Join(mdRows, "\n") + "\n"
	})
}

func MarkdownToStorage(md string) string {
	if md == "" {
		return ""
	}

	lines := strings.Split(md, "\n")
	var storage strings.Builder
	inCodeBlock := false
	codeLanguage := ""
	var codeContent strings.Builder
	inList := false
	var listItems []string

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		// Code blocks
		if strings.HasPrefix(line, "```") {
			if inCodeBlock {
				// End code block
				storage.WriteString(`<ac:structured-macro ac:name="code">`)
				if codeLanguage != "" {
					storage.WriteString(`<ac:parameter ac:name="language">` + codeLanguage + `</ac:parameter>`)
				}
				storage.WriteString(`<ac:plain-text-body><![CDATA[`)
				storage.WriteString(strings.TrimSpace(codeContent.String()))
				storage.WriteString(`]]></ac:plain-text-body></ac:structured-macro>`)
				inCodeBlock = false
				codeContent.Reset()
				codeLanguage = ""
			} else {
				// Start code block
				flushList(&storage, &listItems, &inList)
				inCodeBlock = true
				codeLanguage = strings.TrimPrefix(line, "```")
			}
			continue
		}

		if inCodeBlock {
			codeContent.WriteString(line + "\n")
			continue
		}

		// Headings
		if strings.HasPrefix(line, "# ") {
			flushList(&storage, &listItems, &inList)
			storage.WriteString("<h1>" + html.EscapeString(strings.TrimPrefix(line, "# ")) + "</h1>")
			continue
		}
		if strings.HasPrefix(line, "## ") {
			flushList(&storage, &listItems, &inList)
			storage.WriteString("<h2>" + html.EscapeString(strings.TrimPrefix(line, "## ")) + "</h2>")
			continue
		}
		if strings.HasPrefix(line, "### ") {
			flushList(&storage, &listItems, &inList)
			storage.WriteString("<h3>" + html.EscapeString(strings.TrimPrefix(line, "### ")) + "</h3>")
			continue
		}

		// List items
		if strings.HasPrefix(line, "- ") {
			inList = true
			listItems = append(listItems, strings.TrimPrefix(line, "- "))
			continue
		}

		// Empty line ends list
		if line == "" && inList {
			flushList(&storage, &listItems, &inList)
			continue
		}

		// Regular paragraph
		if strings.TrimSpace(line) != "" {
			flushList(&storage, &listItems, &inList)
			content := line

			// Bold and italic
			content = reBold.ReplaceAllString(content, "<strong>$1</strong>")
			content = reItalic.ReplaceAllString(content, "<em>$1</em>")

			storage.WriteString("<p>" + content + "</p>")
		}
	}

	flushList(&storage, &listItems, &inList)
	return storage.String()
}

func flushList(storage *strings.Builder, items *[]string, inList *bool) {
	if *inList && len(*items) > 0 {
		storage.WriteString("<ul>")
		for _, item := range *items {
			storage.WriteString("<li>" + html.EscapeString(item) + "</li>")
		}
		storage.WriteString("</ul>")
		*items = nil
		*inList = false
	}
}
