// Package router is the strangler layer. It sits in front of both systems
// and decides per route who answers.
package router

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/a-berahman/strangler-go/internal/compare"
)

// We keep at most this much of a body for comparing. Bigger responses are
// still served, we just don't compare them.
const maxBody = 1 << 20

type Router struct {
	cfg     Config
	log     *slog.Logger
	legacy  *httputil.ReverseProxy
	next    *httputil.ReverseProxy
	newURL  *url.URL
	client  *http.Client
	slots   chan struct{}
	pending sync.WaitGroup
}

func New(cfg Config, log *slog.Logger) (*Router, error) {
	if err := cfg.check(); err != nil {
		return nil, err
	}
	legacyURL, err := url.Parse(cfg.Legacy)
	if err != nil {
		return nil, fmt.Errorf("legacy url: %w", err)
	}
	newURL, err := url.Parse(cfg.New)
	if err != nil {
		return nil, fmt.Errorf("new url: %w", err)
	}
	return &Router{
		cfg:    cfg,
		log:    log,
		legacy: proxy(legacyURL, "legacy"),
		next:   proxy(newURL, "new"),
		newURL: newURL,
		// No client timeout here, every shadow request has its own deadline.
		client: &http.Client{},
		slots:  make(chan struct{}, cfg.MaxShadow),
	}, nil
}

func proxy(target *url.URL, name string) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.SetXForwarded()
		},
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Set("X-Served-By", name)
			return nil
		},
	}
}

func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch rt.mode(r.URL.Path) {
	case ModeNew:
		rt.next.ServeHTTP(w, r)
	case ModeShadow:
		// Only safe methods. Shadowing a POST means the order is created
		// twice, and you find out from the customer.
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			rt.shadow(w, r)
			return
		}
		rt.legacy.ServeHTTP(w, r)
	default:
		rt.legacy.ServeHTTP(w, r)
	}
}

// mode finds the longest matching prefix. /customers matches /customers and
// /customers/c-1 but not /customersfoo.
func (rt *Router) mode(path string) Mode {
	best, mode := -1, ModeLegacy
	for _, r := range rt.cfg.Routes {
		p := strings.TrimSuffix(r.Prefix, "/")
		if (path == p || strings.HasPrefix(path, p+"/")) && len(p) > best {
			best, mode = len(p), r.Mode
		}
	}
	return mode
}

// Wait blocks until all background shadow calls are done. Call it after
// the server is shut down, and in tests.
func (rt *Router) Wait() { rt.pending.Wait() }

type result struct {
	status    int
	body      []byte
	truncated bool
}

func (rt *Router) shadow(w http.ResponseWriter, r *http.Request) {
	select {
	case rt.slots <- struct{}{}:
	default:
		// Too many shadow calls already. The real call matters more.
		rt.log.Debug("shadow skipped, no free slot", "path", r.URL.Path)
		rt.legacy.ServeHTTP(w, r)
		return
	}

	// WithoutCancel because the client may hang up before we compare, and
	// the timeout so the goroutine always ends, even if the new service hangs.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()),
		time.Duration(rt.cfg.ShadowTimeout))
	sreq := r.Clone(ctx)
	sreq.URL.Scheme, sreq.URL.Host = rt.newURL.Scheme, rt.newURL.Host
	sreq.Host, sreq.RequestURI = "", ""
	sreq.Body = http.NoBody
	sreq.Header.Del("Accept-Encoding") // let the transport deal with gzip
	sreq.Header.Set("X-Shadow", "1")

	// Buffered, so the send below never blocks, even if the shadow
	// goroutine already gave up.
	legacyDone := make(chan result, 1)
	rt.pending.Add(1)
	go func() {
		defer rt.pending.Done()
		defer func() { <-rt.slots }()
		defer cancel()
		rt.runShadow(ctx, sreq, legacyDone)
	}()

	rec := &recorder{ResponseWriter: w, status: http.StatusOK}
	defer func() { legacyDone <- result{rec.status, rec.buf.Bytes(), rec.truncated} }()
	rt.legacy.ServeHTTP(rec, r)
}

func (rt *Router) runShadow(ctx context.Context, req *http.Request, legacyDone <-chan result) {
	log := rt.log.With("method", req.Method, "path", req.URL.RequestURI())
	resp, err := rt.client.Do(req)
	if err != nil {
		log.Warn("shadow call failed", "err", err)
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	resp.Body.Close()
	if err != nil {
		log.Warn("shadow body read failed", "err", err)
		return
	}

	var old result
	select {
	case old = <-legacyDone:
	case <-ctx.Done():
		log.Warn("legacy was slower than the shadow timeout, not compared")
		return
	}
	if old.truncated || len(body) > maxBody {
		log.Debug("body too big, not compared")
		return
	}

	var diffs []compare.Diff
	if old.status != resp.StatusCode {
		diffs = append(diffs, compare.Diff{Path: "status", Kind: compare.ValueChanged,
			Old: strconv.Itoa(old.status), New: strconv.Itoa(resp.StatusCode)})
	}
	if len(old.body) > 0 || len(body) > 0 {
		d, err := compare.JSON(old.body, body, compare.Options{Ignore: rt.cfg.Ignore})
		if err != nil {
			// Not JSON, so the only fair check is byte by byte.
			if !bytes.Equal(old.body, body) {
				diffs = append(diffs, compare.Diff{Path: "body", Kind: compare.ValueChanged})
			}
		}
		diffs = append(diffs, d...)
	}

	if len(diffs) == 0 {
		log.Debug("shadow match")
		return
	}
	details := make([]string, len(diffs))
	for i, d := range diffs {
		details[i] = d.String()
	}
	log.Warn("shadow mismatch", "count", len(diffs), "diffs", details)
}

// recorder passes the legacy response to the client and keeps a copy.
type recorder struct {
	http.ResponseWriter
	status    int
	buf       bytes.Buffer
	truncated bool
}

func (r *recorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(p []byte) (int, error) {
	if room := maxBody - r.buf.Len(); room >= len(p) {
		r.buf.Write(p)
	} else {
		r.truncated = true
	}
	return r.ResponseWriter.Write(p)
}

// Unwrap lets http.ResponseController (used by ReverseProxy) reach Flush.
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
