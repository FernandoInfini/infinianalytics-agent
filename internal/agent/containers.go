package agent

import (
	"context"
	"sort"
	"time"

	"github.com/rene-roid/kanshi/internal/dockerstats"
)

// containerSampler turns one dockerstats pass per window into container rows.
//
// Containers are identified by compose project/service rather than by id or
// name: a redeploy replaces the container (new id) but it is the same service
// to anyone reading the history, so its series must continue.
type containerSampler struct {
	client *dockerstats.Client
	limit  int

	prevBlk map[string]blkCounters // by container id
}

type blkCounters struct {
	at          time.Time
	read, write uint64
}

func newContainerSampler(client *dockerstats.Client, limit int) *containerSampler {
	return &containerSampler{client: client, limit: limit, prevBlk: map[string]blkCounters{}}
}

// baseKey is project/service from the compose labels, else the container name.
func baseKey(c dockerstats.Container) string {
	if c.Project != "" && c.Name != "" {
		return c.Project + "/" + c.Name
	}
	return c.FullName
}

// containerKeys assigns every container a key unique within the listing.
// Replicas of a scaled service share project/service, so those fall back to
// project/<container name> - stable across windows, unlike an order-based
// suffix would be.
func containerKeys(list []dockerstats.Container) []string {
	count := map[string]int{}
	for _, c := range list {
		count[baseKey(c)]++
	}
	keys := make([]string, len(list))
	for i, c := range list {
		key := baseKey(c)
		if count[key] > 1 && c.Project != "" {
			key = c.Project + "/" + c.FullName
		}
		keys[i] = key
	}
	return keys
}

// pick keeps the busiest running containers (CPU + memory %) and fills any
// room left with stopped ones, so a crashed service stays visible.
func pick(list []dockerstats.Container, keys []string, limit int) ([]dockerstats.Container, []string) {
	idx := make([]int, len(list))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		ca, cb := list[idx[a]], list[idx[b]]
		ra, rb := ca.State == "running", cb.State == "running"
		if ra != rb {
			return ra
		}
		return ca.CPU+ca.MemPercent > cb.CPU+cb.MemPercent
	})
	if limit > 0 && len(idx) > limit {
		idx = idx[:limit]
	}
	outC := make([]dockerstats.Container, len(idx))
	outK := make([]string, len(idx))
	for i, j := range idx {
		outC[i], outK[i] = list[j], keys[j]
	}
	return outC, outK
}

// sample reads every container once and returns this window's rows. ok is
// false when there is no Docker daemon to read.
func (s *containerSampler) sample(ctx context.Context, ts time.Time) (rows []ContainerRow, ok bool) {
	res := s.client.Sample(ctx)
	if res.Unavailable || res.Error != "" {
		return nil, false
	}
	now := time.Now()
	keys := containerKeys(res.Containers)
	list, keys := pick(res.Containers, keys, s.limit)

	seen := map[string]bool{}
	for i, c := range list {
		row := ContainerRow{
			TS:    ts,
			Key:   keys[i],
			Name:  c.Name,
			Image: c.Image,
			State: c.State,
			N:     1,
		}
		if c.Health != nil {
			row.Health = *c.Health
		}
		if c.State == "running" {
			seen[c.ID] = true
			row.CPU = Stat{Avg: ptr(round2(c.CPU)), Max: ptr(round2(c.CPU))}
			row.MemUsed = Stat{Avg: ptr(float64(c.MemUsed)), Max: ptr(float64(c.MemUsed))}
			if c.MemLimit > 0 {
				row.MemLimit = ptr(float64(c.MemLimit))
			}
			row.PIDs = ptr(float64(c.PIDs))
			if c.Net != nil {
				row.NetRX = ptr(round2(c.Net.RXRate))
				row.NetTX = ptr(round2(c.Net.TXRate))
			}
			if c.Blkio != nil {
				row.BlkRead, row.BlkWrite = s.blkRates(c.ID, c.Blkio, now)
			}
		}
		rows = append(rows, row)
	}
	for id := range s.prevBlk {
		if !seen[id] {
			delete(s.prevBlk, id)
		}
	}
	return rows, true
}

// blkRates turns the cumulative block I/O counters into bytes/second.
func (s *containerSampler) blkRates(id string, b *dockerstats.Blkio, now time.Time) (*float64, *float64) {
	prev, ok := s.prevBlk[id]
	s.prevBlk[id] = blkCounters{at: now, read: b.Read, write: b.Write}
	if !ok || !now.After(prev.at) || b.Read < prev.read || b.Write < prev.write {
		return nil, nil // first sighting or a restart that reset the counters
	}
	dt := now.Sub(prev.at).Seconds()
	return ptr(round2(float64(b.Read-prev.read) / dt)), ptr(round2(float64(b.Write-prev.write) / dt))
}
