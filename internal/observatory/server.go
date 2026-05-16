package observatory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"time"

	"github.com/afroash/5g-sim/pkg/obspub"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Server is the observatory HTTP API and static UI.
type Server struct {
	cfg      Config
	hub      *Hub
	poller   *Poller
	ues      *UEManager
	static   fs.FS
	started  time.Time
}

// NewServer creates an observatory HTTP server.
func NewServer(cfg Config, hub *Hub, poller *Poller, ues *UEManager, static fs.FS) *Server {
	return &Server{
		cfg:     cfg,
		hub:     hub,
		poller:  poller,
		ues:     ues,
		static:  static,
		started: time.Now(),
	}
}

// Handler returns the root http.Handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/events", s.handleEvents)
	mux.HandleFunc("/api/v1/messages", s.handleMessages)
	mux.HandleFunc("/api/v1/topology", s.handleTopology)
	mux.HandleFunc("/api/v1/ues", s.handleUEs)
	mux.HandleFunc("/api/v1/ues/", s.handleUEByID)
	mux.HandleFunc("/api/v1/status", s.handleStatus)
	mux.HandleFunc("/ws", s.handleWebSocket)

	if s.static != nil {
		mux.Handle("/", http.FileServer(http.FS(s.static)))
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "UI not embedded — run npm run build in web/observatory", http.StatusNotFound)
		})
	}
	return mux
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var ev obspub.Event
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if ev.ID == "" {
		ev.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if ev.TS.IsZero() {
		ev.TS = time.Now()
	}
	s.hub.Add(ev)
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := 100
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			limit = n
		}
	}
	writeJSON(w, s.hub.Recent(limit))
}

func (s *Server) handleTopology(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, s.poller.Last())
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	topo := s.poller.Last()
	writeJSON(w, map[string]interface{}{
		"uptime":   time.Since(s.started).Round(time.Second).String(),
		"started":  s.started,
		"messages": len(s.hub.Recent(s.cfg.EventBuffer)),
		"topology": topo,
	})
}

func (s *Server) handleUEs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		list, err := s.ues.List(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]interface{}{"ues": list})
	case http.MethodPost:
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		opts, err := ParseSpawnUEBody(raw)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		rec, err := s.ues.Spawn(ctx, opts)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, rec)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleUEByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Path[len("/api/v1/ues/"):]
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	if err := s.ues.Stop(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	_ = conn.WriteJSON(map[string]interface{}{
		"type":     "snapshot",
		"topology": s.poller.Last(),
		"messages": s.hub.Recent(100),
		"ues":      s.mustListUEs(r.Context()),
		"uptime":   time.Since(s.started).String(),
	})

	ch := make(chan obspub.Event, 32)
	s.hub.Subscribe(ch)
	defer s.hub.Unsubscribe(ch)

	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.WriteJSON(map[string]interface{}{"type": "event", "event": ev}); err != nil {
				return
			}
		case <-ticker.C:
			if err := conn.WriteJSON(map[string]interface{}{
				"type":     "topology",
				"topology": s.poller.Last(),
			}); err != nil {
				return
			}
		}
	}
}

func (s *Server) mustListUEs(ctx context.Context) []UERecord {
	list, err := s.ues.List(ctx)
	if err != nil {
		return nil
	}
	return list
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// Start runs the HTTP server until ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	srv := &http.Server{
		Addr:    s.cfg.ListenAddr(),
		Handler: s.Handler(),
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	fmt.Printf("[observatory] listening on http://%s\n", s.cfg.ListenAddr())
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
