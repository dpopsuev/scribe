package service

import (
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

// AnchorResult holds the resolved content and location from an anchor lookup.
type AnchorResult struct {
	Found   bool   // whether the anchor resolved to content
	Section string // section that matched
	Line    int    // 1-based line within the section (0 if section-level match)
	Text    string // the resolved text (line, section, or anchor block)
}

// ResolveAnchor resolves a Selector against an artifact's sections and returns
// the matched content. Resolution priority:
//  1. Anchor — scan all sections for a markdown heading matching the anchor
//  2. Section + Line — return the specific line within the named section
//  3. Section only — return the entire section text
//  4. Line only — return that line from the first section
func ResolveAnchor(art *parchment.Artifact, sel Selector) AnchorResult {
	if sel.IsZero() {
		return AnchorResult{}
	}

	if sel.Anchor != "" {
		return resolveByAnchor(art, sel.Anchor)
	}

	if sel.Section != "" {
		return resolveBySection(art, sel.Section, sel.Line)
	}

	if sel.Line > 0 && len(art.Sections) > 0 {
		return resolveByLine(art.Sections[0], sel.Line)
	}

	return AnchorResult{}
}

func resolveByAnchor(art *parchment.Artifact, anchor string) AnchorResult {
	slug := strings.ToLower(anchor)
	for _, sec := range art.Sections {
		lines := strings.Split(sec.Text, "\n")
		for i, line := range lines {
			if isHeadingMatch(line, slug) {
				block := extractHeadingBlock(lines, i)
				return AnchorResult{
					Found:   true,
					Section: sec.Name,
					Line:    i + 1,
					Text:    block,
				}
			}
		}
	}
	return AnchorResult{}
}

func resolveBySection(art *parchment.Artifact, name string, line int) AnchorResult {
	for _, sec := range art.Sections {
		if sec.Name != name {
			continue
		}
		if line > 0 {
			return resolveByLine(sec, line)
		}
		return AnchorResult{Found: true, Section: sec.Name, Text: sec.Text}
	}
	return AnchorResult{}
}

func resolveByLine(sec parchment.Section, line int) AnchorResult {
	lines := strings.Split(sec.Text, "\n")
	if line < 1 || line > len(lines) {
		return AnchorResult{}
	}
	return AnchorResult{
		Found:   true,
		Section: sec.Name,
		Line:    line,
		Text:    lines[line-1],
	}
}

// isHeadingMatch checks if a markdown line is a heading that matches the slug.
// Supports both ATX headings (# Heading) and generates a slug by lowercasing,
// stripping non-alphanumeric chars, and replacing spaces with hyphens.
func isHeadingMatch(line, slug string) bool {
	trimmed := strings.TrimLeft(line, "#")
	if len(trimmed) == len(line) {
		return false
	}
	heading := strings.TrimSpace(trimmed)
	return headingSlug(heading) == slug
}

func headingSlug(heading string) string {
	heading = strings.ToLower(heading)
	var b strings.Builder
	for _, r := range heading {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// extractHeadingBlock extracts text from a heading line until the next
// same-or-higher-level heading or end of text.
func extractHeadingBlock(lines []string, headingIdx int) string {
	level := headingLevel(lines[headingIdx])
	end := len(lines)
	for i := headingIdx + 1; i < len(lines); i++ {
		if l := headingLevel(lines[i]); l > 0 && l <= level {
			end = i
			break
		}
	}
	return strings.Join(lines[headingIdx:end], "\n")
}

func headingLevel(line string) int {
	level := 0
	for _, r := range line {
		if r == '#' {
			level++
		} else {
			break
		}
	}
	if level > 0 && level < len(line) && line[level] == ' ' {
		return level
	}
	return 0
}
