package portalhttp

import "net/http"

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (s *server) ready(w http.ResponseWriter, r *http.Request) {
	snapshot := s.Store.Snapshot(s.Now())
	if !snapshot.Initialized || snapshot.Expired {
		http.Error(w, "catalog unavailable", http.StatusServiceUnavailable)
		return
	}
	s.health(w, r)
}
