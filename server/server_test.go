package server

import (
	"bufio"
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/mjec/redirector/configuration"
)

var config *configuration.Config = &configuration.Config{}

func TestHandlerDefaultResponse421(t *testing.T) {
	resetConfig()
	config.Domains = map[string]configuration.Domain{}
	req := httptest.NewRequest("", "http://example.com", nil)
	rr := httptest.NewRecorder()

	MakeHandler(config)(rr, req)
	if rr.Code != config.DefaultResponse.Code {
		t.Errorf("Expected status code %d, but got %d", config.DefaultResponse.Code, rr.Code)
	}
	expectedBody := config.DefaultResponse.Body
	if rr.Body.String() != expectedBody {
		t.Errorf("Expected body '%s', but got '%s'", expectedBody, rr.Body.String())
	}
}

func TestHandlerDefaultResponse500(t *testing.T) {
	resetConfig()
	config.Domains = map[string]configuration.Domain{}
	config.DefaultResponse.Code = 0

	req := httptest.NewRequest("", "http://example.com", nil)
	rr := httptest.NewRecorder()

	MakeHandler(config)(rr, req)
	// We expect a 500 here because httptest.NewRecorder is not Hijackable
	if rr.Code != 500 {
		t.Errorf("Expected status code %d, but got %d", 500, rr.Code)
	}
}

func TestHandlerDefaultResponseCloseConnection(t *testing.T) {
	resetConfig()
	config.Domains = map[string]configuration.Domain{}
	config.DefaultResponse.Code = 0

	req := httptest.NewRequest("", "http://example.com", nil)
	rr := &hijackableResponse{
		httptest.NewRecorder(),
		false,
	}

	MakeHandler(config)(rr, req)
	if !rr.wasClosed {
		t.Errorf("Expected hijacked connection to be closed: %v", rr)
	}
}

func TestHandlerSimpleMatching(t *testing.T) {
	resetConfig()
	config.Domains = map[string]configuration.Domain{
		"mjec.example.com": {},
		"example.com": {
			RewriteRules: []configuration.Rule{
				{
					Regexp:      regexp.MustCompile("(.*)"),
					Replacement: "https://www.example.com$1",
					Code:        http.StatusMovedPermanently,
				},
			},
		},
	}

	req := httptest.NewRequest("", "http://example.com/welcome", nil)
	rr := httptest.NewRecorder()

	MakeHandler(config)(rr, req)

	if loc, err := rr.Result().Location(); err == nil {
		if loc.String() != "https://www.example.com/welcome" {
			t.Errorf("Expected URL %s, but got %s", "https://www.example.com/welcome", loc)
		}
	} else {
		t.Errorf("Expected Location header but none found: %v", err)
	}
	if rr.Code != http.StatusMovedPermanently {
		t.Errorf("Expected status code %d, but got %d", http.StatusMovedPermanently, rr.Code)
	}
}

func TestHandlerDomainSpecificDefaultResponse(t *testing.T) {
	resetConfig()
	config.Domains = map[string]configuration.Domain{
		"example.com": {},
		"mjec.example.com": {
			RewriteRules: []configuration.Rule{
				{
					Regexp:      regexp.MustCompile("/only-this"),
					Replacement: "https://www.example.com/only-there",
					Code:        http.StatusMovedPermanently,
				},
			},
			DefaultResponse: &configuration.DefaultResponse{
				Code: http.StatusGone,
				Body: "Gone.\n",
			},
		},
	}

	req := httptest.NewRequest("", "http://example.com/welcome", nil)
	rr := httptest.NewRecorder()

	MakeHandler(config)(rr, req)
	if rr.Code != http.StatusMisdirectedRequest {
		// Nothing matches, go to default response
		t.Errorf("Expected status code %d, but got %d", http.StatusMisdirectedRequest, rr.Code)
	}

	req = httptest.NewRequest("", "http://mjec.example.com/welcome", nil)
	rr = httptest.NewRecorder()

	MakeHandler(config)(rr, req)
	if rr.Code != http.StatusGone {
		// Nothing matches, go to default response for this domain
		t.Errorf("Expected status code %d, but got %d", http.StatusGone, rr.Code)
	}

	req = httptest.NewRequest("", "http://mjec.example.com/only-this", nil)
	rr = httptest.NewRecorder()

	MakeHandler(config)(rr, req)
	if rr.Code != http.StatusMovedPermanently {
		// Nothing matches, go to default response for this domain
		t.Errorf("Expected status code %d, but got %d", http.StatusGone, rr.Code)
	}
	if loc, err := rr.Result().Location(); err == nil {
		if loc.String() != "https://www.example.com/only-there" {
			t.Errorf("Expected URL %s, but got %s", "https://www.example.com/only-there", loc)
		}
	} else {
		t.Errorf("Expected Location header but none found: %v", err)
	}
}

func TestHandlerMultipleRules(t *testing.T) {
	resetConfig()
	config.Domains = map[string]configuration.Domain{
		"mjec.example.com": {},
		"example.com": {
			RewriteRules: []configuration.Rule{
				{
					Regexp:      regexp.MustCompile("/a(/.*)"),
					Replacement: "https://a.example.com$1",
					Code:        http.StatusSeeOther,
				},
				{
					Regexp:      regexp.MustCompile("(.*)"),
					Replacement: "https://www.example.com$1",
					Code:        http.StatusMovedPermanently,
				},
			},
		},
	}

	req := httptest.NewRequest("", "http://example.com/welcome", nil)
	rr := httptest.NewRecorder()

	MakeHandler(config)(rr, req)

	if loc, err := rr.Result().Location(); err == nil {
		if loc.String() != "https://www.example.com/welcome" {
			t.Errorf("Expected URL %s, but got %s", "https://www.example.com/welcome", loc)
		}
	} else {
		t.Errorf("Expected Location header but none found: %v", err)
	}
	if rr.Code != http.StatusMovedPermanently {
		t.Errorf("Expected status code %d, but got %d", http.StatusMovedPermanently, rr.Code)
	}

	req = httptest.NewRequest("", "http://example.com/a/farewell", nil)
	rr = httptest.NewRecorder()

	MakeHandler(config)(rr, req)

	if loc, err := rr.Result().Location(); err == nil {
		if loc.String() != "https://a.example.com/farewell" {
			t.Errorf("Expected URL %s, but got %s", "https://a.example.com/farewell", loc)
		}
	} else {
		t.Errorf("Expected Location header but none found: %v", err)
	}
	if rr.Code != http.StatusSeeOther {
		t.Errorf("Expected status code %d, but got %d", http.StatusSeeOther, rr.Code)
	}
}

func TestHandlerSubdomainMatching(t *testing.T) {
	resetConfig()
	config.Domains = map[string]configuration.Domain{
		"example.com": {
			RewriteRules: []configuration.Rule{
				{
					Regexp:      regexp.MustCompile("(.*)"),
					Replacement: "https://www.example.com$1",
					Code:        http.StatusMovedPermanently,
				},
			},
			DefaultResponse: &configuration.DefaultResponse{
				Code: http.StatusOK,
				Headers: map[string]string{
					"Connection":   "close",
					"Content-Type": "text/plain",
				},
				Body: "Nothing here.\n",
			},
		},
	}

	req := httptest.NewRequest("", "http://www.example.com/welcome", nil)
	rr := httptest.NewRecorder()
	MakeHandler(config)(rr, req)

	if rr.Code != http.StatusMisdirectedRequest {
		// We should not see a match on this domain
		t.Errorf("Expected status code %d, but got %d", http.StatusMisdirectedRequest, rr.Code)
	}

	config.Domains["example.com"] = configuration.Domain{
		MatchSubdomains: true,
		RewriteRules:    config.Domains["example.com"].RewriteRules,
		DefaultResponse: config.Domains["example.com"].DefaultResponse,
	}

	// Verify that we get a match on the subdomain `www`
	req = httptest.NewRequest("", "http://www.example.com/welcome", nil)
	rr = httptest.NewRecorder()
	MakeHandler(config)(rr, req)

	if rr.Code != http.StatusMovedPermanently {
		t.Errorf("Expected status code %d, but got %d", http.StatusMovedPermanently, rr.Code)
	}
	if loc, err := rr.Result().Location(); err == nil {
		if loc.String() != "https://www.example.com/welcome" {
			t.Errorf("Expected URL %s, but got %s", "https://www.example.com/welcome", loc)
		}
	} else {
		t.Errorf("Expected Location header but none found: %v", err)
	}

	req = httptest.NewRequest("", "http://www.not-example.com/welcome", nil)
	rr = httptest.NewRecorder()
	MakeHandler(config)(rr, req)
	if rr.Code != http.StatusMisdirectedRequest {
		// This should NOT be a match
		t.Errorf("Expected status code %d, but got %d", http.StatusMisdirectedRequest, rr.Code)
	}
}

func TestHandlerLogging(t *testing.T) {
	previousLogger := slog.Default()
	defer slog.SetDefault(previousLogger)
	loggerSpy := &logSpy{}
	newLogger := slog.New(loggerSpy)
	slog.SetDefault(newLogger)

	resetConfig()
	config.DefaultResponse.LogHits = false

	req := httptest.NewRequest("", "http://example.com/welcome", nil)
	rr := httptest.NewRecorder()

	MakeHandler(config)(rr, req)

	if loggerSpy.lineCounter != 0 {
		t.Errorf("Expected no lines logged, but got %d lines logged (%v)", loggerSpy.lineCounter, loggerSpy.lines)
	}

	loggerSpy.reset()
	resetConfig()
	config.DefaultResponse.LogHits = true

	rr = httptest.NewRecorder()

	MakeHandler(config)(rr, req)

	if loggerSpy.lineCounter != 1 {
		t.Errorf("Expected 1 line logged, but got %d lines logged (%v)", loggerSpy.lineCounter, loggerSpy.lines)
	}

	loggerSpy.reset()
	resetConfig()
	config.Domains = map[string]configuration.Domain{
		"mjec.example.com": {
			RewriteRules: []configuration.Rule{
				{
					Regexp:      regexp.MustCompile("(.*)"),
					Replacement: "https://www.example.com$1",
					Code:        http.StatusMovedPermanently,
					LogHits:     false,
				},
			},
		},
	}
	config.DefaultResponse.LogHits = true

	req = httptest.NewRequest("", "http://example.com/welcome", nil)
	rr = httptest.NewRecorder()
	MakeHandler(config)(rr, req)

	if loggerSpy.lineCounter != 1 {
		t.Errorf("Expected 1 line logged, but got %d lines logged (%v)", loggerSpy.lineCounter, loggerSpy.lines)
	}

	loggerSpy.reset()

	req = httptest.NewRequest("", "http://mjec.example.com/welcome", nil)
	rr = httptest.NewRecorder()
	MakeHandler(config)(rr, req)

	if loggerSpy.lineCounter != 0 {
		t.Errorf("Expected 0 lines logged, but got %d lines logged (%v)", loggerSpy.lineCounter, loggerSpy.lines)
	}

	loggerSpy.reset()
	resetConfig()
	config.Domains = map[string]configuration.Domain{
		"mjec.example.com": {
			RewriteRules: []configuration.Rule{
				{
					Regexp:      regexp.MustCompile("(.*)"),
					Replacement: "https://www.example.com$1",
					Code:        http.StatusMovedPermanently,
					LogHits:     true,
				},
			},
		},
	}
	config.DefaultResponse.LogHits = false

	req = httptest.NewRequest("", "http://example.com/welcome", nil)
	rr = httptest.NewRecorder()
	MakeHandler(config)(rr, req)

	if loggerSpy.lineCounter != 0 {
		t.Errorf("Expected 0 lines logged, but got %d lines logged (%v)", loggerSpy.lineCounter, loggerSpy.lines)
	}

	loggerSpy.reset()

	req = httptest.NewRequest("", "http://mjec.example.com/welcome", nil)
	rr = httptest.NewRecorder()
	MakeHandler(config)(rr, req)

	if loggerSpy.lineCounter != 1 {
		t.Errorf("Expected 1 line logged, but got %d lines logged (%v)", loggerSpy.lineCounter, loggerSpy.lines)
	}
}

func TestHandlerPanicsWithoutConfig(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Errorf("Expected handler to panic being called without config in context")
		}
	}()

	req := httptest.NewRequest("", "http://example.com/welcome", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)
}

func resetConfig() {
	config = &configuration.Config{
		DefaultResponse: &configuration.DefaultResponse{
			Code: http.StatusMisdirectedRequest,
			Headers: map[string]string{
				"Connection":   "close",
				"Content-Type": "text/plain",
			},
			Body: "421 Misdirected Request\n\nTarget URI does not match an origin for which the server has been configured.\n",
		},
		Domains: map[string]configuration.Domain{},
	}
}

type hijackableResponse struct {
	recorder  *httptest.ResponseRecorder
	wasClosed bool
}

func (r *hijackableResponse) Header() http.Header {
	return r.recorder.Header()
}

func (r *hijackableResponse) Write(b []byte) (int, error) {
	return r.recorder.Write(b)
}

func (r *hijackableResponse) WriteHeader(statusCode int) {
	r.recorder.WriteHeader(statusCode)
}

func (r *hijackableResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	buf := bytes.NewBuffer([]byte(""))
	return &connectionSpy{r}, bufio.NewReadWriter(bufio.NewReader(buf), bufio.NewWriter(buf)), nil
}

type connectionSpy struct {
	response *hijackableResponse
}

func (c *connectionSpy) Read(b []byte) (n int, err error) {
	panic("Not implemented")
}

func (c *connectionSpy) Write(b []byte) (n int, err error) {
	panic("Not implemented")
}

func (c *connectionSpy) Close() error {
	c.response.wasClosed = true
	return nil
}

func (c *connectionSpy) LocalAddr() net.Addr {
	panic("Not implemented")
}

func (c *connectionSpy) RemoteAddr() net.Addr {
	panic("Not implemented")
}

func (c *connectionSpy) SetDeadline(t time.Time) error {
	panic("Not implemented")
}

func (c *connectionSpy) SetReadDeadline(t time.Time) error {
	panic("Not implemented")
}

func (c *connectionSpy) SetWriteDeadline(t time.Time) error {
	panic("Not implemented")
}

type logSpy struct {
	lineCounter int
	lines       []slog.Record
}

func (h *logSpy) Handle(ctx context.Context, record slog.Record) error {
	h.lineCounter++
	h.lines = append(h.lines, record)
	return nil
}

func (h *logSpy) Enabled(ctx context.Context, lvl slog.Level) bool {
	return true
}

func (h *logSpy) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *logSpy) WithGroup(name string) slog.Handler {
	return h
}

func (h *logSpy) reset() {
	h.lineCounter = 0
	h.lines = []slog.Record{}
}
