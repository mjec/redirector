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
	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
)

var config *configuration.Config = &configuration.Config{}
var metrics *Metrics = &Metrics{
	InFlightRequests: prometheus.NewGauge(prometheus.GaugeOpts{Name: "in_flight_requests", Help: "A gauge of requests currently being served"}),
	TotalRequests:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "requests_total", Help: "A counter for requests"}, []string{"domain", "rule_index", "method", "code"}),
	HandlerDuration:  prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "request_duration_seconds", Help: "A histogram of latencies for requests"}, []string{"domain", "rule_index", "method", "code"}),
}

func TestHandlerDefaultResponse421(t *testing.T) {
	resetConfigAndMetrics()
	config.Domains = map[string]configuration.Domain{}
	req := httptest.NewRequest("", "http://example.com", nil)
	rr := httptest.NewRecorder()

	MakeHandler(config, metrics)(rr, req)

	if rr.Code != config.DefaultResponse.Code {
		t.Errorf("Expected status code %d, but got %d", config.DefaultResponse.Code, rr.Code)
	}
	expectedBody := config.DefaultResponse.Body
	if rr.Body.String() != expectedBody {
		t.Errorf("Expected body '%s', but got '%s'", expectedBody, rr.Body.String())
	}
	expectCounterValue(t, metrics.TotalRequests, prometheus.Labels{"domain": "default", "rule_index": "default", "method": "GET", "code": "421"}, 1)
}

func TestHandlerDefaultResponseNonHijackable(t *testing.T) {
	resetConfigAndMetrics()
	config.Domains = map[string]configuration.Domain{}
	config.DefaultResponse.Code = 0

	req := httptest.NewRequest("", "http://example.com", nil)
	rr := httptest.NewRecorder()

	MakeHandler(config, metrics)(rr, req)
	// We expect a 500 here because httptest.NewRecorder is not Hijackable
	if rr.Code != 500 {
		t.Errorf("Expected status code %d, but got %d", 500, rr.Code)
	}
	// We expect this to record with code 0, because it's only not instantly closing the connection because rr is not Hijackable
	expectCounterValue(t, metrics.TotalRequests, prometheus.Labels{"domain": "default", "rule_index": "default", "method": "GET", "code": "0"}, 1)
}

func TestHandlerDefaultResponseCloseConnection(t *testing.T) {
	resetConfigAndMetrics()
	config.Domains = map[string]configuration.Domain{}
	config.DefaultResponse.Code = 0

	req := httptest.NewRequest("", "http://example.com", nil)
	rr := &hijackableResponse{
		httptest.NewRecorder(),
		false,
	}

	MakeHandler(config, metrics)(rr, req)

	if !rr.wasClosed {
		t.Errorf("Expected hijacked connection to be closed: %v", rr)
	}
	expectCounterValue(t, metrics.TotalRequests, prometheus.Labels{"domain": "default", "rule_index": "default", "method": "GET", "code": "0"}, 1)
}

func TestHandlerSimpleMatching(t *testing.T) {
	resetConfigAndMetrics()
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

	MakeHandler(config, metrics)(rr, req)

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
	expectCounterValue(t, metrics.TotalRequests, prometheus.Labels{"domain": "example.com", "rule_index": "0", "method": "GET", "code": "301"}, 1)
}

func TestHandlerDomainSpecificDefaultResponse(t *testing.T) {
	resetConfigAndMetrics()
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

	MakeHandler(config, metrics)(rr, req)

	if rr.Code != http.StatusMisdirectedRequest {
		// Nothing matches, go to default response
		t.Errorf("Expected status code %d, but got %d", http.StatusMisdirectedRequest, rr.Code)
	}
	expectCounterValue(t, metrics.TotalRequests, prometheus.Labels{"domain": "default", "rule_index": "default", "method": "GET", "code": "421"}, 1)

	req = httptest.NewRequest("", "http://mjec.example.com/welcome", nil)
	rr = httptest.NewRecorder()

	MakeHandler(config, metrics)(rr, req)

	if rr.Code != http.StatusGone {
		// Nothing matches, go to default response for this domain
		t.Errorf("Expected status code %d, but got %d", http.StatusGone, rr.Code)
	}
	expectCounterValue(t, metrics.TotalRequests, prometheus.Labels{"domain": "mjec.example.com", "rule_index": "default", "method": "GET", "code": "410"}, 1)

	req = httptest.NewRequest("", "http://mjec.example.com/only-this", nil)
	rr = httptest.NewRecorder()

	MakeHandler(config, metrics)(rr, req)

	if rr.Code != http.StatusMovedPermanently {
		// Nothing matches, go to default response for this domain
		t.Errorf("Expected status code %d, but got %d", http.StatusMovedPermanently, rr.Code)
	}
	if loc, err := rr.Result().Location(); err == nil {
		if loc.String() != "https://www.example.com/only-there" {
			t.Errorf("Expected URL %s, but got %s", "https://www.example.com/only-there", loc)
		}
	} else {
		t.Errorf("Expected Location header but none found: %v", err)
	}
	expectCounterValue(t, metrics.TotalRequests, prometheus.Labels{"domain": "mjec.example.com", "rule_index": "0", "method": "GET", "code": "301"}, 1)
}

func TestHandlerMultipleRules(t *testing.T) {
	resetConfigAndMetrics()
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

	MakeHandler(config, metrics)(rr, req)

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
	expectCounterValue(t, metrics.TotalRequests, prometheus.Labels{"domain": "example.com", "rule_index": "1", "method": "GET", "code": "301"}, 1)

	req = httptest.NewRequest("", "http://example.com/a/farewell", nil)
	rr = httptest.NewRecorder()

	MakeHandler(config, metrics)(rr, req)

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
	expectCounterValue(t, metrics.TotalRequests, prometheus.Labels{"domain": "example.com", "rule_index": "0", "method": "GET", "code": "303"}, 1)
}

func TestHandlerSubdomainMatching(t *testing.T) {
	resetConfigAndMetrics()
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

	MakeHandler(config, metrics)(rr, req)

	if rr.Code != http.StatusMisdirectedRequest {
		// We should not see a match on this domain
		t.Errorf("Expected status code %d, but got %d", http.StatusMisdirectedRequest, rr.Code)
	}
	expectCounterValue(t, metrics.TotalRequests, prometheus.Labels{"domain": "default", "rule_index": "default", "method": "GET", "code": "421"}, 1)

	config.Domains["example.com"] = configuration.Domain{
		MatchSubdomains: true,
		RewriteRules:    config.Domains["example.com"].RewriteRules,
		DefaultResponse: config.Domains["example.com"].DefaultResponse,
	}

	// Verify that we get a match on the subdomain `www`
	req = httptest.NewRequest("", "http://www.example.com/welcome", nil)
	rr = httptest.NewRecorder()

	MakeHandler(config, metrics)(rr, req)

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
	expectCounterValue(t, metrics.TotalRequests, prometheus.Labels{"domain": "example.com", "rule_index": "0", "method": "GET", "code": "301"}, 1)

	req = httptest.NewRequest("", "http://www.not-example.com/welcome", nil)
	rr = httptest.NewRecorder()

	MakeHandler(config, metrics)(rr, req)

	if rr.Code != http.StatusMisdirectedRequest {
		// This should NOT be a match
		t.Errorf("Expected status code %d, but got %d", http.StatusMisdirectedRequest, rr.Code)
	}
	// Second request that hits default, so counter should be at 2
	expectCounterValue(t, metrics.TotalRequests, prometheus.Labels{"domain": "default", "rule_index": "default", "method": "GET", "code": "421"}, 2)
}

func TestHandlerLogging(t *testing.T) {
	previousLogger := slog.Default()
	defer slog.SetDefault(previousLogger)
	loggerSpy := &logSpy{}
	newLogger := slog.New(loggerSpy)
	slog.SetDefault(newLogger)

	resetConfigAndMetrics()
	config.DefaultResponse.LogHits = false

	req := httptest.NewRequest("", "http://example.com/welcome", nil)
	rr := httptest.NewRecorder()

	MakeHandler(config, metrics)(rr, req)

	if loggerSpy.lineCounter != 0 {
		t.Errorf("Expected no lines logged, but got %d lines logged (%v)", loggerSpy.lineCounter, loggerSpy.lines)
	}

	loggerSpy.reset()
	resetConfigAndMetrics()
	config.DefaultResponse.LogHits = true

	rr = httptest.NewRecorder()

	MakeHandler(config, metrics)(rr, req)

	if loggerSpy.lineCounter != 1 {
		t.Errorf("Expected 1 line logged, but got %d lines logged (%v)", loggerSpy.lineCounter, loggerSpy.lines)
	}

	loggerSpy.reset()
	resetConfigAndMetrics()
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
	MakeHandler(config, metrics)(rr, req)

	if loggerSpy.lineCounter != 1 {
		t.Errorf("Expected 1 line logged, but got %d lines logged (%v)", loggerSpy.lineCounter, loggerSpy.lines)
	}

	loggerSpy.reset()

	req = httptest.NewRequest("", "http://mjec.example.com/welcome", nil)
	rr = httptest.NewRecorder()
	MakeHandler(config, metrics)(rr, req)

	if loggerSpy.lineCounter != 0 {
		t.Errorf("Expected 0 lines logged, but got %d lines logged (%v)", loggerSpy.lineCounter, loggerSpy.lines)
	}

	loggerSpy.reset()
	resetConfigAndMetrics()
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
	MakeHandler(config, metrics)(rr, req)

	if loggerSpy.lineCounter != 0 {
		t.Errorf("Expected 0 lines logged, but got %d lines logged (%v)", loggerSpy.lineCounter, loggerSpy.lines)
	}

	loggerSpy.reset()

	req = httptest.NewRequest("", "http://mjec.example.com/welcome", nil)
	rr = httptest.NewRecorder()
	MakeHandler(config, metrics)(rr, req)

	if loggerSpy.lineCounter != 1 {
		t.Errorf("Expected 1 line logged, but got %d lines logged (%v)", loggerSpy.lineCounter, loggerSpy.lines)
	}
}

func TestHandlerClientIPFromHeader(t *testing.T) {
	const CONNECTION_REMOTE_ADDR string = "127.0.0.1"
	const REAL_IP string = "127.0.0.2"
	const IP_HEADER string = "X-Real-Ip"

	previousLogger := slog.Default()
	defer slog.SetDefault(previousLogger)
	loggerSpy := &logSpy{}
	newLogger := slog.New(loggerSpy)
	slog.SetDefault(newLogger)

	resetConfigAndMetrics()
	config.DefaultResponse.LogHits = true
	config.ClientIPHeader = ""

	req := httptest.NewRequest("", "http://example.com/welcome", nil)
	req.RemoteAddr = CONNECTION_REMOTE_ADDR
	req.Header.Set(IP_HEADER, REAL_IP)
	rr := httptest.NewRecorder()

	MakeHandler(config, metrics)(rr, req)

	if loggerSpy.lineCounter != 1 {
		t.Errorf("Expected one line logged, but got %d lines logged (%v)", loggerSpy.lineCounter, loggerSpy.lines)
	}

	loggerSpy.lines[0].Attrs(func(a slog.Attr) bool {
		if a.Key == "remote_addr" {
			if a.Value.String() != CONNECTION_REMOTE_ADDR {
				t.Errorf("Expected remote_addr to be %s (header ignored), but got %s", CONNECTION_REMOTE_ADDR, a.Value)
			}
			return true
		}
		return false
	})

	loggerSpy.reset()
	resetConfigAndMetrics()

	config.DefaultResponse.LogHits = true
	config.ClientIPHeader = IP_HEADER

	req = httptest.NewRequest("", "http://example.com/welcome", nil)
	req.RemoteAddr = CONNECTION_REMOTE_ADDR
	rr = httptest.NewRecorder()

	MakeHandler(config, metrics)(rr, req)

	if loggerSpy.lineCounter != 1 {
		t.Errorf("Expected one line logged, but got %d lines logged (%v)", loggerSpy.lineCounter, loggerSpy.lines)
	}

	loggerSpy.lines[0].Attrs(func(a slog.Attr) bool {
		if a.Key == "remote_addr" {
			if a.Value.String() != CONNECTION_REMOTE_ADDR {
				t.Errorf("Expected remote_addr to be %s (header configured but not set), but got %s", CONNECTION_REMOTE_ADDR, a.Value)
			}
			return true
		}
		return false
	})

	loggerSpy.reset()
	resetConfigAndMetrics()

	config.DefaultResponse.LogHits = true
	config.ClientIPHeader = IP_HEADER

	req = httptest.NewRequest("", "http://example.com/welcome", nil)
	req.RemoteAddr = CONNECTION_REMOTE_ADDR
	req.Header.Set(IP_HEADER, REAL_IP)
	rr = httptest.NewRecorder()

	MakeHandler(config, metrics)(rr, req)

	if loggerSpy.lineCounter != 1 {
		t.Errorf("Expected one line logged, but got %d lines logged (%v)", loggerSpy.lineCounter, loggerSpy.lines)
	}

	loggerSpy.lines[0].Attrs(func(a slog.Attr) bool {
		if a.Key == "remote_addr" {
			if a.Value.String() != REAL_IP {
				t.Errorf("Expected remote_addr to be %s (header used), but got %s", REAL_IP, a.Value)
			}
			return true
		}
		return false
	})

	loggerSpy.reset()
	resetConfigAndMetrics()
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

func resetConfigAndMetrics() {
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

	metrics.InFlightRequests.Set(0)
	metrics.TotalRequests.Reset()
	metrics.HandlerDuration.Reset()
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

func expectCounterValue(t *testing.T, counterVec *prometheus.CounterVec, labels prometheus.Labels, expected float64) {
	metric := &io_prometheus_client.Metric{}
	if request_count, err := counterVec.GetMetricWith(labels); err != nil {
		t.Errorf("Expected metric to exist, but got error: %v", err)
	} else if err := request_count.Write(metric); err != nil {
		t.Errorf("Expected metric to be written, but got error: %v", err)
	} else if metric.GetCounter().GetValue() != expected {
		t.Errorf("Expected metric value %f, but got %f", expected, metric.GetCounter().GetValue())
	}
}
