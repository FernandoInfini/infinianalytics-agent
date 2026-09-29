// Command fakeingest stands in for the InfiniAnalytics ingestion API in
// scripts/smoke.sh. It enrolls any code, accepts metric batches and logs what
// arrived under -dir:
//
//	batches.log  one line per accepted push: "<seq> <samples>"
//	samples.log  one line per host window: its RFC 3339 start
//	events.log   one line per event: its type
//
// While the file <dir>/down exists every push answers 503, which is how the
// smoke test simulates an outage.
package main

import (
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type batch struct {
	Seq     uint64 `json:"seq"`
	Samples []struct {
		TS time.Time `json:"ts"`
	} `json:"samples"`
	Events []struct {
		Type string `json:"type"`
	} `json:"events"`
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8199", "listen address")
	dir := flag.String("dir", ".", "where to write the logs")
	flag.Parse()
	var mu sync.Mutex
	appendLine := func(name, line string) {
		f, err := os.OpenFile(filepath.Join(*dir, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			log.Print(err)
			return
		}
		fmt.Fprintln(f, line)
		f.Close()
	}

	http.HandleFunc("/v1/servers/enroll/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"server_id":"00000000-0000-4000-8000-000000000001","agent_key":"iak_smoke","push_url":"x"}`))
	})
	http.HandleFunc("/v1/servers/metrics/", func(w http.ResponseWriter, r *http.Request) {
		if _, err := os.Stat(filepath.Join(*dir, "down")); err == nil {
			http.Error(w, `{"detail":"down for the smoke test"}`, http.StatusServiceUnavailable)
			return
		}
		if r.Header.Get("X-Agent-Key") != "iak_smoke" {
			http.Error(w, `{"detail":"bad key"}`, http.StatusUnauthorized)
			return
		}
		var body io.Reader = r.Body
		if r.Header.Get("Content-Encoding") == "gzip" {
			zr, err := gzip.NewReader(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			body = zr
		}
		var b batch
		if err := json.NewDecoder(body).Decode(&b); err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		appendLine("batches.log", fmt.Sprintf("%d %d", b.Seq, len(b.Samples)))
		for _, s := range b.Samples {
			appendLine("samples.log", s.TS.UTC().Format(time.RFC3339))
		}
		for _, e := range b.Events {
			appendLine("events.log", e.Type)
		}
		fmt.Fprintf(w, `{"accepted_seq":%d,"next_push_s":10}`, b.Seq)
	})
	log.Printf("fakeingest listening on %s, logging to %s", *addr, *dir)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
