// Package attach checks and describes the files a person attaches to a message.
//
// What a file is comes from its bytes, never from the name or the type the sender claims: a
// PNG is recognised by its signature, a PDF by its header, a Word document by its parts, text by
// being valid UTF-8 with no NUL. A file that is a native executable is refused. The name is
// only a label: it is cleaned of folders and control characters, and the bytes are stored
// under an id of our own, so no name can reach another path.
package attach

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Kinds of attachment.
const (
	KindImage = "image" // png, jpeg, gif, webp: shown to the model as a picture
	KindPDF   = "pdf"   // a PDF: sent as a document where the provider reads them
	KindText  = "text"  // text and code: put in the message
	KindDoc   = "doc"   // a Word document: its text is put in the message
	KindFile  = "file"  // anything else: saved, and told about
)

// Limits bound what one message may carry.
const (
	MaxFiles     = 6
	MaxTotal     = 20 << 20 // all the files of a message
	MaxImage     = 5 << 20  // the most a provider takes as a picture
	MaxPDF       = 10 << 20
	MaxText      = 2 << 20
	MaxOther     = 10 << 20
	MaxNameRunes = 100
	// MaxBody is the largest request body for a message with files: the files in base64 plus the
	// text and the JSON around them.
	MaxBody = MaxTotal/3*4 + 1<<20
)

// Upload is a file as it arrives: its name and type as the sender gave them, and its bytes.
type Upload struct {
	Name string `json:"name"`
	MIME string `json:"mime"`
	Data []byte `json:"-"`
}

// Attachment describes a stored file. The bytes live on disk under ID.
type Attachment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	MIME string `json:"mime"`
	Kind string `json:"kind"`
	Size int    `json:"size"`
}

// ErrRefused is wrapped by every refusal, so a caller can tell a bad file from a broken disk.
var ErrRefused = errors.New("file refused")

func refuse(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrRefused, fmt.Sprintf(format, a...))
}

// Inspect works out what a file really is, and refuses one that is empty, too big for its kind or
// a program. The Attachment has no ID yet.
func Inspect(u Upload) (Attachment, error) {
	name := CleanName(u.Name)
	if len(u.Data) == 0 {
		return Attachment{}, refuse("%s is empty", name)
	}
	mimeType, kind := Sniff(u.Name, u.Data)
	if kind == "" {
		return Attachment{}, refuse("%s is a program, not a document", name)
	}
	if limit := limitOf(kind); len(u.Data) > limit {
		return Attachment{}, refuse("%s is %s; the most for this kind of file is %s", name, human(len(u.Data)), human(limit))
	}
	return Attachment{Name: name, MIME: mimeType, Kind: kind, Size: len(u.Data)}, nil
}

// Check applies the limits of a whole message to its files.
func Check(files []Upload) error {
	if len(files) > MaxFiles {
		return refuse("a message takes at most %d files", MaxFiles)
	}
	total := 0
	for _, f := range files {
		total += len(f.Data)
	}
	if total > MaxTotal {
		return refuse("the files add up to %s; the most for a message is %s", human(total), human(MaxTotal))
	}
	return nil
}

func limitOf(kind string) int {
	switch kind {
	case KindImage:
		return MaxImage
	case KindPDF:
		return MaxPDF
	case KindText:
		return MaxText
	}
	return MaxOther
}

func human(n int) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d KB", (n+1023)/1024)
}

// Sniff returns the media type and the kind of a file from its bytes. The name only helps to
// tell which kind of text it is. An empty kind means a native executable.
func Sniff(name string, data []byte) (string, string) {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png", KindImage
	case bytes.HasPrefix(data, []byte("\xff\xd8\xff")):
		return "image/jpeg", KindImage
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return "image/gif", KindImage
	case len(data) > 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp", KindImage
	case bytes.HasPrefix(data, []byte("%PDF-")):
		return "application/pdf", KindPDF
	case bytes.HasPrefix(data, []byte("\x7fELF")), bytes.HasPrefix(data, []byte("MZ")),
		bytes.HasPrefix(data, []byte("\xcf\xfa\xed\xfe")), bytes.HasPrefix(data, []byte("\xce\xfa\xed\xfe")),
		bytes.HasPrefix(data, []byte("\xca\xfe\xba\xbe")), bytes.HasPrefix(data, []byte("\xfe\xed\xfa")):
		return "", ""
	}
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		if isDocx(data) {
			return "application/vnd.openxmlformats-officedocument.wordprocessingml.document", KindDoc
		}
		return http.DetectContentType(data), KindFile
	}
	if looksText(data) {
		return textMIME(name), KindText
	}
	detected := http.DetectContentType(data)
	if detected == "" || strings.HasPrefix(detected, "text/") || strings.HasPrefix(detected, "image/") {
		// Bytes that are not text, and not a picture the models read: kept, not shown.
		detected = "application/octet-stream"
	}
	return detected, KindFile
}

// looksText is true for valid UTF-8 without NUL bytes (the first 64 KB decide).
func looksText(data []byte) bool {
	if len(data) > 64<<10 {
		data = data[:64<<10]
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return false
	}
	// A cut in the middle of a character is not an error.
	for i := 0; i < 4 && len(data) > 0 && !utf8.Valid(data); i++ {
		data = data[:len(data)-1]
	}
	return utf8.Valid(data)
}

var textTypes = map[string]string{
	".md": "text/markdown", ".markdown": "text/markdown", ".csv": "text/csv", ".tsv": "text/tab-separated-values",
	".json": "application/json", ".yaml": "text/yaml", ".yml": "text/yaml", ".xml": "application/xml",
	".html": "text/html", ".htm": "text/html", ".svg": "image/svg+xml", ".css": "text/css", ".js": "text/javascript",
}

// textMIME is the media type of a text file: from its extension when it is a known one, else
// text/plain. An SVG is text here (it can carry scripts), never a picture.
func textMIME(name string) string {
	ext := strings.ToLower(path.Ext(CleanName(name)))
	if t, ok := textTypes[ext]; ok {
		return t
	}
	if t := mime.TypeByExtension(ext); strings.HasPrefix(t, "text/") {
		return strings.SplitN(t, ";", 2)[0]
	}
	return "text/plain"
}

// isDocx is true for a zip that has the main part of a Word document.
func isDocx(data []byte) bool {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false
	}
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			return true
		}
	}
	return false
}

// CleanName turns whatever name came with a file into a plain one: the last part of the path
// (either separator), without control characters, line breaks or a leading dot, at most
// MaxNameRunes long. A name that comes out empty is "file".
func CleanName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	if name == "." || name == "/" {
		name = ""
	}
	var sb strings.Builder
	n := 0
	for _, r := range name {
		switch {
		case unicode.IsControl(r), r == utf8.RuneError, unicode.Is(unicode.Cf, r):
			continue
		case strings.ContainsRune(`<>:"|?*`, r):
			r = '_'
		}
		sb.WriteRune(r)
		if n++; n >= MaxNameRunes {
			break
		}
	}
	out := strings.TrimLeft(strings.TrimSpace(sb.String()), ".")
	out = strings.TrimSpace(out)
	if out == "" {
		return "file"
	}
	return out
}

// DiskName is a name for a file's place on a machine's disk: the clean name, with the extension
// the real type calls for when the name has none that fits.
func DiskName(a Attachment) string {
	name := CleanName(a.Name)
	ext := strings.ToLower(filepath.Ext(name))
	want := map[string]string{
		"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp", "application/pdf": ".pdf",
	}[a.MIME]
	if want == "" || ext == want || (a.MIME == "image/jpeg" && ext == ".jpeg") {
		return name
	}
	return strings.TrimSuffix(name, filepath.Ext(name)) + want
}
