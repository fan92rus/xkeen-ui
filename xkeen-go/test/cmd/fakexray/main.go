package main

import (
	"encoding/json"
	"log"
	"math"
	"net/http"
	"sync/atomic"
	"time"
)

// Fake Xray metrics server for local development.
// Serves /debug/vars with incrementing counters to simulate real traffic.

var (
	dlCounter uint64
	ulCounter uint64
)

func main() {
	addr := ":11111"
	log.Printf("Fake Xray metrics server on %s", addr)

	// Simulate traffic growth in background
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			// Simulate ~2 MB/s download, ~500 KB/s upload with some variance
			atomic.AddUint64(&dlCounter, uint64(800000+time.Now().UnixNano()%800000))
			atomic.AddUint64(&ulCounter, uint64(150000+time.Now().UnixNano()%100000))
		}
	}()

	http.HandleFunc("/debug/vars", func(w http.ResponseWriter, r *http.Request) {
		dl := atomic.LoadUint64(&dlCounter)
		ul := atomic.LoadUint64(&ulCounter)

		resp := map[string]interface{}{
			"stats": map[string]interface{}{
				"inbound": map[string]interface{}{
					"tproxy_tcp_inbound": map[string]interface{}{
						"downlink": dl,
						"uplink":   ul,
					},
					"http_inbound": map[string]interface{}{
						"downlink": dl / 10,
						"uplink":   ul / 10,
					},
				},
				"outbound": map[string]interface{}{
					"proxy-DE-1": map[string]interface{}{
						"downlink": dl * 3 / 4,
						"uplink":   ul * 3 / 5,
					},
					"proxy-US-1": map[string]interface{}{
						"downlink": dl / 4,
						"uplink":   ul * 2 / 5,
					},
					"direct": map[string]interface{}{
						"downlink": dl / 20,
						"uplink":   ul / 20,
					},
				},
			},
			"observatory": map[string]interface{}{
				"proxy-DE-1": map[string]interface{}{
					"alive":         true,
					"delay":         180 + float64(time.Now().UnixNano()%200),
					"outbound_tag":  "proxy-DE-1",
					"last_seen_time": time.Now().Unix() - 5,
					"last_try_time": time.Now().Unix() - 5,
				},
				"proxy-US-1": map[string]interface{}{
					"alive":         true,
					"delay":         320 + float64(time.Now().UnixNano()%400),
					"outbound_tag":  "proxy-US-1",
					"last_seen_time": time.Now().Unix() - 12,
					"last_try_time": time.Now().Unix() - 12,
				},
				"proxy-JP-1": map[string]interface{}{
					"alive":         false,
					"delay":         0,
					"outbound_tag":  "proxy-JP-1",
					"last_seen_time": time.Now().Unix() - 3600,
					"last_try_time": time.Now().Unix() - 30,
				},
			},
		}

		// Sometimes return null stats to test that edge case
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = math.Sin(0) // suppress unused import
		enc.Encode(resp)
	})

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}
