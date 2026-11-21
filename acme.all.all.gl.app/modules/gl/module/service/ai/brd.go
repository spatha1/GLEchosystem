package ai

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// BRD is a business requirements document as plain text.
type BRD struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// Limits on what is read. A document over the limit is refused, never cut short: the AI
// would otherwise miss requirements without anyone knowing.
const (
	MaxBRDBytes = 1536 << 10 // the upload travels base64-encoded within the 2 MB request limit
	MaxBRDChars = 200_000
)

// ExtractText reads a BRD: Word (.docx), plain text (.txt) or Markdown (.md).
func ExtractText(name string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("the file is empty")
	}
	if len(data) > MaxBRDBytes {
		return "", fmt.Errorf("the file is %.1f MB; the limit is 1.5 MB. Remove images from the document, or save it as text (.txt)", float64(len(data))/(1<<20))
	}
	var text string
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt", ".md", ".markdown":
		if !utf8.Valid(data) {
			return "", fmt.Errorf("the text file is not UTF-8")
		}
		text = string(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))
	case ".docx":
		t, err := docxText(data)
		if err != nil {
			return "", err
		}
		text = t
	case ".pdf", ".doc":
		return "", fmt.Errorf("%s files are not read yet: save the BRD as Word (.docx), text (.txt) or Markdown (.md)", strings.ToLower(filepath.Ext(name)))
	default:
		return "", fmt.Errorf("unsupported file type: use Word (.docx), text (.txt) or Markdown (.md)")
	}
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if text == "" {
		return "", fmt.Errorf("no text was found in the document")
	}
	if n := utf8.RuneCountInString(text); n > MaxBRDChars {
		return "", fmt.Errorf("the BRD has %d characters; the limit is %d. Split it, for example one BRD per business area", n, MaxBRDChars)
	}
	return text, nil
}

// docxText reads the paragraphs and table cells of word/document.xml, in order. Each
// paragraph becomes a line; table cells are separated by " | ".
func docxText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("the file is not a valid Word document")
	}
	var doc *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			doc = f
		}
	}
	if doc == nil {
		return "", fmt.Errorf("the Word document has no body (word/document.xml)")
	}
	rc, err := doc.Open()
	if err != nil {
		return "", fmt.Errorf("the Word document could not be opened")
	}
	defer rc.Close()
	dec := xml.NewDecoder(io.LimitReader(rc, 64<<20))
	var out, para strings.Builder
	inText, cellOpen := false, false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("the Word document could not be read")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				para.WriteString("\t")
			case "br", "cr":
				para.WriteString("\n")
			case "tc":
				cellOpen = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				line := strings.TrimRight(para.String(), " \t")
				para.Reset()
				if cellOpen {
					if line != "" {
						out.WriteString(line)
						out.WriteString(" ")
					}
				} else {
					out.WriteString(line)
					out.WriteString("\n")
				}
			case "tc":
				cellOpen = false
				out.WriteString("| ")
			case "tr":
				out.WriteString("\n")
			}
		case xml.CharData:
			if inText {
				para.Write(t)
			}
		}
	}
	return out.String(), nil
}
