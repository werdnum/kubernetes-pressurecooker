package pressurecooker

import (
	"fmt"

	"github.com/prometheus/procfs"
)

type Load struct {
	Source   string
	Smallest float64
	Load1Min float64
	Load5Min float64
}

type LoadGetter interface {
	GetLoad() (Load, error)
}

type PressureLoadGetter struct {
	ProcFS procfs.FS
}

func (g *PressureLoadGetter) GetLoad() (Load, error) {
	cpu, err := g.ProcFS.PSIStatsForResource("cpu")
	if err != nil {
		return Load{}, err
	}

	if cpu.Some == nil {
		return Load{}, fmt.Errorf("could not load cpu pressure, got %v", cpu)
	}

	return Load{
		Source:   "psi",
		Smallest: cpu.Some.Avg10,
		Load1Min: cpu.Some.Avg60,
		Load5Min: cpu.Some.Avg300,
	}, nil
}

type LoadAvgLoadGetter struct {
	ProcFS procfs.FS
}

func (g *LoadAvgLoadGetter) GetLoad() (Load, error) {
	la, err := g.ProcFS.LoadAvg()
	if err != nil {
		return Load{}, err
	}

	return Load{
		Source:   "psi",
		Smallest: la.Load1,
		Load1Min: la.Load1,
		Load5Min: la.Load5,
	}, nil
}
