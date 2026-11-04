package attach

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// CharsPerToken is the characters a token is taken to hold when fitting text to a context window.
// It is low on purpose (English is about 4, code and other languages fewer), so a fit is safe.
const CharsPerToken = 3

// InlineChars is how many characters of files' text may go into one message for a model with a
// context window of contextTokens: half of it, the rest is for the thread, the system prompt and
// the answer. A file within it goes whole; past it the rest stays in the file.
func InlineChars(contextTokens int) int { return contextTokens / 2 * CharsPerToken }

// Text is the text of a file for the model: the file itself when it is text, the words of a Word
// document, the best-effort text of a PDF. The second result is true when it was cut at max
// characters. Empty means there is nothing to read (a scan, a picture, a binary).
func Text(a Attachment, data []byte, max int) (string, bool) {
	var text string
	switch a.Kind {
	case KindText:
		text = string(data)
	case KindDoc:
		text = officeText(data)
	case KindPDF:
		text = pdfText(data)
	}
	text = strings.ToValidUTF8(strings.ReplaceAll(text, "\x00", ""), "")
	text = strings.TrimSpace(text)
	if max > 0 && utf8.RuneCountInString(text) > max {
		return string([]rune(text)[:max]), true
	}
	return text, false
}

// officeText reads the text of an Office Open XML file: the paragraphs of a Word document, the
// shared strings of a workbook (one per line) and the paragraphs of each slide, in slide order.
func officeText(data []byte) string {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ""
	}
	var slides []*zip.File
	var out strings.Builder
	for _, f := range zr.File {
		switch {
		case f.Name == "word/document.xml":
			out.WriteString(xmlText(f, "p"))
		case f.Name == "xl/sharedStrings.xml":
			out.WriteString(xmlText(f, "si"))
		case strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml"):
			slides = append(slides, f)
		}
	}
	sort.Slice(slides, func(i, j int) bool {
		if len(slides[i].Name) != len(slides[j].Name) {
			return len(slides[i].Name) < len(slides[j].Name)
		}
		return slides[i].Name < slides[j].Name
	})
	for n, f := range slides {
		fmt.Fprintf(&out, "--- slide %d ---\n%s", n+1, xmlText(f, "p"))
	}
	return out.String()
}

// xmlText collects the text of the t elements of an XML part, ending a line at each end element
// called para. Reading stops at 20 MB of XML, so a zip bomb costs little.
func xmlText(f *zip.File, para string) string {
	rc, err := f.Open()
	if err != nil {
		return ""
	}
	defer rc.Close()
	dec := xml.NewDecoder(io.LimitReader(rc, 20<<20))
	var sb strings.Builder
	inText := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				sb.WriteByte('\t')
			case "br":
				sb.WriteByte('\n')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case para:
				sb.WriteByte('\n')
			}
		case xml.CharData:
			if inText {
				sb.Write(t)
			}
		}
	}
	return sb.String()
}

var (
	pdfStream = regexp.MustCompile(`(?s)stream\r?\n(.*?)\r?\nendstream`)
	pdfShow   = regexp.MustCompile(`(?s)\[((?:[^\]\\]|\\.)*)\]\s*TJ|\(((?:[^()\\]|\\.)*)\)\s*(?:Tj|'|")|<([0-9A-Fa-f\s]+)>\s*Tj|\bT\*|\bET\b|\bTd\b|\bTD\b`)
	pdfPart   = regexp.MustCompile(`(?s)\(((?:[^()\\]|\\.)*)\)`)
)

// pdfText is a best-effort reading of a PDF: it inflates the content streams and collects the
// strings the text operators show. It has no font maps, so a PDF that encodes its text in custom
// fonts gives little or nothing, and then the file is sent as a document where the provider reads
// them or kept for the kitty to open.
func pdfText(data []byte) string {
	var sb strings.Builder
	for _, m := range pdfStream.FindAllSubmatch(data, -1) {
		body := m[1]
		if zr, err := zlib.NewReader(bytes.NewReader(body)); err == nil {
			if inflated, err := io.ReadAll(io.LimitReader(zr, 16<<20)); err == nil || len(inflated) > 0 {
				body = inflated
			}
		}
		if !bytes.Contains(body, []byte("BT")) {
			continue
		}
		for _, op := range pdfShow.FindAllSubmatch(body, -1) {
			switch {
			case op[1] != nil:
				for _, p := range pdfPart.FindAllSubmatch(op[1], -1) {
					sb.WriteString(unescapePDF(p[1]))
				}
			case op[2] != nil:
				sb.WriteString(unescapePDF(op[2]))
			case op[3] != nil:
				sb.WriteString(hexPDF(op[3]))
			default:
				sb.WriteByte('\n')
			}
		}
		sb.WriteByte('\n')
	}
	out := regexp.MustCompile(`\n{3,}`).ReplaceAllString(sb.String(), "\n\n")
	return out
}

func unescapePDF(b []byte) string {
	var sb strings.Builder
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c != '\\' || i+1 >= len(b) {
			sb.WriteByte(c)
			continue
		}
		i++
		switch b[i] {
		case 'n':
			sb.WriteByte('\n')
		case 'r', 'b', 'f':
		case 't':
			sb.WriteByte('\t')
		case '0', '1', '2', '3', '4', '5', '6', '7':
			j := i
			for j < len(b) && j < i+3 && b[j] >= '0' && b[j] <= '7' {
				j++
			}
			n, _ := strconv.ParseUint(string(b[i:j]), 8, 16)
			sb.WriteRune(rune(n))
			i = j - 1
		default:
			sb.WriteByte(b[i])
		}
	}
	return sb.String()
}

func hexPDF(b []byte) string {
	clean := strings.Join(strings.Fields(string(b)), "")
	if len(clean)%2 == 1 {
		clean += "0"
	}
	var sb strings.Builder
	for i := 0; i+1 < len(clean); i += 2 {
		n, err := strconv.ParseUint(clean[i:i+2], 16, 8)
		if err == nil && n >= 32 && n < 127 {
			sb.WriteByte(byte(n))
		}
	}
	return sb.String()
}
