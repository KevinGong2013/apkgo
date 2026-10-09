package httptrace

import (
	"bytes"
	"encoding/json"
	"mime"
	"net/url"
	"time"
	"unicode/utf8"
)

// Exchange is one HTTP round trip with a store, as recorded.
type Exchange struct {
	// Seq numbers the exchanges of one recording in the order they
	// completed; set by the recorder.
	Seq   int       `json:"seq,omitempty"`
	Time  time.Time `json:"time"`
	Store string    `json:"store"`

	Method  string  `json:"method"`
	URL     string  `json:"url"`
	Request Message `json:"request"`

	// Status is 0 and Response nil when the round trip failed (Error).
	Status     int      `json:"status,omitempty"`
	Response   *Message `json:"response,omitempty"`
	DurationMS int64    `json:"duration_ms"`
	Error      string   `json:"error,omitempty"`
}

// Message is one direction of an exchange. At most one of JSON, Form,
// Text and Multipart holds the body; Omitted says why none does.
type Message struct {
	Headers map[string]string `json:"headers,omitempty"`
	// Bytes is the body's size: what was seen for a recorded body, the
	// declared Content-Length otherwise (-1 when unknown).
	Bytes int64 `json:"bytes,omitempty"`

	JSON      json.RawMessage     `json:"json,omitempty"`
	Form      map[string][]string `json:"form,omitempty"`
	Text      string              `json:"text,omitempty"`
	Multipart *Multipart          `json:"multipart,omitempty"`

	// Truncated: the body was longer than MaxBody and only its start is
	// here (as Text — a cut-off JSON document no longer parses).
	Truncated bool `json:"truncated,omitempty"`
	// Omitted is "binary" for a body that was not recorded.
	Omitted string `json:"omitted,omitempty"`
}

// Multipart describes a multipart/form-data request: its fields, and for
// each file only where it sat in the form, its name and its size.
type Multipart struct {
	Fields map[string]string `json:"fields,omitempty"`
	Files  []FilePart        `json:"files,omitempty"`
}

// FilePart is the basic description of an uploaded file.
type FilePart struct {
	Field string `json:"field"`
	Name  string `json:"name"`
	Size  int64  `json:"size,omitempty"`
}

func (m *Multipart) redacted() *Multipart {
	out := &Multipart{Files: m.Files}
	if len(m.Fields) > 0 {
		out.Fields = make(map[string]string, len(m.Fields))
		for k, v := range m.Fields {
			out.Fields[k] = redactValue(k, v)
		}
	}
	return out
}

// setBody stores data, redacted, in the shape its content allows: a JSON
// document, form values, or text. Anything that isn't text is omitted.
func (m *Message) setBody(contentType string, data []byte, truncated bool) {
	if len(data) == 0 {
		return
	}
	m.Truncated = truncated
	mt, _, _ := mime.ParseMediaType(contentType)
	if !truncated {
		if mt == "application/x-www-form-urlencoded" {
			if vals, err := url.ParseQuery(string(data)); err == nil {
				m.Form = redactValues(vals)
				return
			}
		}
		if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
			if out, ok := redactJSON(trimmed); ok {
				m.JSON = out
				return
			}
		}
	} else {
		// The cut may have landed inside a character.
		for i := 0; i < utf8.UTFMax && len(data) > 0 && !utf8.Valid(data); i++ {
			data = data[:len(data)-1]
		}
	}
	if !utf8.Valid(data) {
		m.Truncated = false
		m.Omitted = "binary"
		return
	}
	m.Text = scrubText(string(data))
}
