package evaluation

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	goldmark "github.com/yuin/goldmark"
	markdownast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extensionast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	"golang.org/x/net/html"
	"golang.org/x/text/unicode/norm"
)

const ParserMetricContractVersion = 1

type ParserHeadingV1 struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
}

type ParserListItemV1 struct {
	Depth   int    `json:"depth"`
	Ordered bool   `json:"ordered"`
	Text    string `json:"text"`
}

type ParserTableCellV1 struct {
	Table  int    `json:"table"`
	Row    int    `json:"row"`
	Column int    `json:"column"`
	Text   string `json:"text"`
}

type ParserAnchorMatchV1 struct {
	Text       string `json:"text"`
	Count      int    `json:"count"`
	RuneOffset int    `json:"rune_offset"`
}

type ParserComponentsV1 struct {
	Text          string                `json:"text"`
	Headings      []ParserHeadingV1     `json:"headings"`
	ListItems     []ParserListItemV1    `json:"list_items"`
	TableCells    []ParserTableCellV1   `json:"table_cells"`
	Formulas      []string              `json:"formulas"`
	AnchorMatches []ParserAnchorMatchV1 `json:"anchor_matches"`
	ImageCount    int                   `json:"image_count"`
	Diagnostics   []string              `json:"diagnostics,omitempty"`
}

type ParserMetricValueV1 struct {
	Applicable bool     `json:"applicable"`
	Value      *float64 `json:"value"`
}

type ParserMetricSetV1 struct {
	TextSimilarity       ParserMetricValueV1 `json:"text_similarity"`
	StructureF1          ParserMetricValueV1 `json:"structure_f1"`
	TableCellF1          ParserMetricValueV1 `json:"table_cell_f1"`
	FormulaSimilarity    ParserMetricValueV1 `json:"formula_similarity"`
	ReadingOrderAccuracy ParserMetricValueV1 `json:"reading_order_accuracy"`
	SpuriousTableCount   int                 `json:"spurious_table_count"`
	SpuriousFormulaCount int                 `json:"spurious_formula_count"`
}

// ExtractParserComponents is the sole parser-benchmark normalization and
// component extraction seam. Tables and formulas are intentionally excluded
// from Text so that they cannot be scored twice.
func ExtractParserComponents(markdown string, anchors []string) (ParserComponentsV1, error) {
	if !utf8.ValidString(markdown) {
		return ParserComponentsV1{}, fmt.Errorf("parser_invalid_utf8")
	}
	normalized := normalizeParserSource(markdown)
	masked, formulas := scanAndMaskFormulas(normalized)
	source := []byte(masked)
	document := goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser().Parse(text.NewReader(source))

	result := ParserComponentsV1{
		Headings: make([]ParserHeadingV1, 0), ListItems: make([]ParserListItemV1, 0),
		TableCells: make([]ParserTableCellV1, 0), Formulas: formulas,
		AnchorMatches: make([]ParserAnchorMatchV1, 0, len(anchors)), Diagnostics: make([]string, 0),
	}
	tableNumber := 0
	visible := make([]string, 0)
	err := markdownast.Walk(document, func(node markdownast.Node, entering bool) (markdownast.WalkStatus, error) {
		if !entering {
			return markdownast.WalkContinue, nil
		}
		switch typed := node.(type) {
		case *extensionast.Table:
			appendGFMTable(&result, typed, source, tableNumber)
			tableNumber++
			return markdownast.WalkSkipChildren, nil
		case *markdownast.Heading:
			headingText := inlineNodeText(typed, source, nil)
			result.Headings = append(result.Headings, ParserHeadingV1{Level: typed.Level, Text: headingText})
			appendVisible(&visible, headingText)
			return markdownast.WalkSkipChildren, nil
		case *markdownast.ListItem:
			list, _ := typed.Parent().(*markdownast.List)
			itemText := inlineNodeText(typed, source, func(n markdownast.Node) bool {
				_, nested := n.(*markdownast.List)
				return nested && n != typed.Parent()
			})
			result.ListItems = append(result.ListItems, ParserListItemV1{
				Depth: listDepth(typed), Ordered: list != nil && list.IsOrdered(), Text: itemText,
			})
			appendVisible(&visible, itemText)
		case *markdownast.Image:
			result.ImageCount++
			return markdownast.WalkSkipChildren, nil
		case *markdownast.Paragraph:
			if !hasAncestorKind(typed, extensionast.KindTable) {
				appendVisible(&visible, inlineNodeText(typed, source, nil))
			}
		case *markdownast.FencedCodeBlock:
			appendBlockLines(&visible, typed.Lines(), source)
			return markdownast.WalkSkipChildren, nil
		case *markdownast.CodeBlock:
			appendBlockLines(&visible, typed.Lines(), source)
			return markdownast.WalkSkipChildren, nil
		}
		return markdownast.WalkContinue, nil
	})
	if err != nil {
		return ParserComponentsV1{}, fmt.Errorf("walk markdown: %w", err)
	}

	htmlTables, diagnostics := extractHTMLTables(normalized, tableNumber)
	result.TableCells = append(result.TableCells, htmlTables...)
	result.Diagnostics = append(result.Diagnostics, diagnostics...)
	result.Text = strings.Join(visible, "\n")
	for _, anchor := range anchors {
		normalizedAnchor := normalizeParserText(anchor)
		count := strings.Count(result.Text, normalizedAnchor)
		offset := -1
		if count > 0 {
			byteOffset := strings.Index(result.Text, normalizedAnchor)
			offset = utf8.RuneCountInString(result.Text[:byteOffset])
		}
		result.AnchorMatches = append(result.AnchorMatches, ParserAnchorMatchV1{Text: normalizedAnchor, Count: count, RuneOffset: offset})
	}
	return result, nil
}

func ScoreParserComponents(groundTruth, prediction ParserComponentsV1) ParserMetricSetV1 {
	result := ParserMetricSetV1{
		TextSimilarity:       similarityMetric(groundTruth.Text, prediction.Text),
		StructureF1:          multisetF1(structureKeys(groundTruth), structureKeys(prediction), len(groundTruth.Headings)+len(groundTruth.ListItems) > 0),
		TableCellF1:          multisetF1(tableKeys(groundTruth.TableCells), tableKeys(prediction.TableCells), len(groundTruth.TableCells) > 0),
		FormulaSimilarity:    formulaMetric(groundTruth.Formulas, prediction.Formulas),
		ReadingOrderAccuracy: readingOrderMetric(groundTruth.AnchorMatches, prediction.AnchorMatches),
	}
	if len(groundTruth.TableCells) == 0 && len(prediction.TableCells) > 0 {
		result.SpuriousTableCount = len(uniqueTableNumbers(prediction.TableCells))
	}
	if len(groundTruth.Formulas) == 0 && len(prediction.Formulas) > 0 {
		result.SpuriousFormulaCount = len(prediction.Formulas)
	}
	return result
}

func normalizeParserSource(value string) string {
	value = strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	value = norm.NFKC.String(value)
	var out strings.Builder
	for _, r := range value {
		if unicode.Is(unicode.Cf, r) {
			continue
		}
		if r != '\n' && unicode.IsSpace(r) {
			r = ' '
		}
		out.WriteRune(r)
	}
	return out.String()
}

func normalizeParserText(value string) string {
	value = normalizeParserSource(value)
	return strings.Join(strings.Fields(value), " ")
}

func scanAndMaskFormulas(source string) (string, []string) {
	runes := []rune(source)
	masked := append([]rune(nil), runes...)
	formulas := make([]string, 0, 1)
	for i := 0; i < len(runes); {
		open, close, contentStart := "", "", i
		switch {
		case runes[i] == '$' && i+1 < len(runes) && runes[i+1] == '$' && !escapedRune(runes, i):
			open, close, contentStart = "$$", "$$", i+2
		case runes[i] == '\\' && i+1 < len(runes) && runes[i+1] == '[':
			open, close, contentStart = "\\[", "\\]", i+2
		case runes[i] == '\\' && i+1 < len(runes) && runes[i+1] == '(':
			open, close, contentStart = "\\(", "\\)", i+2
		case runes[i] == '$' && !escapedRune(runes, i):
			open, close, contentStart = "$", "$", i+1
		default:
			i++
			continue
		}
		end := findFormulaClose(runes, contentStart, close)
		if end < 0 {
			i++
			continue
		}
		content := string(runes[contentStart:end])
		if open == "$" && !inlineMathContent(content) {
			i++
			continue
		}
		formula := normalizeFormula(content)
		if formula == "" {
			i++
			continue
		}
		closeLen := len([]rune(close))
		for j := i; j < end+closeLen; j++ {
			if masked[j] != '\n' {
				masked[j] = ' '
			}
		}
		formulas = append(formulas, formula)
		i = end + closeLen
	}
	return string(masked), formulas
}

func findFormulaClose(runes []rune, start int, close string) int {
	closer := []rune(close)
	for i := start; i+len(closer) <= len(runes); i++ {
		if len(closer) == 1 && runes[i] == '\n' {
			return -1
		}
		if string(runes[i:i+len(closer)]) == close && !escapedRune(runes, i) {
			return i
		}
	}
	return -1
}

func escapedRune(runes []rune, index int) bool {
	backslashes := 0
	for index--; index >= 0 && runes[index] == '\\'; index-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func inlineMathContent(value string) bool {
	return strings.ContainsAny(value, "\\^_=+*/<>×÷±−∑√")
}

func normalizeFormula(value string) string {
	value = norm.NFKC.String(value)
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, value)
}

func inlineNodeText(root markdownast.Node, source []byte, skip func(markdownast.Node) bool) string {
	parts := make([]string, 0)
	_ = markdownast.Walk(root, func(node markdownast.Node, entering bool) (markdownast.WalkStatus, error) {
		if !entering {
			return markdownast.WalkContinue, nil
		}
		if node != root && skip != nil && skip(node) {
			return markdownast.WalkSkipChildren, nil
		}
		switch typed := node.(type) {
		case *markdownast.Image:
			return markdownast.WalkSkipChildren, nil
		case *markdownast.Text:
			parts = append(parts, string(typed.Segment.Value(source)))
		case *markdownast.CodeSpan:
			parts = append(parts, string(typed.Text(source)))
			return markdownast.WalkSkipChildren, nil
		}
		return markdownast.WalkContinue, nil
	})
	return normalizeParserText(strings.Join(parts, " "))
}

func listDepth(item markdownast.Node) int {
	depth := 0
	for parent := item.Parent(); parent != nil; parent = parent.Parent() {
		if _, ok := parent.(*markdownast.List); ok {
			depth++
		}
	}
	return depth
}

func appendGFMTable(result *ParserComponentsV1, table *extensionast.Table, source []byte, number int) {
	rowIndex := 0
	for row := table.FirstChild(); row != nil; row = row.NextSibling() {
		columnIndex := 0
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			result.TableCells = append(result.TableCells, ParserTableCellV1{
				Table: number, Row: rowIndex, Column: columnIndex, Text: inlineNodeText(cell, source, nil),
			})
			columnIndex++
		}
		rowIndex++
	}
}

func extractHTMLTables(source string, startNumber int) ([]ParserTableCellV1, []string) {
	tokenizer := html.NewTokenizer(strings.NewReader(source))
	cells := make([]ParserTableCellV1, 0)
	diagnostics := make([]string, 0)
	inTable, inCell, invalid := false, false, false
	tableNumber, row, column := startNumber-1, -1, 0
	cellText := strings.Builder{}
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			break
		}
		token := tokenizer.Token()
		name := strings.ToLower(token.Data)
		switch tokenType {
		case html.StartTagToken:
			switch name {
			case "table":
				inTable, invalid = true, false
				tableNumber++
			case "tr":
				if inTable {
					row++
					column = 0
				}
			case "td", "th":
				if inTable {
					inCell = true
					cellText.Reset()
					for _, attribute := range token.Attr {
						if (attribute.Key == "rowspan" || attribute.Key == "colspan") && attribute.Val != "" && attribute.Val != "1" {
							invalid = true
						}
					}
				}
			}
		case html.TextToken:
			if inTable && inCell {
				cellText.WriteString(token.Data)
			}
		case html.EndTagToken:
			switch name {
			case "td", "th":
				if inTable && inCell {
					cells = append(cells, ParserTableCellV1{Table: tableNumber, Row: row, Column: column, Text: normalizeParserText(cellText.String())})
					column++
					inCell = false
				}
			case "table":
				if inTable && invalid {
					filtered := cells[:0]
					for _, cell := range cells {
						if cell.Table != tableNumber {
							filtered = append(filtered, cell)
						}
					}
					cells = filtered
					diagnostics = append(diagnostics, "html_table_span_unsupported")
				}
				inTable, inCell = false, false
			}
		}
	}
	return cells, diagnostics
}

func appendVisible(parts *[]string, value string) {
	if normalized := normalizeParserText(value); normalized != "" {
		*parts = append(*parts, normalized)
	}
}

func appendBlockLines(parts *[]string, lines *text.Segments, source []byte) {
	for i := 0; i < lines.Len(); i++ {
		segment := lines.At(i)
		appendVisible(parts, string(segment.Value(source)))
	}
}

func hasAncestorKind(node markdownast.Node, kind markdownast.NodeKind) bool {
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		if parent.Kind() == kind {
			return true
		}
	}
	return false
}

func similarityMetric(left, right string) ParserMetricValueV1 {
	leftRunes, rightRunes := []rune(left), []rune(right)
	if len(leftRunes) == 0 && len(rightRunes) == 0 {
		return ParserMetricValueV1{}
	}
	maximum := len(leftRunes)
	if len(rightRunes) > maximum {
		maximum = len(rightRunes)
	}
	value := 1 - float64(levenshteinRunes(leftRunes, rightRunes))/float64(maximum)
	return applicableMetric(value)
}

func levenshteinRunes(left, right []rune) int {
	if len(left) > len(right) {
		left, right = right, left
	}
	previous := make([]int, len(left)+1)
	for i := range previous {
		previous[i] = i
	}
	for row, rightRune := range right {
		current := make([]int, len(left)+1)
		current[0] = row + 1
		for column, leftRune := range left {
			cost := 0
			if leftRune != rightRune {
				cost = 1
			}
			current[column+1] = min(previous[column+1]+1, current[column]+1, previous[column]+cost)
		}
		previous = current
	}
	return previous[len(left)]
}

func structureKeys(components ParserComponentsV1) []string {
	keys := make([]string, 0, len(components.Headings)+len(components.ListItems))
	for _, heading := range components.Headings {
		keys = append(keys, fmt.Sprintf("heading\x00%d\x00%s", heading.Level, heading.Text))
	}
	for _, item := range components.ListItems {
		keys = append(keys, fmt.Sprintf("list_item\x00%d\x00%t\x00%s", item.Depth, item.Ordered, item.Text))
	}
	return keys
}

func tableKeys(cells []ParserTableCellV1) []string {
	keys := make([]string, 0, len(cells))
	for _, cell := range cells {
		keys = append(keys, fmt.Sprintf("table\x00%d\x00%d\x00%d\x00%s", cell.Table, cell.Row, cell.Column, cell.Text))
	}
	return keys
}

func multisetF1(want, got []string, applicable bool) ParserMetricValueV1 {
	if !applicable {
		return ParserMetricValueV1{}
	}
	wantCounts := make(map[string]int, len(want))
	gotCounts := make(map[string]int, len(got))
	for _, key := range want {
		wantCounts[key]++
	}
	for _, key := range got {
		gotCounts[key]++
	}
	truePositive := 0
	for key, count := range wantCounts {
		if gotCounts[key] < count {
			count = gotCounts[key]
		}
		truePositive += count
	}
	if truePositive == 0 {
		return applicableMetric(0)
	}
	precision := float64(truePositive) / float64(len(got))
	recall := float64(truePositive) / float64(len(want))
	return applicableMetric(2 * precision * recall / (precision + recall))
}

func formulaMetric(want, got []string) ParserMetricValueV1 {
	if len(want) == 0 {
		return ParserMetricValueV1{}
	}
	if len(want) != 1 || len(got) != 1 {
		return applicableMetric(0)
	}
	return similarityMetric(want[0], got[0])
}

func readingOrderMetric(want, got []ParserAnchorMatchV1) ParserMetricValueV1 {
	if len(want) < 2 {
		return ParserMetricValueV1{}
	}
	gotByText := make(map[string]ParserAnchorMatchV1, len(got))
	for _, match := range got {
		gotByText[match.Text] = match
	}
	correct, total := 0, 0
	for left := 0; left < len(want); left++ {
		for right := left + 1; right < len(want); right++ {
			total++
			leftMatch, leftOK := gotByText[want[left].Text]
			rightMatch, rightOK := gotByText[want[right].Text]
			if leftOK && rightOK && leftMatch.Count == 1 && rightMatch.Count == 1 && leftMatch.RuneOffset < rightMatch.RuneOffset {
				correct++
			}
		}
	}
	return applicableMetric(float64(correct) / float64(total))
}

func applicableMetric(value float64) ParserMetricValueV1 {
	return ParserMetricValueV1{Applicable: true, Value: &value}
}

func uniqueTableNumbers(cells []ParserTableCellV1) []int {
	set := make(map[int]struct{})
	for _, cell := range cells {
		set[cell.Table] = struct{}{}
	}
	result := make([]int, 0, len(set))
	for number := range set {
		result = append(result, number)
	}
	sort.Ints(result)
	return result
}
