package evaluation

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

type authoredBlock struct {
	Kind    string
	Level   int
	Depth   int
	Ordered bool
	Text    string
	Rows    [][]string
}

func buildDigitalMulticolumnPDF(document parserAuthoredDocumentV1) []byte {
	blocks := parseAuthoredBlocks(document.Markdown)
	lines := authoredPlainLines(blocks)
	var content strings.Builder
	content.WriteString(pdfText(72, 748, 18, lines[0]))
	left, right := lines[1:4], lines[4:]
	for index, line := range left {
		content.WriteString(pdfText(62, 690-float64(index*44), 11, line))
	}
	for index, line := range right {
		content.WriteString(pdfText(320, 690-float64(index*44), 11, line))
	}
	content.WriteString("0.7 w 300 150 m 300 710 l S\n")
	return assembleTextPDF([]byte(content.String()))
}

func buildTableFormulaPDF(document parserAuthoredDocumentV1) []byte {
	blocks := parseAuthoredBlocks(document.Markdown)
	var content strings.Builder
	y := 748.0
	for _, block := range blocks {
		switch block.Kind {
		case "heading":
			content.WriteString(pdfText(72, y, 18, block.Text))
			y -= 54
		case "paragraph", "formula":
			content.WriteString(pdfText(72, y, 11, block.Text))
			y -= 38
		case "table":
			cellWidth, cellHeight := 170.0, 30.0
			top := y
			for rowIndex, row := range block.Rows {
				for columnIndex, cell := range row {
					x := 72 + float64(columnIndex)*cellWidth
					fmt.Fprintf(&content, "%.1f %.1f %.1f %.1f re S\n", x, top-float64(rowIndex+1)*cellHeight, cellWidth, cellHeight)
					content.WriteString(pdfText(x+8, top-float64(rowIndex)*cellHeight-20, 10, cell))
				}
			}
			y -= float64(len(block.Rows))*cellHeight + 30
		}
	}
	return assembleTextPDF([]byte(content.String()))
}

func buildScannedPagePNG(document parserAuthoredDocumentV1) []byte {
	small := image.NewRGBA(image.Rect(0, 0, 400, 533))
	draw.Draw(small, small.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	drawer := &font.Drawer{Dst: small, Src: image.NewUniform(color.RGBA{20, 20, 20, 255}), Face: basicfont.Face7x13}
	y := 58
	for index, line := range authoredPlainLines(parseAuthoredBlocks(document.Markdown)) {
		drawer.Dot = fixed.P(22, y)
		drawer.DrawString(line)
		if index == 0 {
			y += 52
		} else {
			y += 38
		}
	}
	large := image.NewRGBA(image.Rect(0, 0, 1200, 1599))
	for y := 0; y < large.Bounds().Dy(); y++ {
		for x := 0; x < large.Bounds().Dx(); x++ {
			large.Set(x, y, small.At(x/3, y/3))
		}
	}
	var output bytes.Buffer
	_ = png.Encode(&output, large)
	return output.Bytes()
}

func buildImagePDF(pagePNG []byte) []byte {
	decoded, err := png.Decode(bytes.NewReader(pagePNG))
	if err != nil {
		panic(err)
	}
	bounds := decoded.Bounds()
	raw := make([]byte, 0, bounds.Dx()*bounds.Dy()*3)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := decoded.At(x, y).RGBA()
			raw = append(raw, byte(r>>8), byte(g>>8), byte(b>>8))
		}
	}
	compressed := zlibBytes(raw)
	imageObject := pdfStream(fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /FlateDecode", bounds.Dx(), bounds.Dy()), compressed)
	content := []byte("q 540 0 0 720 36 36 cm /Im1 Do Q\n")
	return assemblePDF(content, imageObject)
}

func assembleTextPDF(content []byte) []byte { return assemblePDF(content, nil) }

func assemblePDF(content, imageObject []byte) []byte {
	resources := "<< /Font << /F1 4 0 R >> >>"
	if imageObject != nil {
		resources = "<< /Font << /F1 4 0 R >> /XObject << /Im1 6 0 R >> >>"
	}
	objects := [][]byte{
		[]byte("<< /Type /Catalog /Pages 2 0 R >>"),
		[]byte("<< /Type /Pages /Kids [3 0 R] /Count 1 >>"),
		[]byte("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources " + resources + " /Contents 5 0 R >>"),
		[]byte("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"),
		pdfStream("", content),
	}
	if imageObject != nil {
		objects = append(objects, imageObject)
	}
	var output bytes.Buffer
	output.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = output.Len()
		fmt.Fprintf(&output, "%d 0 obj\n", index+1)
		output.Write(object)
		output.WriteString("\nendobj\n")
	}
	xref := output.Len()
	fmt.Fprintf(&output, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for index := 1; index < len(offsets); index++ {
		fmt.Fprintf(&output, "%010d 00000 n \n", offsets[index])
	}
	fmt.Fprintf(&output, "trailer\n<< /Size %d /Root 1 0 R /ID [<746f706963332d7061727365722d7631><746f706963332d7061727365722d7631>] >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return output.Bytes()
}

func pdfStream(dictionary string, data []byte) []byte {
	var output bytes.Buffer
	output.WriteString("<< ")
	if dictionary != "" {
		output.WriteString(dictionary)
		output.WriteByte(' ')
	}
	fmt.Fprintf(&output, "/Length %d >>\nstream\n", len(data))
	output.Write(data)
	output.WriteString("\nendstream")
	return output.Bytes()
}

func pdfText(x, y, size float64, value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "(", "\\(", ")", "\\)")
	return fmt.Sprintf("BT /F1 %.1f Tf %.1f %.1f Td (%s) Tj ET\n", size, x, y, replacer.Replace(value))
}

func zlibBytes(contents []byte) []byte {
	var output bytes.Buffer
	writer, _ := zlib.NewWriterLevel(&output, zlib.BestCompression)
	_, _ = writer.Write(contents)
	_ = writer.Close()
	return output.Bytes()
}

func buildHeadingListTableDOCX(document parserAuthoredDocumentV1) []byte {
	return buildDOCX(parseAuthoredBlocks(document.Markdown), nil)
}

func buildEmbeddedImageDOCX(document parserAuthoredDocumentV1) []byte {
	return buildDOCX(parseAuthoredBlocks(document.Markdown), buildDiagramPNG())
}

func buildDOCX(blocks []authoredBlock, imagePNG []byte) []byte {
	var body strings.Builder
	paragraphIndex := 0
	for _, block := range blocks {
		switch block.Kind {
		case "heading":
			body.WriteString(docxParagraph(block.Text, "Heading1"))
		case "paragraph", "formula", "code":
			body.WriteString(docxParagraph(block.Text, ""))
			paragraphIndex++
			if imagePNG != nil && paragraphIndex == 2 {
				body.WriteString(docxImageParagraph())
			}
		case "list":
			body.WriteString(docxListParagraph(block.Text, block.Depth, block.Ordered))
		case "table":
			body.WriteString(docxTable(block.Rows))
		}
	}
	if imagePNG != nil && paragraphIndex < 2 {
		body.WriteString(docxImageParagraph())
	}
	documentXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"><w:body>` + body.String() + `<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1080" w:right="1080" w:bottom="1080" w:left="1080"/></w:sectPr></w:body></w:document>`
	parts := map[string][]byte{
		"[Content_Types].xml":          []byte(docxContentTypes(imagePNG != nil)),
		"_rels/.rels":                  []byte(docxPackageRelationships),
		"docProps/app.xml":             []byte(docxAppProperties),
		"docProps/core.xml":            []byte(docxCoreProperties),
		"word/document.xml":            []byte(documentXML),
		"word/styles.xml":              []byte(docxStyles),
		"word/numbering.xml":           []byte(docxNumbering),
		"word/_rels/document.xml.rels": []byte(docxDocumentRelationships(imagePNG != nil)),
	}
	if imagePNG != nil {
		parts["word/media/image1.png"] = imagePNG
	}
	return deterministicZIP(parts)
}

func deterministicZIP(parts map[string][]byte) []byte {
	names := make([]string, 0, len(parts))
	for name := range parts {
		names = append(names, name)
	}
	sort.Strings(names)
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		header.SetMode(0o644)
		writer, _ := archive.CreateHeader(header)
		_, _ = writer.Write(parts[name])
	}
	_ = archive.Close()
	return output.Bytes()
}

func buildDiagramPNG() []byte {
	canvas := image.NewRGBA(image.Rect(0, 0, 640, 260))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(canvas, image.Rect(0, 0, 640, 85), image.NewUniform(color.RGBA{82, 150, 76, 255}), image.Point{}, draw.Src)
	draw.Draw(canvas, image.Rect(0, 175, 640, 260), image.NewUniform(color.RGBA{82, 150, 76, 255}), image.Point{}, draw.Src)
	draw.Draw(canvas, image.Rect(0, 85, 640, 175), image.NewUniform(color.RGBA{65, 145, 220, 255}), image.Point{}, draw.Src)
	var output bytes.Buffer
	_ = png.Encode(&output, canvas)
	return output.Bytes()
}

func docxParagraph(value, style string) string {
	properties := ""
	if style != "" {
		properties = `<w:pPr><w:pStyle w:val="` + style + `"/></w:pPr>`
	}
	return `<w:p>` + properties + `<w:r><w:t xml:space="preserve">` + xmlEscape(value) + `</w:t></w:r></w:p>`
}

func docxListParagraph(value string, depth int, ordered bool) string {
	_ = ordered // Numbering format is selected by the shared multilevel definition.
	return `<w:p><w:pPr><w:numPr><w:ilvl w:val="` + strconv.Itoa(max(0, depth-1)) + `"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>` + xmlEscape(value) + `</w:t></w:r></w:p>`
}

func docxTable(rows [][]string) string {
	var output strings.Builder
	output.WriteString(`<w:tbl><w:tblPr><w:tblBorders><w:top w:val="single" w:sz="8"/><w:left w:val="single" w:sz="8"/><w:bottom w:val="single" w:sz="8"/><w:right w:val="single" w:sz="8"/><w:insideH w:val="single" w:sz="8"/><w:insideV w:val="single" w:sz="8"/></w:tblBorders></w:tblPr>`)
	for _, row := range rows {
		output.WriteString(`<w:tr>`)
		for _, cell := range row {
			output.WriteString(`<w:tc><w:tcPr><w:tcW w:w="3600" w:type="dxa"/></w:tcPr>`)
			output.WriteString(docxParagraph(cell, ""))
			output.WriteString(`</w:tc>`)
		}
		output.WriteString(`</w:tr>`)
	}
	output.WriteString(`</w:tbl>`)
	return output.String()
}

func docxImageParagraph() string {
	return `<w:p><w:r><w:drawing><wp:inline><wp:extent cx="4572000" cy="1857375"/><wp:docPr id="1" name="River diagram" descr="A blue river between two green banks"/><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic><pic:nvPicPr><pic:cNvPr id="0" name="image1.png"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="rIdImage1"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill><pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="4572000" cy="1857375"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`
}

func docxContentTypes(hasImage bool) string {
	imageDefault := ""
	if hasImage {
		imageDefault = `<Default Extension="png" ContentType="image/png"/>`
	}
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>` + imageDefault + `<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/><Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/><Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/><Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/></Types>`
}

func docxDocumentRelationships(hasImage bool) string {
	imageRelationship := ""
	if hasImage {
		imageRelationship = `<Relationship Id="rIdImage1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image1.png"/>`
	}
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rIdStyles" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/><Relationship Id="rIdNumbering" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>` + imageRelationship + `</Relationships>`
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;", "'", "&apos;")
	return replacer.Replace(value)
}

func parseAuthoredBlocks(markdown string) []authoredBlock {
	lines := strings.Split(normalizeParserSource(markdown), "\n")
	blocks := make([]authoredBlock, 0)
	for index := 0; index < len(lines); {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			index++
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			level := 0
			for level < len(trimmed) && trimmed[level] == '#' {
				level++
			}
			blocks = append(blocks, authoredBlock{Kind: "heading", Level: level, Text: strings.TrimSpace(trimmed[level:])})
			index++
			continue
		}
		if strings.HasPrefix(trimmed, "|") {
			rows := make([][]string, 0)
			for index < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[index]), "|") {
				cells := splitMarkdownTableRow(lines[index])
				if !markdownSeparatorRow(cells) {
					rows = append(rows, cells)
				}
				index++
			}
			blocks = append(blocks, authoredBlock{Kind: "table", Rows: rows})
			continue
		}
		leading := len(line) - len(strings.TrimLeft(line, " "))
		if strings.HasPrefix(trimmed, "- ") {
			blocks = append(blocks, authoredBlock{Kind: "list", Depth: leading/2 + 1, Text: strings.TrimSpace(trimmed[2:])})
			index++
			continue
		}
		if dot := strings.Index(trimmed, ". "); dot > 0 {
			if _, err := strconv.Atoi(trimmed[:dot]); err == nil {
				blocks = append(blocks, authoredBlock{Kind: "list", Depth: leading/2 + 1, Ordered: true, Text: strings.TrimSpace(trimmed[dot+2:])})
				index++
				continue
			}
		}
		kind := "paragraph"
		textValue := trimmed
		if strings.HasPrefix(trimmed, "$") && strings.HasSuffix(trimmed, "$") {
			kind = "formula"
			textValue = normalizeFormula(strings.Trim(trimmed, "$"))
		}
		blocks = append(blocks, authoredBlock{Kind: kind, Text: textValue})
		index++
	}
	return blocks
}

func splitMarkdownTableRow(line string) []string {
	trimmed := strings.Trim(strings.TrimSpace(line), "|")
	parts := strings.Split(trimmed, "|")
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	return parts
}

func markdownSeparatorRow(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		trimmed := strings.Trim(cell, " :-")
		if trimmed != "" {
			return false
		}
	}
	return true
}

func authoredPlainLines(blocks []authoredBlock) []string {
	lines := make([]string, 0)
	for _, block := range blocks {
		switch block.Kind {
		case "heading", "paragraph", "formula", "code":
			lines = append(lines, block.Text)
		case "list":
			prefix := "- "
			if block.Ordered {
				prefix = "1. "
			}
			lines = append(lines, strings.Repeat("  ", max(0, block.Depth-1))+prefix+block.Text)
		case "table":
			for _, row := range block.Rows {
				lines = append(lines, strings.Join(row, " | "))
			}
		}
	}
	return lines
}

const docxPackageRelationships = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/><Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/></Relationships>`
const docxAppProperties = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Application>WeKnora deterministic corpus generator</Application><AppVersion>1.0</AppVersion></Properties>`
const docxCoreProperties = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"><dc:title>WeKnora Topic 3 parser corpus</dc:title><dc:creator>WeKnora project</dc:creator><dcterms:created xsi:type="dcterms:W3CDTF">2026-09-12T00:00:00Z</dcterms:created><dcterms:modified xsi:type="dcterms:W3CDTF">2026-09-12T00:00:00Z</dcterms:modified></cp:coreProperties>`
const docxStyles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:rPr><w:rFonts w:ascii="Arial" w:hAnsi="Arial"/><w:sz w:val="22"/></w:rPr></w:style><w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:spacing w:before="240" w:after="160"/></w:pPr><w:rPr><w:b/><w:sz w:val="32"/></w:rPr></w:style></w:styles>`
const docxNumbering = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:abstractNum w:abstractNumId="0"><w:multiLevelType w:val="multilevel"/><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:lvlText w:val="•"/><w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr></w:lvl><w:lvl w:ilvl="1"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%2."/><w:pPr><w:ind w:left="1080" w:hanging="360"/></w:pPr></w:lvl></w:abstractNum><w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num></w:numbering>`
