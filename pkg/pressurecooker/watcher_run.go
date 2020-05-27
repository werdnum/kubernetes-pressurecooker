package pressurecooker

import (
	"fmt"

	"github.com/golang/glog"

	"time"
)

func (w *Watcher) SetAsHigh(high bool) {
	w.isCurrentlyHigh = high
}

func (w *Watcher) Run(closeChan chan struct{}) (<-chan PressureThresholdEvent, <-chan PressureThresholdEvent, <-chan error) {
	exceeded := make(chan PressureThresholdEvent)
	deceeded := make(chan PressureThresholdEvent)
	errs := make(chan error)
	ticker := time.Tick(w.TickerInterval)

	go func() {
		defer func() {
			close(exceeded)
			close(deceeded)
			close(errs)
		}()

		for {
			select {
			case <-ticker:
				cpu, err := w.proc.PSIStatsForResource("cpu")
				if err != nil {
					errs <- err
					continue
				}

				if cpu.Some == nil {
					errs <- fmt.Errorf("could not load cpu pressure, got %v", cpu)
					continue
				}

				glog.Infof("current state: high_load=%t avg10=%.2f avg60=%.2f avg300=%.2f threshold=%.2f",
					w.isCurrentlyHigh, cpu.Some.Avg10, cpu.Some.Avg60, cpu.Some.Avg300, w.PressureThreshold)

				if cpu.Some.Avg300 >= w.PressureThreshold && !w.isCurrentlyHigh {
					w.isCurrentlyHigh = true
					exceeded <- PressureThresholdEvent(*cpu.Some)
				} else if cpu.Some.Avg300 < w.PressureThreshold && cpu.Some.Avg60 < w.PressureThreshold && cpu.Some.Avg10 < w.PressureThreshold && w.isCurrentlyHigh {
					w.isCurrentlyHigh = false
					deceeded <- PressureThresholdEvent(*cpu.Some)
				}
			case <-closeChan:
				return
			}
		}
	}()

	return exceeded, deceeded, errs
}
