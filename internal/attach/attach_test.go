package attach

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"errors"
	"strings"
	"testing"
)

var png = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 20)...)

func docx(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("word/document.xml")
	_, _ = w.Write([]byte(`<w:document xmlns:w="x"><w:body>` + body + `</w:body></w:document>`))
	_ = zw.Close()
	return buf.Bytes()
}

func pdf(stream string) []byte {
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	_, _ = zw.Write([]byte(stream))
	_ = zw.Close()
	return []byte("%PDF-1.4\n1 0 obj\n<< /Filter /FlateDecode >>\nstream\n" + z.String() + "\nendstream\nendobj\n%%EOF")
}

func TestSniffGoesByBytesNotByName(t *testing.T) {
	cases := []struct {
		name, kind, mime string
		data             []byte
	}{
		{"a.txt", KindImage, "image/png", png},
		{"a.png", KindText, "text/plain", []byte("hello, just text")},
		{"a.jpg", KindImage, "image/jpeg", []byte("\xff\xd8\xff\xe0abc")},
		{"a.gif", KindImage, "image/gif", []byte("GIF89a....")},
		{"a.webp", KindImage, "image/webp", []byte("RIFF\x00\x00\x00\x00WEBPVP8 ")},
		{"x.pdf", KindPDF, "application/pdf", []byte("%PDF-1.7\n")},
		{"notes.md", KindText, "text/markdown", []byte("# hi")},
		{"data.csv", KindText, "text/csv", []byte("a,b\n1,2")},
		{"logo.svg", KindText, "image/svg+xml", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)},
		{"r.docx", KindDoc, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", docx(t, "")},
		{"blob.bin", KindFile, "application/octet-stream", []byte("\x00\x01\x02\x03\xff\xfe")},
		{"a.zip", KindFile, "application/zip", []byte("PK\x03\x04\x14\x00\x00\x00")},
	}
	for _, c := range cases {
		mime, kind := Sniff(c.name, c.data)
		if kind != c.kind || mime != c.mime {
			t.Errorf("%s: got %s %s, want %s %s", c.name, kind, mime, c.kind, c.mime)
		}
	}
}

func TestInspectRefuses(t *testing.T) {
	big := append(append([]byte{}, png...), make([]byte, MaxImage)...)
	cases := map[string][]byte{
		"empty.txt": nil,
		"run.exe":   []byte("MZ\x90\x00\x03"),
		"run":       []byte("\x7fELF\x02\x01"),
		"huge.png":  big,
	}
	for name, data := range cases {
		_, err := Inspect(Upload{Name: name, MIME: "image/png", Data: data})
		if !errors.Is(err, ErrRefused) {
			t.Errorf("%s: want a refusal, got %v", name, err)
		}
	}
	a, err := Inspect(Upload{Name: "../../etc/passwd.png", MIME: "text/plain", Data: png})
	if err != nil || a.Name != "passwd.png" || a.Kind != KindImage || a.MIME != "image/png" || a.Size != len(png) {
		t.Fatalf("got %+v %v", a, err)
	}
}

func TestCheckLimitsAMessage(t *testing.T) {
	if err := Check(make([]Upload, MaxFiles+1)); !errors.Is(err, ErrRefused) {
		t.Fatalf("too many files: %v", err)
	}
	if err := Check([]Upload{{Data: make([]byte, MaxTotal)}, {Data: []byte("x")}}); !errors.Is(err, ErrRefused) {
		t.Fatalf("too many bytes: %v", err)
	}
	if err := Check([]Upload{{Data: []byte("x")}}); err != nil {
		t.Fatal(err)
	}
}

func TestCleanName(t *testing.T) {
	cases := map[string]string{
		"report.pdf":              "report.pdf",
		"../../etc/passwd":        "passwd",
		`C:\Users\me\cv.docx`:     "cv.docx",
		"..":                      "file",
		"":                        "file",
		".bashrc":                 "bashrc",
		"a\x00b\nc.txt":           "abc.txt",
		"we<ird>:name?.txt":       "we_ird__name_.txt",
		"foto de gatos ñandú.png": "foto de gatos ñandú.png",
		"/":                       "file",
	}
	for in, want := range cases {
		if got := CleanName(in); got != want {
			t.Errorf("CleanName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := CleanName(strings.Repeat("ñ", 500)); len([]rune(got)) != MaxNameRunes {
		t.Fatalf("long name has %d runes", len([]rune(got)))
	}
}

func TestDiskNameFollowsTheRealType(t *testing.T) {
	if got := DiskName(Attachment{Name: "pic.txt", MIME: "image/png"}); got != "pic.png" {
		t.Fatal(got)
	}
	if got := DiskName(Attachment{Name: "pic.jpeg", MIME: "image/jpeg"}); got != "pic.jpeg" {
		t.Fatal(got)
	}
	if got := DiskName(Attachment{Name: "notes.md", MIME: "text/markdown"}); got != "notes.md" {
		t.Fatal(got)
	}
}

func TestText(t *testing.T) {
	txt, cut := Text(Attachment{Kind: KindText}, []byte("héllo\x00 world"), 0)
	if txt != "héllo world" || cut {
		t.Fatalf("%q %v", txt, cut)
	}
	txt, cut = Text(Attachment{Kind: KindText}, []byte("abcdef"), 3)
	if txt != "abc" || !cut {
		t.Fatalf("%q %v", txt, cut)
	}
	d := docx(t, `<w:p><w:r><w:t>One</w:t></w:r><w:r><w:tab/><w:t>two</w:t></w:r></w:p><w:p><w:r><w:t>Three</w:t></w:r></w:p>`)
	if txt, _ := Text(Attachment{Kind: KindDoc}, d, 0); txt != "One\ttwo\nThree" {
		t.Fatalf("%q", txt)
	}
	if txt, _ := Text(Attachment{Kind: KindDoc}, []byte("not a zip"), 0); txt != "" {
		t.Fatalf("%q", txt)
	}
	p := pdf("BT /F1 12 Tf (Hello \\(PDF\\)) Tj 0 -14 Td [(Wor) -20 (ld)] TJ <4142> Tj ET")
	txt, _ = Text(Attachment{Kind: KindPDF}, p, 0)
	for _, want := range []string{"Hello (PDF)", "World", "AB"} {
		if !strings.Contains(txt, want) {
			t.Errorf("pdf text %q lacks %q", txt, want)
		}
	}
	if txt, _ := Text(Attachment{Kind: KindImage}, png, 0); txt != "" {
		t.Fatalf("an image has no text: %q", txt)
	}
	if got := unescapePDF([]byte(`a\101\n\\`)); got != "aA\n\\" {
		t.Fatalf("%q", got)
	}
}
