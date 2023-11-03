package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/procfs"
	"github.com/rtreffer/kubernetes-pressurecooker/pkg/config"
	"github.com/rtreffer/kubernetes-pressurecooker/pkg/pressurecooker"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	prometheusNamespace       = "pressurecooker"
	pressureThresholdExceeded = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: prometheusNamespace,
		Name:      "pressure_threshold_exceeded",
		Help:      "cpu pressure is currently above (1) or below (0) threshold",
	})
	pressureThresholdExceededTotal = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: prometheusNamespace,
		Name:      "pressure_threshold_exceeded_total",
		Help:      "number of times the pressure threshold was exceeded",
	})
	pressureRecoveredTotal = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: prometheusNamespace,
		Name:      "pressure_recovered_total",
		Help:      "number of times the pressure on the node recovered",
	})
	pressureMode = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: prometheusNamespace,
		Name:      "mode",
		Help:      "pressurecooker mode",
	}, []string{"mode"})
	pressureEnabled = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: prometheusNamespace,
		Name:      "enabled",
		Help:      "pressurecooker is enabled (1) or disabled (0)",
	})
)

func main() {
	prometheus.MustRegister(pressureThresholdExceeded)
	prometheus.MustRegister(pressureThresholdExceededTotal)
	prometheus.MustRegister(pressureRecoveredTotal)
	prometheus.MustRegister(pressureEnabled)

	var f config.StartupFlags

	flag.StringVar(&f.KubeConfig, "kubeconfig", "", "file path to kubeconfig")
	flag.Float64Var(&f.PressureTaintThreshold, "taint-threshold", 25, "pressure threshold value to taint the node")
	flag.Float64Var(&f.PressureEvictThreshold, "evict-threshold", 50, "pressure threshold value to evict pods")
	flag.Float64Var(&f.LoadTaintThreshold, "load-taint-threshold", 25, "load average threshold value to taint the node - used if pressure is not available")
	flag.Float64Var(&f.LoadEvictThreshold, "load-evict-threshold", 50, "load average threshold value to evict pods - used if pressure is not available")
	flag.StringVar(&f.EvictBackoff, "evict-backoff", "10m", "time to wait between evicting Pods")
	flag.StringVar(&f.MinPodAge, "min-pod-age", "5m", "minimum age of Pods to be evicted")
	flag.StringVar(&f.NodeName, "node-name", "", "current node name")
	flag.IntVar(&f.MetricsPort, "metrics-port", 8080, "port for prometheus metrics endpoint")
	flag.Parse()

	if f.NodeName == "" {
		panic("-node-name not set")
	}

	cfg, err := loadKubernetesConfig(f)
	if err != nil {
		panic(err)
	}

	c, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		panic(err)
	}

	fs, err := procfs.NewDefaultFS()

	if err != nil {
		panic(err)
	}

	var lg pressurecooker.LoadGetter
	var taintThreshold float64
	var evictThreshold float64
	if _, err := fs.PSIStatsForResource("cpu"); err == nil {
		lg = &pressurecooker.PressureLoadGetter{ProcFS: fs}
		taintThreshold = f.PressureTaintThreshold
		evictThreshold = f.PressureEvictThreshold
		pressureMode.WithLabelValues("psi").Set(1)
	} else {
		lg = &pressurecooker.LoadAvgLoadGetter{ProcFS: fs}
		taintThreshold = f.LoadTaintThreshold
		evictThreshold = f.LoadEvictThreshold
		pressureMode.WithLabelValues("loadavg").Set(1)
	}

	w, err := pressurecooker.NewWatcher(taintThreshold, lg)
	if err != nil {
		panic(err)
	}

	t, err := pressurecooker.NewTainter(c, f.NodeName)
	if err != nil {
		panic(err)
	}

	e, err := pressurecooker.NewEvicter(c, evictThreshold, f.NodeName, f.EvictBackoff, f.MinPodAge)
	if err != nil {
		panic(err)
	}

	closeChan := make(chan struct{})

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		s := <-sigChan

		glog.Infof("received signal %s", s)

		close(closeChan)
	}()

	go func() {
		http.HandleFunc("/-/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("OK\n"))
		})
		http.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", f.MetricsPort), nil)
	}()

	isTainted, err := t.IsNodeTainted()
	if err != nil {
		panic(err)
	}

	isDisabled, err := t.IsPressurecookerDisabled()
	lastDisabledCheck := time.Now()
	if err != nil {
		panic(err)
	}
	if isDisabled {
		pressureEnabled.Set(0)
	} else {
		pressureEnabled.Set(1)
	}

	w.SetAsHigh(isTainted)
	if isTainted {
		pressureThresholdExceeded.Set(1)
	} else {
		pressureThresholdExceeded.Set(0)
	}

	exc, dec, errs := w.Run(closeChan)
	for {
		select {
		case evt, ok := <-exc:
			if !ok {
				glog.Infof("exceedance channel closed; stopping")
				return
			}

			if time.Since(lastDisabledCheck) > 1*time.Minute {
				if disabled, err := t.IsPressurecookerDisabled(); err == nil {
					isDisabled = disabled
					if isDisabled {
						pressureEnabled.Set(0)
					} else {
						pressureEnabled.Set(1)
					}
				} else {
					glog.Errorf("could not check pressurecooker.enabled: %s", err.Error())
				}
				lastDisabledCheck = time.Now()
			}
			if isDisabled && isTainted {
				if err := t.UntaintNode(pressurecooker.ThresholdEvent{}); err != nil {
					glog.Errorf("error while untainting node: %s", err.Error())
				} else {
					isTainted = false
				}
			}

			if isDisabled {
				glog.Infof("pressurecooker disabled, pressure: %v", evt.String())
				continue
			}

			if isTainted {
				if _, err := e.EvictPod(evt); err != nil {
					glog.Errorf("error while evicting pod: %s", err.Error())
				}
				continue
			}

			glog.Infof("5 minute pressure average exceeded threshold, %v", evt.Load)

			if err := t.TaintNode(evt); err != nil {
				glog.Errorf("error while tainting node: %s", err.Error())
			} else {
				isTainted = true
				pressureThresholdExceeded.Set(1)
				pressureThresholdExceededTotal.Inc()
			}
		case evt, ok := <-dec:
			if !ok {
				glog.Infof("deceedance channel closed; stopping")
				return
			}

			if !isTainted {
				continue
			}

			glog.Infof("pressure deceeded threshold, %s", evt.String())
			if err := t.UntaintNode(evt); err != nil {
				glog.Errorf("error while removing taint from node: %s", err.Error())
			} else {
				isTainted = false
				pressureThresholdExceeded.Set(0)
				pressureRecoveredTotal.Inc()
			}

		case err := <-errs:
			glog.Errorf("error while polling for status updates: %s", err.Error())
		}
	}
}

func loadKubernetesConfig(f config.StartupFlags) (*rest.Config, error) {
	if f.KubeConfig == "" {
		return rest.InClusterConfig()
	}

	return clientcmd.BuildConfigFromFlags("", f.KubeConfig)
}
