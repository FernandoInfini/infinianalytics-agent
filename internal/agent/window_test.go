package agent

import (
	"math"
	"testing"
	"time"

	"github.com/rene-roid/kanshi/internal/vitals"
)

func sampleWith(cpu float64, cores []float64, mem float64) vitals.Sample {
	load := [3]float64{1.5, 1, 0.5}
	return vitals.Sample{
		CPU:     vitals.CPU{Percent: cpu, Cores: cores, Load: &load, Count: len(cores)},
		Memory:  vitals.Memory{Total: 1000, Used: uint64(mem * 10), Percent: mem},
		Swap:    vitals.Swap{Total: 100, Used: 10, Percent: 10},
		Network: vitals.RxTx{RX: cpu * 10, TX: 1},
		DiskIO:  vitals.ReadWrite{Read: 5, Write: cpu},
		Uptime:  3600 + cpu,
	}
}

func TestWindowAggregatesMinAvgMaxAndN(t *testing.T) {
	w := newHostWindow(time.Unix(1000, 0).UTC(), 10*time.Second)
	for _, s := range []vitals.Sample{
		sampleWith(10, []float64{5, 15}, 40),
		sampleWith(30, []float64{20, 97.5}, 50),
		sampleWith(20, []float64{10, 30}, 60),
	} {
		w.add(s)
	}
	got, ok := w.sample()
	if !ok {
		t.Fatal("window with samples reported empty")
	}
	if got.N != 3 || got.DurS != 10 || !got.TS.Equal(time.Unix(1000, 0)) {
		t.Fatalf("n/dur/ts = %d/%d/%v", got.N, got.DurS, got.TS)
	}
	check := func(name string, s Stat, avg, lo, hi float64) {
		t.Helper()
		if s.Avg == nil || *s.Avg != avg {
			t.Errorf("%s avg = %v, want %v", name, deref(s.Avg), avg)
		}
		if lo >= 0 && (s.Min == nil || *s.Min != lo) {
			t.Errorf("%s min = %v, want %v", name, deref(s.Min), lo)
		}
		if s.Max == nil || *s.Max != hi {
			t.Errorf("%s max = %v, want %v", name, deref(s.Max), hi)
		}
	}
	check("cpu", got.CPU, 20, 10, 30)
	check("mem", got.MemPct, 50, 40, 60)
	check("net_rx", got.NetRX, 200, -1, 300)
	if got.NetRX.Min != nil {
		t.Error("rates carry no min")
	}
	// The busiest single core anywhere in the window.
	if got.CPUCoreMax == nil || *got.CPUCoreMax != 97.5 {
		t.Errorf("cpu_core_max = %v", deref(got.CPUCoreMax))
	}
	if got.Load1 == nil || *got.Load1 != 1.5 {
		t.Errorf("load1 = %v", deref(got.Load1))
	}
	if got.UptimeS == nil || *got.UptimeS != 3630 {
		t.Errorf("uptime = %v", got.UptimeS)
	}
	if got.TempMax != nil {
		t.Error("no sensor, no temperature")
	}
}

func TestEmptyWindow(t *testing.T) {
	if _, ok := newHostWindow(time.Now(), 10*time.Second).sample(); ok {
		t.Fatal("an empty window must not produce a row")
	}
}

func TestAccIgnoresNaN(t *testing.T) {
	var a acc
	a.add(math.NaN())
	a.add(4)
	a.add(math.Inf(1))
	if a.n != 1 || *a.full().Avg != 4 {
		t.Fatalf("acc = %+v", a)
	}
}

func TestWindowStartIsOnTheEpochGrid(t *testing.T) {
	for _, sec := range []int64{1727604000, 1727604009, 1727604005} {
		got := windowStart(time.Unix(sec, 123456789), 10*time.Second)
		if got.Unix()%10 != 0 || got.Unix() > sec || sec-got.Unix() >= 10 {
			t.Errorf("windowStart(%d) = %d", sec, got.Unix())
		}
	}
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}
