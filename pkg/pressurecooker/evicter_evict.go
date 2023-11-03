package pressurecooker

import (
	"time"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
)

var (
	prometheusNamespace = "pressurecooker"
	podsEvictedTotal    = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: prometheusNamespace,
		Name:      "pods_evicted_total",
		Help:      "total number of pods evicted on this node",
	})
)

func init() {
	prometheus.MustRegister(podsEvictedTotal)
}

func (e *Evicter) CanEvict() bool {
	if e.lastEviction.IsZero() {
		return true
	}

	return time.Since(e.lastEviction) > e.backoff
}

func (e *Evicter) EvictPod(evt ThresholdEvent) (bool, error) {
	if evt.Load.Load5Min < e.threshold {
		return false, nil
	}

	if !e.CanEvict() {
		glog.Infof("eviction threshold exceeded; still in back-off")
		return false, nil
	}

	glog.Infof("searching for pod to evict")

	fieldSelector := fields.OneTermEqualSelector("spec.nodeName", e.nodeName)

	podsOnNode, err := e.client.CoreV1().Pods("").List(metav1.ListOptions{
		FieldSelector: fieldSelector.String(),
	})

	if err != nil {
		return false, err
	}

	candidates := PodCandidateSetFromPodList(podsOnNode)
	podToEvict := candidates.SelectPodForEviction(e.minPodAge)

	if podToEvict == nil {
		e.recorder.Eventf(e.nodeRef, v1.EventTypeWarning, "NoPodToEvict", "wanted to evict Pod, but no suitable candidate found")
		return false, nil
	}

	eviction := v1beta1.Eviction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podToEvict.ObjectMeta.Name,
			Namespace: podToEvict.ObjectMeta.Namespace,
		},
	}

	podsEvictedTotal.Inc()

	glog.Infof("eviction: %+v", eviction)

	e.lastEviction = time.Now()

	e.recorder.Eventf(podToEvict, v1.EventTypeWarning, "EvictHighLoad", "evicting pod due to high cpu pressure on node: %s", evt.String())
	e.recorder.Eventf(e.nodeRef, v1.EventTypeWarning, "EvictHighLoad", "evicting pod due to high cpu pressure on node: %s", evt.String())

	err = e.client.CoreV1().Pods(podToEvict.Namespace).Evict(&eviction)
	return true, err
}
