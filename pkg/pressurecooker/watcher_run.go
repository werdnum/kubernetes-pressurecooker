package pressurecooker

import (
	"time"

	"github.com/golang/glog"
)

func (w *Watcher) SetAsHigh(high bool) {
	w.isCurrentlyHigh = high
}

func (w *Watcher) Run(closeChan chan struct{}) (<-chan ThresholdEvent, <-chan ThresholdEvent, <-chan error) {
	exceeded := make(chan ThresholdEvent)
	deceeded := make(chan ThresholdEvent)
	errs := make(chan error)
	ticker := time.NewTicker(w.TickerInterval)

	go func() {
		defer func() {
			close(exceeded)
			close(deceeded)
			close(errs)
		}()

		for {
			select {
			case <-ticker.C:
				load, err := w.LoadGetter.GetLoad()
				if err != nil {
					errs <- err
					continue
				}

				glog.Infof("current state: high_load=%t %v threshold=%.2f",
					w.isCurrentlyHigh, load, w.Threshold)

				if load.Load5Min >= w.Threshold {
					if !w.isCurrentlyHigh {
						w.isCurrentlyHigh = true
						exceeded <- ThresholdEvent{Load: load, Threshold: w.Threshold}
					} else if load.Load1Min >= w.Threshold && load.Smallest >= w.Threshold {
						exceeded <- ThresholdEvent{Load: load, Threshold: w.Threshold}
					}
				} else if load.Load5Min < w.Threshold && load.Load1Min < w.Threshold && load.Smallest < w.Threshold {
					w.isCurrentlyHigh = false
					deceeded <- ThresholdEvent{Load: load, Threshold: w.Threshold}
				}
			case <-closeChan:
				return
			}
		}
	}()

	return exceeded, deceeded, errs
}
