package governor

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// governorModeGauge reports the current governor mode (Normal=0, Throttle=1,
	// Shed=2).
	governorModeGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "governor_mode",
		Help: "Current resource governor mode (0=normal, 1=throttle, 2=shed)",
	})

	// governorAdmissionRejected counts admissions rejected by the governor,
	// labelled by class and endpoint.
	governorAdmissionRejected = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "governor_admission_rejected_total",
		Help: "Total number of admissions rejected by the resource governor",
	}, []string{"class", "endpoint"})

	// governorAdmissionInflight tracks the number of currently admitted
	// requests, labelled by class.
	governorAdmissionInflight = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "governor_admission_inflight",
		Help: "Number of currently admitted requests by class",
	}, []string{"class"})
)
