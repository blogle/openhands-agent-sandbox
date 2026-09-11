// Package metrics provides Prometheus metrics for the runtime adapter.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "openhands_runtime"

var (
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "requests_total",
		Help:      "Total number of runtime API requests.",
	}, []string{"operation", "result"})

	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "request_duration_seconds",
		Help:      "Duration of runtime API requests in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"operation"})

	ActiveRuntimes = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "active",
		Help:      "Number of currently active runtimes.",
	})

	PausedRuntimes = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "paused",
		Help:      "Number of currently paused runtimes.",
	})

	StartDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "start_duration_seconds",
		Help:      "Duration of the full start lifecycle.",
		Buckets:   []float64{5, 10, 15, 30, 60, 120, 180, 300},
	})

	ClaimReadyDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "claim_ready_duration_seconds",
		Help:      "Time for SandboxClaim to become Ready.",
		Buckets:   []float64{5, 10, 15, 30, 60, 120, 180},
	})

	InitDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "init_duration_seconds",
		Help:      "Time for Agent Server deferred init to complete.",
		Buckets:   []float64{1, 2, 5, 10, 20, 30, 60},
	})

	ProxyRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "proxy_requests_total",
		Help:      "Total proxy requests.",
	}, []string{"result"})

	ErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "errors_total",
		Help:      "Total errors by operation and class.",
	}, []string{"operation", "class"})
)
