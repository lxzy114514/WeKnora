package evaluation

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractParserComponentsUsesTheSingleMarkdownContract(t *testing.T) {
	input := "## Ｃａｆｅ\u0301\u00a0Plan\r\n\r\n" +
		"- First\u200b item\n  1. Nested `code`\n\n" +
		"| Key | Value |\n| --- | --- |\n| A | 10 |\n\n" +
		"<table><tr><th>H</th><th>V</th></tr><tr><td>B</td><td>20</td></tr></table>\n\n" +
		"Formula: $E = mc^2$. Price: $12.50. [visible label](https://example.invalid). " +
		"![ignored alt](image.png).\n\n```go\nfmt.Println(\"kept\")\n```\n"

	got, err := ExtractParserComponents(input, []string{"Café Plan", "First item", "visible label"})
	require.NoError(t, err)
	require.Equal(t, []ParserHeadingV1{{Level: 2, Text: "Café Plan"}}, got.Headings)
	require.Equal(t, []ParserListItemV1{
		{Depth: 1, Ordered: false, Text: "First item"},
		{Depth: 2, Ordered: true, Text: "Nested code"},
	}, got.ListItems)
	require.Equal(t, []ParserTableCellV1{
		{Table: 0, Row: 0, Column: 0, Text: "Key"},
		{Table: 0, Row: 0, Column: 1, Text: "Value"},
		{Table: 0, Row: 1, Column: 0, Text: "A"},
		{Table: 0, Row: 1, Column: 1, Text: "10"},
		{Table: 1, Row: 0, Column: 0, Text: "H"},
		{Table: 1, Row: 0, Column: 1, Text: "V"},
		{Table: 1, Row: 1, Column: 0, Text: "B"},
		{Table: 1, Row: 1, Column: 1, Text: "20"},
	}, got.TableCells)
	require.Equal(t, []string{"E=mc^2"}, got.Formulas)
	require.Equal(t, 1, got.ImageCount)
	require.Equal(t, "Café Plan\nFirst item\nNested code\nFormula: . Price: $12.50. visible label . .\nfmt.Println(\"kept\")", got.Text)
	require.NotContains(t, got.Text, "https://example.invalid")
	require.NotContains(t, got.Text, "ignored alt")
	require.Contains(t, got.Text, "$12.50")
	require.Equal(t, []ParserAnchorMatchV1{
		{Text: "Café Plan", Count: 1, RuneOffset: 0},
		{Text: "First item", Count: 1, RuneOffset: 10},
		{Text: "visible label", Count: 1, RuneOffset: got.AnchorMatches[2].RuneOffset},
	}, got.AnchorMatches)
}

func TestScoreParserComponentsComputesFiveIndependentMetrics(t *testing.T) {
	groundTruth := ParserComponentsV1{
		Text:      "abc A B C",
		Headings:  []ParserHeadingV1{{Level: 1, Text: "A"}},
		ListItems: []ParserListItemV1{{Depth: 1, Ordered: false, Text: "X"}},
		TableCells: []ParserTableCellV1{
			{Table: 0, Row: 0, Column: 0, Text: "A"},
			{Table: 0, Row: 0, Column: 1, Text: "B"},
		},
		Formulas: []string{"x=1"},
		AnchorMatches: []ParserAnchorMatchV1{
			{Text: "A", Count: 1, RuneOffset: 4},
			{Text: "B", Count: 1, RuneOffset: 6},
			{Text: "C", Count: 1, RuneOffset: 8},
		},
	}
	prediction := ParserComponentsV1{
		Text:      "adc A C B",
		Headings:  []ParserHeadingV1{{Level: 1, Text: "A"}},
		ListItems: []ParserListItemV1{{Depth: 1, Ordered: false, Text: "Y"}},
		TableCells: []ParserTableCellV1{
			{Table: 0, Row: 0, Column: 0, Text: "A"},
			{Table: 0, Row: 0, Column: 1, Text: "Z"},
		},
		Formulas: []string{"x=2"},
		AnchorMatches: []ParserAnchorMatchV1{
			{Text: "A", Count: 1, RuneOffset: 4},
			{Text: "B", Count: 1, RuneOffset: 8},
			{Text: "C", Count: 1, RuneOffset: 6},
		},
	}

	got := ScoreParserComponents(groundTruth, prediction)
	requireMetric(t, got.TextSimilarity, 2.0/3.0)
	requireMetric(t, got.StructureF1, 0.5)
	requireMetric(t, got.TableCellF1, 0.5)
	requireMetric(t, got.FormulaSimilarity, 2.0/3.0)
	requireMetric(t, got.ReadingOrderAccuracy, 2.0/3.0)
	require.Equal(t, 0, got.SpuriousTableCount)
	require.Equal(t, 0, got.SpuriousFormulaCount)
}

func TestScoreParserComponentsKeepsNAAndSpuriousDistinctFromZero(t *testing.T) {
	got := ScoreParserComponents(
		ParserComponentsV1{AnchorMatches: []ParserAnchorMatchV1{
			{Text: "A", Count: 1, RuneOffset: 0},
			{Text: "B", Count: 1, RuneOffset: 2},
			{Text: "C", Count: 1, RuneOffset: 4},
		}},
		ParserComponentsV1{
			TableCells: []ParserTableCellV1{{Table: 0, Row: 0, Column: 0, Text: "hallucinated"}},
			Formulas:   []string{"x=1"},
			AnchorMatches: []ParserAnchorMatchV1{
				{Text: "A", Count: 2, RuneOffset: 0},
				{Text: "B", Count: 1, RuneOffset: 2},
				{Text: "C", Count: 0, RuneOffset: -1},
			},
		},
	)
	require.False(t, got.TextSimilarity.Applicable)
	require.False(t, got.TableCellF1.Applicable)
	require.False(t, got.FormulaSimilarity.Applicable)
	requireMetric(t, got.ReadingOrderAccuracy, 0)
	require.Equal(t, 1, got.SpuriousTableCount)
	require.Equal(t, 1, got.SpuriousFormulaCount)
}

func requireMetric(t *testing.T, metric ParserMetricValueV1, want float64) {
	t.Helper()
	require.True(t, metric.Applicable)
	require.NotNil(t, metric.Value)
	require.True(t, math.Abs(*metric.Value-want) < 1e-12, "got %.16f want %.16f", *metric.Value, want)
}
