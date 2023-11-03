package config

type StartupFlags struct {
	KubeConfig             string
	PressureTaintThreshold float64
	PressureEvictThreshold float64
	LoadTaintThreshold     float64
	LoadEvictThreshold     float64
	EvictBackoff           string
	MinPodAge              string
	NodeName               string
	MetricsPort            int
}
