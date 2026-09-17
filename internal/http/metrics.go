package portalhttp

import "github.com/prometheus/client_golang/prometheus"

type metrics struct {
	server                                                    *server
	age, items, initialized, invalid, state, reconnect, watch *prometheus.Desc
	login, denied                                             *prometheus.CounterVec
}

func newMetrics(s *server) *metrics {
	desc := func(name, help string, labels ...string) *prometheus.Desc {
		return prometheus.NewDesc("portal_"+name, help, labels, nil)
	}
	m := &metrics{server: s,
		age:         desc("catalog_age_seconds", "Age of the latest successfully rebuilt catalog; zero before initialization."),
		items:       desc("catalog_items", "Current valid catalog items, across all access levels."),
		initialized: desc("catalog_initialized", "Whether the initial Kubernetes list has succeeded."),
		invalid:     desc("invalid_publications", "Current invalid publications requiring remediation."),
		state:       desc("catalog_state", "Current catalog freshness state.", "state"),
		reconnect:   desc("watch_reconnect_duration_seconds", "Elapsed time of the current watch reconnection attempt sequence."),
		watch:       desc("watch_state", "Current Kubernetes watcher state.", "state"),
		login:       prometheus.NewCounterVec(prometheus.CounterOpts{Name: "portal_login_total", Help: "Login outcomes."}, []string{"outcome"}),
		denied:      prometheus.NewCounterVec(prometheus.CounterOpts{Name: "portal_authorization_denials_total", Help: "Denied admin and logout requests."}, []string{"route"}),
	}
	for _, outcome := range []string{"started", "success", "failed", "throttled", "unavailable"} {
		m.login.WithLabelValues(outcome)
	}
	for _, route := range []string{"admin", "logout"} {
		m.denied.WithLabelValues(route)
	}
	return m
}

func (m *metrics) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{m.age, m.items, m.initialized, m.invalid, m.state, m.reconnect, m.watch} {
		ch <- desc
	}
	m.login.Describe(ch)
	m.denied.Describe(ch)
}

func (m *metrics) Collect(ch chan<- prometheus.Metric) {
	now := m.server.Now()
	snapshot := m.server.Store.Snapshot(now)
	age, initialized := 0.0, 0.0
	state := "uninitialized"
	if snapshot.Initialized {
		initialized = 1
		age = max(0, now.Sub(snapshot.LastSuccess).Seconds())
		state = "fresh"
		if snapshot.Stale {
			state = "stale"
		}
		if snapshot.Expired {
			state = "expired"
		}
	}
	ch <- prometheus.MustNewConstMetric(m.age, prometheus.GaugeValue, age)
	ch <- prometheus.MustNewConstMetric(m.initialized, prometheus.GaugeValue, initialized)
	ch <- prometheus.MustNewConstMetric(m.items, prometheus.GaugeValue, float64(len(snapshot.Items)))
	ch <- prometheus.MustNewConstMetric(m.invalid, prometheus.GaugeValue, float64(len(snapshot.Diagnostics)))
	for _, candidate := range []string{"uninitialized", "fresh", "stale", "expired"} {
		value := 0.0
		if state == candidate {
			value = 1
		}
		ch <- prometheus.MustNewConstMetric(m.state, prometheus.GaugeValue, value, candidate)
	}
	watch := m.server.watchStatus(now)
	ch <- prometheus.MustNewConstMetric(m.reconnect, prometheus.GaugeValue, watch.ReconnectDuration.Seconds())
	for _, candidate := range []string{"starting", "watching", "reconnecting", "stopped"} {
		value := 0.0
		if watch.State == candidate {
			value = 1
		}
		ch <- prometheus.MustNewConstMetric(m.watch, prometheus.GaugeValue, value, candidate)
	}
	m.login.Collect(ch)
	m.denied.Collect(ch)
}
