package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const dockerAPI = "v1.43"

// dockerEvents follows the daemon's event stream and turns container
// lifecycle changes into agent events: starts, stops, crashes with their exit
// code, OOM kills, restarts, health flips and - by comparing each start's image
// with the one that service ran before - deploys.
type dockerEvents struct {
	http *http.Client
	emit func(Event)
	logf func(string, ...any)

	mu     sync.Mutex
	images map[string]string // key → image last seen running
}

func newDockerEvents(host string, emit func(Event), logf func(string, ...any)) (*dockerEvents, error) {
	dial, err := dockerDialer(host)
	if err != nil {
		return nil, err
	}
	return &dockerEvents{
		http: &http.Client{
			// No timeout: the events response is a stream that never ends.
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return dial(ctx) },
			},
		},
		emit:   emit,
		logf:   logf,
		images: map[string]string{},
	}, nil
}

// run follows the stream until ctx ends, reconnecting with backoff.
func (d *dockerEvents) run(ctx context.Context) {
	b := newBackoff(time.Second, time.Minute)
	for ctx.Err() == nil {
		err := d.follow(ctx)
		if ctx.Err() != nil {
			return
		}
		wait := b.next()
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "dial" {
			// No daemon on this machine (or not yet): look again rarely.
			wait = 5 * time.Minute
		} else if err != nil {
			d.logf("docker events: %v; reconnecting in %s", err, wait.Round(time.Second))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

type dockerMessage struct {
	Type   string `json:"Type"`
	Action string `json:"Action"`
	Actor  struct {
		ID         string            `json:"ID"`
		Attributes map[string]string `json:"Attributes"`
	} `json:"Actor"`
	TimeNano int64 `json:"timeNano"`
}

func (d *dockerEvents) follow(ctx context.Context) error {
	if err := d.seedImages(ctx); err != nil {
		return err
	}
	filters := url.QueryEscape(`{"type":["container"]}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://docker/"+dockerAPI+"/events?filters="+filters, nil)
	if err != nil {
		return err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("events: %s", resp.Status)
	}
	dec := json.NewDecoder(resp.Body)
	for {
		var msg dockerMessage
		if err := dec.Decode(&msg); err != nil {
			return err
		}
		if ev, ok := d.translate(msg); ok {
			d.emit(ev)
		}
	}
}

// seedImages records what every container runs now, so the first start after
// the agent starts is only reported as a deploy if the image really changed.
func (d *dockerEvents) seedImages(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/"+dockerAPI+"/containers/json?all=true", nil)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	resp, err := d.http.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var list []struct {
		Names  []string          `json:"Names"`
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range list {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimLeft(c.Names[0], "/")
		}
		if key := eventKey(c.Labels, name); key != "" {
			d.images[key] = c.Image
		}
	}
	return nil
}

// eventKey mirrors containerKeys' base rule (compose project/service, else
// the container name) from an event's attributes, which carry the labels.
func eventKey(attrs map[string]string, name string) string {
	project, service := attrs["com.docker.compose.project"], attrs["com.docker.compose.service"]
	switch {
	case project != "" && service != "":
		return project + "/" + service
	case project != "" && name != "":
		return project + "/" + name
	}
	return name
}

func (d *dockerEvents) translate(msg dockerMessage) (Event, bool) {
	if msg.Type != "container" {
		return Event{}, false
	}
	attrs := msg.Actor.Attributes
	name := attrs["name"]
	key := eventKey(attrs, name)
	ts := time.Now().UTC()
	if msg.TimeNano > 0 {
		ts = time.Unix(0, msg.TimeNano).UTC()
	}
	data := map[string]any{"key": key, "name": name}
	if img := attrs["image"]; img != "" {
		data["image"] = img
	}

	action := msg.Action
	switch {
	case action == "start":
		d.mu.Lock()
		prev, known := d.images[key]
		d.images[key] = attrs["image"]
		d.mu.Unlock()
		if known && prev != "" && attrs["image"] != "" && prev != attrs["image"] {
			// A start on a new image is a deploy; the start itself is
			// implied, so only the more useful marker is sent.
			return Event{TS: ts, Type: EventContainerImage, Data: map[string]any{
				"key": key, "name": name, "from": prev, "to": attrs["image"],
			}}, true
		}
		return Event{TS: ts, Type: EventContainerStart, Data: data}, true
	case action == "stop":
		return Event{TS: ts, Type: EventContainerStop, Data: data}, true
	case action == "die":
		if code, err := strconv.Atoi(attrs["exitCode"]); err == nil {
			data["exit_code"] = code
		}
		return Event{TS: ts, Type: EventContainerDie, Data: data}, true
	case action == "oom":
		return Event{TS: ts, Type: EventContainerOOM, Data: data}, true
	case action == "restart":
		return Event{TS: ts, Type: EventContainerRestart, Data: data}, true
	case strings.HasPrefix(action, "health_status"):
		status := strings.TrimSpace(strings.TrimPrefix(action, "health_status:"))
		data["status"] = status
		return Event{TS: ts, Type: EventContainerHealth, Data: data}, true
	}
	return Event{}, false
}
