package pressurecooker

import (
	"fmt"

	"github.com/golang/glog"
	"github.com/prometheus/procfs"

	"time"
)

func (w *Watcher) SetAsHigh(high bool) {
	w.isCurrentlyHigh = high
}

func (w *Watcher) Run(closeChan chan struct{}, useAvarage bool, targetMetic int) (<-chan PressureThresholdEvent, <-chan error) {
	result := make(chan PressureThresholdEvent)
	errs := make(chan error)
	ticker := time.Tick(w.TickerInterval)

	if targetMetic < 1 || targetMetic > 3 {
		glog.Errorf("Target metric is not in available %d choosing midle one", targetMetic)
		targetMetic = 2
	}

	var totalPressureLatency uint64
	go func() {
		defer func() {
			close(result)
			close(errs)
		}()
		for {
			select {
			case <-ticker:
				var res [3]float64
				if useAvarage {

					avg, err := w.proc.LoadAvg()
					if err != nil {
						errs <- err
						continue
					}

					if avg == nil {
						errs <- fmt.Errorf("could not load cpu avarage, got %v", avg)
						continue
					}

					glog.Infof("current Avarage state:  LoadAvg : Load1=%.2f, Load5=%.2f, Load15=%.2f threshold=%.2f HIGH_LOAD=%t",
						avg.Load1, avg.Load5, avg.Load15, w.PressureThreshold, w.isCurrentlyHigh)

					res = extactWorkingData(nil, avg, useAvarage)

				} else {
					cpu, err := w.proc.PSIStatsForResource("cpu")
					if err != nil {
						errs <- err
						continue
					}

					if cpu.Some == nil {
						errs <- fmt.Errorf("could not load cpu pressure, got %v", cpu)
						continue
					}

					totalPressureDiff := cpu.Some.Total - totalPressureLatency
					totalPressureLatency = cpu.Some.Total

					glog.Infof("current Pressure state: totalDiffLatency=%d  avg10=%.2f avg60=%.2f avg300=%.2f threshold=%.2f HIGH_LOAD=%t",
						totalPressureDiff, cpu.Some.Avg10, cpu.Some.Avg60, cpu.Some.Avg300, w.PressureThreshold, w.isCurrentlyHigh)

					res = extactWorkingData(cpu.Some, nil, useAvarage)
				}
				if res[targetMetic-1] >= w.PressureThreshold {
					w.isCurrentlyHigh = true
				} else if res[0] < w.PressureThreshold && res[1] < w.PressureThreshold && res[2] < w.PressureThreshold {
					w.isCurrentlyHigh = false
				}
				result <- PressureThresholdEvent{Message: generateEventMessage(useAvarage, res[targetMetic-1], targetMetic, w.PressureThreshold, w.isCurrentlyHigh), MeticValue: res[targetMetic-1], IsCurrentlyHigh: w.isCurrentlyHigh}

			case <-closeChan:
				return
			}
		}
	}()

	return result, errs
}

func generateEventMessage(useAvarage bool, usedMetric float64, targetMetric int, threshold float64, isOver bool) string {

	messagetype, metricName := targetMetricData(targetMetric, useAvarage)

	var action string
	if isOver {
		action = "exceeded"
	} else {
		action = "deceeded"
	}
	return fmt.Sprintf("%s Metric %s %s=(%.2f) the threshold=(%.2f)", messagetype, metricName, action, usedMetric, threshold)
}

func extactWorkingData(cpu *procfs.PSILine, ava *procfs.LoadAvg, useAvarage bool) [3]float64 {
	if useAvarage {
		return [3]float64{ava.Load1, ava.Load5, ava.Load15}
	} else {
		return [3]float64{cpu.Avg10, cpu.Avg60, cpu.Avg300}
	}
}

func targetMetricData(targetMetric int, useAvarage bool) (string, string) {
	var messagetype string
	var metricName string
	avaNames := []string{"Load1", "Load5", "Load15"}
	pressureNames := []string{"Avg10", "Avg60", "Avg300"}
	if useAvarage {
		messagetype = "LoadAvg"
		metricName = avaNames[targetMetric-1]
	} else {
		messagetype = "Pressure"
		metricName = pressureNames[targetMetric-1]
	}

	return messagetype, metricName
}
