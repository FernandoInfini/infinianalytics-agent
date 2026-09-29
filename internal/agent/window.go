package agent

import (
	"math"
	"time"

	"github.com/rene-roid/kanshi/internal/vitals"
)

// acc folds readings into min / mean / max.
type acc struct {
	n             int
	sum, min, max float64
}

func (a *acc) add(v float64) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return
	}
	if a.n == 0 || v < a.min {
		a.min = v
	}
	if a.n == 0 || v > a.max {
		a.max = v
	}
	a.sum += v
	a.n++
}

// full is avg/min/max; empty when nothing was added.
func (a acc) full() Stat {
	if a.n == 0 {
		return Stat{}
	}
	return Stat{Avg: ptr(round2(a.sum / float64(a.n))), Min: ptr(round2(a.min)), Max: ptr(round2(a.max))}
}

// avgMax is avg/max, for rates and sizes where the minimum says little.
func (a acc) avgMax() Stat {
	if a.n == 0 {
		return Stat{}
	}
	return Stat{Avg: ptr(round2(a.sum / float64(a.n))), Max: ptr(round2(a.max))}
}

func (a acc) mean() *float64 {
	if a.n == 0 {
		return nil
	}
	return ptr(round2(a.sum / float64(a.n)))
}

func (a acc) maximum() *float64 {
	if a.n == 0 {
		return nil
	}
	return ptr(round2(a.max))
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// windowStart aligns t to the window grid. time.Truncate counts from year 1,
// and the Unix epoch sits a whole number of days after it, so a 10 s grid here
// is the same 10 s grid everywhere - agents and the backend agree on it.
func windowStart(t time.Time, d time.Duration) time.Time {
	return t.UTC().Truncate(d)
}

// hostWindow accumulates the vitals samples that fall into one window.
type hostWindow struct {
	start time.Time
	dur   time.Duration

	n                           int
	cpu, memPct, memUsed, swap  acc
	netRX, netTX, diskR, diskW  acc
	coreMax, load, temp, uptime acc
	lastCores                   []float64
	memTotal                    uint64
}

func newHostWindow(start time.Time, dur time.Duration) *hostWindow {
	return &hostWindow{start: start, dur: dur}
}

func (w *hostWindow) add(s vitals.Sample) {
	w.n++
	w.cpu.add(s.CPU.Percent)
	for _, c := range s.CPU.Cores {
		// Only the busiest core over the window is history; the per-core
		// list itself is kept as the current reading.
		w.coreMax.add(c)
	}
	w.lastCores = append(w.lastCores[:0], s.CPU.Cores...)
	w.memPct.add(s.Memory.Percent)
	w.memUsed.add(float64(s.Memory.Used))
	w.memTotal = s.Memory.Total
	if s.Swap.Total > 0 {
		w.swap.add(s.Swap.Percent)
	}
	w.netRX.add(s.Network.RX)
	w.netTX.add(s.Network.TX)
	w.diskR.add(s.DiskIO.Read)
	w.diskW.add(s.DiskIO.Write)
	if s.CPU.Load != nil {
		w.load.add(s.CPU.Load[0])
	}
	if s.CPU.Temp != nil {
		w.temp.add(*s.CPU.Temp)
	}
	if s.Uptime > 0 {
		w.uptime.add(s.Uptime)
	}
}

// sample closes the window into its wire row. ok is false for an empty window.
func (w *hostWindow) sample() (HostSample, bool) {
	if w.n == 0 {
		return HostSample{}, false
	}
	out := HostSample{
		TS:         w.start,
		DurS:       int(w.dur / time.Second),
		N:          w.n,
		CPU:        w.cpu.full(),
		CPUCoreMax: w.coreMax.maximum(),
		MemPct:     w.memPct.full(),
		MemUsed:    w.memUsed.avgMax(),
		SwapPct:    w.swap.avgMax(),
		NetRX:      w.netRX.avgMax(),
		NetTX:      w.netTX.avgMax(),
		DiskRead:   w.diskR.avgMax(),
		DiskWrite:  w.diskW.avgMax(),
		Load1:      w.load.mean(),
		TempMax:    w.temp.maximum(),
	}
	if w.uptime.n > 0 {
		out.UptimeS = ptr(int64(w.uptime.max))
	}
	return out, true
}
