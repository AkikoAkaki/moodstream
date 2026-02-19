package observability

import (
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	registerOnce sync.Once

	taskEnqueueTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "task_enqueue_total",
			Help: "Total number of enqueue attempts by topic and status.",
		},
		[]string{"topic", "status"},
	)

	taskProcessDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "task_process_duration_seconds",
			Help:    "Task processing latency in seconds by topic.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"topic"},
	)

	queueDepth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "queue_depth",
			Help: "Current queue depth by topic.",
		},
		[]string{"topic"},
	)
)

func init() {
	Register()
}

func Register() {
	registerOnce.Do(func() {
		prometheus.MustRegister(taskEnqueueTotal, taskProcessDurationSeconds, queueDepth)
	})
}

func IncTaskEnqueue(topic, status string) {
	Register()
	taskEnqueueTotal.WithLabelValues(normalize(topic), normalize(status)).Inc()
}

func ObserveTaskProcessDuration(topic string, d time.Duration) {
	Register()
	taskProcessDurationSeconds.WithLabelValues(normalize(topic)).Observe(d.Seconds())
}

func SetQueueDepth(topic string, depth float64) {
	Register()
	queueDepth.WithLabelValues(normalize(topic)).Set(depth)
}

func normalize(v string) string {
	value := strings.TrimSpace(v)
	if value == "" {
		return "unknown"
	}
	return value
}
