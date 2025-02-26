/*
Package multipartstreamer helps you encode large files in MIME multipart format
without reading the entire content into memory.  It uses io.MultiReader to
combine an inner multipart.Reader with a file handle.
*/
package multipartstreamer

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
)

type MultipartStreamer struct {
	ContentType   string
	bodyBuffer    *bytes.Buffer
	bodyWriter    *multipart.Writer
	closeBuffer   *bytes.Buffer
	reader        io.Reader
	contentLength int64
}

type MSOption func(ms *MultipartStreamer)

func WithBoundary(b string) MSOption {
	return func(ms *MultipartStreamer) {
		ms.bodyWriter.SetBoundary(b)
	}
}

// New initializes a new MultipartStreamer.
func New(opts ...MSOption) (m *MultipartStreamer) {
	m = &MultipartStreamer{bodyBuffer: new(bytes.Buffer)}
	m.bodyWriter = multipart.NewWriter(m.bodyBuffer)

	for _, fopt := range opts {
		fopt(m)
	}

	boundary := m.bodyWriter.Boundary()
	m.ContentType = "multipart/form-data; boundary=" + boundary

	closeBoundary := fmt.Sprintf("\r\n--%s--\r\n", boundary)
	m.closeBuffer = bytes.NewBufferString(closeBoundary)

	return
}

// WriteFields writes multiple form fields to the multipart.Writer.
func (m *MultipartStreamer) WriteFields(fields map[string]string) error {
	var err error

	for key, value := range fields {
		err = m.bodyWriter.WriteField(key, value)
		if err != nil {
			return err
		}
	}

	return nil
}

// WriteReader adds an io.Reader to get the content of a file.
// The reader is not accessed until the multipart.Reader is copied to some output writer.
func (m *MultipartStreamer) WriteReader(key, filename string, size int64, reader io.Reader) (err error) {
	m.reader = reader
	m.contentLength = size

	_, err = m.bodyWriter.CreateFormFile(key, filename)
	return
}

// WriteReaderWithSize adds an io.Reader to get the content of a file.
// The reader is not accessed until the multipart.Reader is copied to some output writer.
func (m *MultipartStreamer) WriteReaderWithSize(key, filename string, size int64, reader io.Reader) (err error) {
	return m.WriteReaderWithHeaders(key, filename, reader, map[string]any{
		"Content-Type":   "application/octet-stream",
		"Content-Length": size,
	})
}

// WriteReaderWithHeaders adds an io.Reader to get the content of a file.
// The reader is not accessed until the multipart.Reader is copied to some output writer.
func (m *MultipartStreamer) WriteReaderWithHeaders(key, filename string, reader io.Reader, headers map[string]any) (err error) {
	m.reader = reader
	m.contentLength = (headers["Content-Length"].(int64))

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name="%s"; filename="%s"`, escapeQuotes(key), escapeQuotes(filename)))
	for k, v := range headers {
		h.Set(k, fmt.Sprintf("%v", v))
	}
	_, err = m.bodyWriter.CreatePart(h)
	return
}

// WriteFile is a shortcut for adding a local file as an io.Reader.
func (m *MultipartStreamer) WriteFile(key, filename string) error {
	fh, err := os.Open(filename)
	if err != nil {
		return err
	}

	stat, err := fh.Stat()
	if err != nil {
		return err
	}

	return m.WriteReader(key, filepath.Base(filename), stat.Size(), fh)
}

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

func escapeQuotes(s string) string {
	return quoteEscaper.Replace(s)
}

// WritePart writes a multipart "part" with specified headers and content
func (m *MultipartStreamer) WritePart(fieldname string, data io.Reader, headers map[string]string) error {
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name="%s"`, escapeQuotes(fieldname)))
	for k, v := range headers {
		h.Set(k, v)
	}

	part, err := m.bodyWriter.CreatePart(h)
	if err != nil {
		return err
	}

	_, err = io.Copy(part, data)
	return err
}

// SetupRequest sets up the http.Request body, and some crucial HTTP headers.
func (m *MultipartStreamer) SetupRequest(req *http.Request) {
	req.Body = m.GetReader()
	req.Header.Add("Content-Type", m.ContentType)
	req.ContentLength = m.Len()
}

func (m *MultipartStreamer) Boundary() string {
	return m.bodyWriter.Boundary()
}

// Len calculates the byte size of the multipart content.
func (m *MultipartStreamer) Len() int64 {
	return m.contentLength + int64(m.bodyBuffer.Len()) + int64(m.closeBuffer.Len())
}

// GetReader gets an io.ReadCloser for passing to an http.Request.
func (m *MultipartStreamer) GetReader() io.ReadCloser {
	reader := io.MultiReader(m.bodyBuffer, m.reader, m.closeBuffer)
	return ioutil.NopCloser(reader)
}
