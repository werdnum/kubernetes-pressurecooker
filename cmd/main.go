package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/golang/glog"
	"github.com/rtreffer/kubernetes-pressurecooker/pkg/config"
	"github.com/rtreffer/kubernetes-pressurecooker/pkg/pressurecooker"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	var f config.StartupFlags

	flag.StringVar(&f.KubeConfig, "kubeconfig", "", "file path to kubeconfig")
	flag.Float64Var(&f.TaintThreshold, "taint-threshold", 25, "pressure threshold value")
	flag.Float64Var(&f.EvictThreshold, "evict-threshold", 50, "pressure threshold value")
	flag.StringVar(&f.EvictBackoff, "evict-backoff", "10m", "time to wait between evicting Pods")
	flag.StringVar(&f.MinPodAge, "min-pod-age", "5m", "time to wait between evicting Pods")
	flag.StringVar(&f.NodeName, "node-name", "", "current node name")
	flag.Parse()

	cfg, err := loadKubernetesConfig(f)
	if err != nil {
		panic(err)
	}

	c, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		panic(err)
	}

	w, err := pressurecooker.NewWatcher(f.TaintThreshold)
	if err != nil {
		panic(err)
	}

	t, err := pressurecooker.NewTainter(c, f.NodeName)
	if err != nil {
		panic(err)
	}

	e, err := pressurecooker.NewEvicter(c, f.EvictThreshold, f.NodeName, f.EvictBackoff, f.MinPodAge)
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

	isTainted, err := t.IsNodeTainted()
	if err != nil {
		panic(err)
	}

	w.SetAsHigh(isTainted)

	exc, dec, errs := w.Run(closeChan)
	for {
		select {
		case evt, ok := <-exc:
			if !ok {
				glog.Infof("exceedance channel closed; stopping")
				return
			}

			glog.Infof("5 minute pressure average exceeded threshold, avg300=%f", evt.Avg300)

			if err := t.TaintNode(evt); err != nil {
				glog.Errorf("error while tainting node: %s", err.Error())
			}

			if _, err := e.EvictPod(evt); err != nil {
				glog.Errorf("error while evicting pod: %s", err.Error())
			}
		case evt, ok := <-dec:
			if !ok {
				glog.Infof("deceedance channel closed; stopping")
				return
			}

			glog.Infof("pressure deceeded threshold, avg300=%f avg60=%f avg10=%f", evt.Avg300, evt.Avg60, evt.Avg10)

			if err := t.UntaintNode(evt); err != nil {
				glog.Errorf("error while removing taint from node: %s", err.Error())
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
