// Package web serves the embedded Cytoscape frontend over HTTP + WebSocket.
package web

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/pydevsg/sudiviz-go/internal/render"
	"github.com/pydevsg/sudiviz-go/internal/run"
	front "github.com/pydevsg/sudiviz-go/web"
)

// Config is the live-server configuration.
type Config struct {
	Profile         string
	Region          string
	VPCID           string
	ServiceTag      string
	RefreshInterval time.Duration
	Host            string
	Port            int
}

type cached struct {
	graph     render.Cytoscape
	diagnosis map[string]any
	err       string
	at        time.Time
}

// Server is the HTTP + WebSocket topology server.
type Server struct {
	cfg   Config
	mu    sync.Mutex
	cache map[string]*cached
	hub   *hub
}

// New builds a server with an empty per-region cache.
func New(cfg Config) *Server {
	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = 30 * time.Second
	}
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}
	if cfg.Port == 0 {
		cfg.Port = 8000
	}
	return &Server{cfg: cfg, cache: map[string]*cached{}, hub: newHub()}
}

func (c Config) opts(region string) run.Options {
	if region == "" {
		region = c.Region
	}
	return run.Options{Profile: c.Profile, Region: region, VPCID: c.VPCID, ServiceTag: c.ServiceTag}
}

func cacheKey(region, fallback string) string {
	if region != "" {
		return region
	}
	if fallback != "" {
		return fallback
	}
	return "default"
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	static, err := fs.Sub(front.StaticFS, "static")
	if err != nil {
		panic(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	mux.HandleFunc("/", s.index)
	mux.HandleFunc("/graph", s.graph)
	mux.HandleFunc("/diagnose", s.diagnose)
	mux.HandleFunc("/healthz", s.healthz)
	mux.HandleFunc("/ws", s.ws)
	return mux
}

// ListenAndServe starts the blocking HTTP server and a background refresh loop.
func (s *Server) ListenAndServe(ctx context.Context) error {
	go s.refreshLoop(ctx)
	addr := s.cfg.Host + ":" + strconv.Itoa(s.cfg.Port)
	srv := &http.Server{Addr: addr, Handler: s.Handler()}
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()
	return srv.ListenAndServe()
}

func (s *Server) refreshLoop(ctx context.Context) {
	s.refresh(ctx, s.cfg.Region)
	t := time.NewTicker(s.cfg.RefreshInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.refresh(ctx, s.cfg.Region)
		}
	}
}

func (s *Server) refresh(ctx context.Context, region string) *cached {
	key := cacheKey(region, s.cfg.Region)
	snap, err := run.Live(ctx, s.cfg.opts(region))
	c := &cached{at: time.Now().UTC()}
	if err != nil {
		c.err = err.Error()
	} else {
		c.graph = render.ExportCytoscape(snap.Graph)
		diag := snap.Diagnosis.ToMap()
		diag["region"] = snap.Graph.Region
		c.diagnosis = diag
	}
	s.mu.Lock()
	s.cache[key] = c
	s.mu.Unlock()
	if region == "" || region == s.cfg.Region {
		if c.err == "" {
			s.hub.broadcast(map[string]any{"type": "graph", "graph": c.graph})
			s.hub.broadcast(map[string]any{"type": "diagnosis", "diagnosis": c.diagnosis})
		}
	}
	return c
}

func (s *Server) get(ctx context.Context, region string) *cached {
	key := cacheKey(region, s.cfg.Region)
	s.mu.Lock()
	c := s.cache[key]
	s.mu.Unlock()
	if c != nil {
		return c
	}
	return s.refresh(ctx, region)
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	b, err := front.StaticFS.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}

func (s *Server) graph(w http.ResponseWriter, r *http.Request) {
	c := s.get(r.Context(), r.URL.Query().Get("region"))
	if c.err != "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": c.err})
		return
	}
	writeJSON(w, http.StatusOK, c.graph)
}

func (s *Server) diagnose(w http.ResponseWriter, r *http.Request) {
	c := s.get(r.Context(), r.URL.Query().Get("region"))
	if c.err != "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": c.err})
		return
	}
	writeJSON(w, http.StatusOK, c.diagnosis)
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	c := s.cache[cacheKey("", s.cfg.Region)]
	s.mu.Unlock()
	payload := map[string]any{"ok": true, "last_refresh": nil, "error": nil}
	if c != nil {
		payload["ok"] = c.err == ""
		payload["last_refresh"] = c.at.Format(time.RFC3339)
		if c.err != "" {
			payload["error"] = c.err
		}
	}
	writeJSON(w, http.StatusOK, payload)
}

var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

func (s *Server) ws(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.hub.add(conn)
	defer s.hub.remove(conn)

	c := s.get(r.Context(), s.cfg.Region)
	if c.err == "" {
		_ = conn.WriteJSON(map[string]any{"type": "graph", "graph": c.graph})
		_ = conn.WriteJSON(map[string]any{"type": "diagnosis", "diagnosis": c.diagnosis})
	}
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

type hub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
}

func newHub() *hub { return &hub{clients: map[*websocket.Conn]struct{}{}} }

func (h *hub) add(c *websocket.Conn) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *hub) remove(c *websocket.Conn) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	_ = c.Close()
}

func (h *hub) broadcast(payload map[string]any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if err := c.WriteJSON(payload); err != nil {
			_ = c.Close()
			delete(h.clients, c)
		}
	}
}
