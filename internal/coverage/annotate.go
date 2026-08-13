package coverage

import (
	"cmp"
	"fmt"
	"html/template"
	"slices"
	"strings"

	"github.com/primandproper/tarpaulin/internal/analysis"

	"golang.org/x/tools/cover"
)

// tabWidth is how many spaces a tab is rendered as, matching `go tool cover`.
const tabWidth = 8

// verdict is the color one run of source text is painted. It is the whole point
// of this report: `go tool cover` has two states where tarp has four, because a
// statement that ran says nothing about whether anybody tested the function it
// belongs to.
type verdict uint8

const (
	// verdictUngraded is code that ran inside a declaration tarp does not hold
	// to the standard — init, main, generated, or explicitly ignored. Painting
	// it green would claim a test that was never asked for.
	verdictUngraded verdict = iota
	// verdictUncovered is code that never ran at all.
	verdictUncovered
	// verdictIndirect is code that ran, in a function no test names directly:
	// coverage bought on somebody else's assertion.
	verdictIndirect
	// verdictDirect is code that ran, in a function a TestXxx body references.
	verdictDirect
)

// class is the CSS class the verdict is rendered with.
func (v verdict) class() string {
	switch v {
	case verdictUncovered:
		return "tarp-uncovered"
	case verdictIndirect:
		return "tarp-indirect"
	case verdictDirect:
		return "tarp-direct"
	case verdictUngraded:
		return "tarp-ungraded"
	default:
		return "tarp-ungraded"
	}
}

// boundary marks where a coverage block opens or closes in the source text.
type boundary struct {
	offset int
	block  int
	start  bool
}

// annotate renders src as HTML, wrapping each coverage block in a span colored
// by the verdict on the function that block falls inside.
func annotate(src []byte, blocks []cover.ProfileBlock, functions []analysis.Function) template.HTML {
	var out strings.Builder

	bounds := boundaries(src, blocks)

	for i := range src {
		for len(bounds) > 0 && bounds[0].offset == i {
			writeBoundary(&out, bounds[0], blocks, functions)

			bounds = bounds[1:]
		}

		writeSourceByte(&out, src[i])
	}

	// Every closing boundary at the end of the file lands on the offset one past
	// the last byte, which the loop above never reaches.
	for _, remaining := range bounds {
		writeBoundary(&out, remaining, blocks, functions)
	}

	// The loop above is the escaper: every byte of src was either translated to
	// an entity or is inert, and the spans are ours. Nothing unescaped reaches
	// this conversion.
	return template.HTML(out.String()) //nolint:gosec // G203: the body is escaped by writeSourceByte, byte by byte.
}

// writeBoundary opens or closes the span for one coverage block.
func writeBoundary(out *strings.Builder, at boundary, blocks []cover.ProfileBlock, functions []analysis.Function) {
	if !at.start {
		out.WriteString("</span>")

		return
	}

	block := blocks[at.block]
	found := functionAt(functions, block.StartLine)

	fmt.Fprintf(out, `<span class="%s" title="%s">`,
		verdictFor(block, found).class(), template.HTMLEscapeString(describe(block, found)))
}

// writeSourceByte writes one byte of source, escaped for HTML.
func writeSourceByte(out *strings.Builder, b byte) {
	switch b {
	case '<':
		out.WriteString("&lt;")
	case '>':
		out.WriteString("&gt;")
	case '&':
		out.WriteString("&amp;")
	case '"':
		out.WriteString("&#34;")
	case '\'':
		out.WriteString("&#39;")
	case '\t':
		out.WriteString(strings.Repeat(" ", tabWidth))
	default:
		out.WriteByte(b)
	}
}

// boundaries locates each coverage block within src.
//
// This is golang.org/x/tools/cover.Boundaries' walk, keeping the block each
// boundary belongs to rather than a normalized count: the block carries the
// line number, and the line number is what attributes a run of source to a
// function. Blocks arrive sorted by start position and never overlap — they are
// basic blocks — so the boundaries come out in offset order and need no sort.
func boundaries(src []byte, blocks []cover.ProfileBlock) []boundary {
	found := make([]boundary, 0, len(blocks)*2)

	line, col := 1, 1
	offset, index := 0, 0
	open := false

	for offset <= len(src) && index < len(blocks) {
		block := blocks[index]

		if !open && block.StartLine == line && block.StartCol == col {
			found = append(found, boundary{offset: offset, block: index, start: true})
			open = true
		}

		if (block.EndLine == line && block.EndCol == col) || line > block.EndLine {
			// A block whose start never matched opens nothing to close. That
			// only happens against a profile the source has moved on from, and
			// an unbalanced span would corrupt the whole page rather than the
			// one block it came from.
			if open {
				found = append(found, boundary{offset: offset, block: index})
				open = false
			}

			index++

			// Do not advance through src: the next block may start right here.
			continue
		}

		if offset == len(src) {
			break
		}

		if src[offset] == '\n' {
			line++
			col = 0
		}

		col++
		offset++
	}

	if open {
		found = append(found, boundary{offset: len(src), block: index})
	}

	return found
}

// functionAt returns the function whose declaration contains line, or nil when
// the line belongs to no function the report graded.
func functionAt(functions []analysis.Function, line int) *analysis.Function {
	index, exact := slices.BinarySearchFunc(functions, line, func(fn analysis.Function, target int) int {
		return cmp.Compare(fn.Line, target)
	})

	if !exact {
		index--
	}

	if index < 0 || index >= len(functions) || functions[index].EndLine < line {
		return nil
	}

	return &functions[index]
}

// verdictFor decides what color a block is painted.
func verdictFor(block cover.ProfileBlock, found *analysis.Function) verdict {
	switch {
	case block.Count == 0:
		return verdictUncovered
	case found == nil:
		return verdictUngraded
	case found.Tested:
		return verdictDirect
	default:
		return verdictIndirect
	}
}

// describe is the hover text on a block: what the profile measured, and what
// tarp concluded about the function it belongs to.
func describe(block cover.ProfileBlock, found *analysis.Function) string {
	var ran string

	switch block.Count {
	case 0:
		ran = "never ran"
	case 1:
		ran = "ran once"
	default:
		ran = fmt.Sprintf("ran %d times", block.Count)
	}

	if found == nil {
		return ran + "; not graded"
	}

	if found.Tested {
		return fmt.Sprintf("%s; %s is directly tested", ran, found.Name)
	}

	return fmt.Sprintf("%s; %s has no direct test", ran, found.Name)
}
