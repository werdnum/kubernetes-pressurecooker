package pressurecooker

import (
	"fmt"
	"time"
)

type ThresholdEvent struct {
	Load      Load
	Threshold float64
}

func (t ThresholdEvent) String() string {
	return fmt.Sprintf("load=%v threshold=%.2f", t.Load, t.Threshold)
}

type Watcher struct {
	TickerInterval time.Duration
	Threshold      float64
	LoadGetter     LoadGetter

	isCurrentlyHigh bool
}

func NewWatcher(threshold float64, loadGetter LoadGetter) (*Watcher, error) {
	if threshold == 0 {
		threshold = 25
	}

	return &Watcher{
		Threshold:      threshold,
		TickerInterval: 15 * time.Second,
		LoadGetter:     loadGetter,
	}, nil
}
