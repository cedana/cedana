package utils

import (
	"encoding/json"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	"github.com/felixge/fgprof"
)

type Timings struct {
	enabled bool
	data    map[string]int64
	timers  map[OperationType]time.Time
}

type OperationType string

const (
	CriuCheckpointOp OperationType = "checkpoint"
	CriuRestoreOp    OperationType = "restore"
	CompressOp       OperationType = "compress"
	DecompressOp     OperationType = "decompress"
	UploadOp         OperationType = "upload"
	DownloadOp       OperationType = "download"
)

func NewTimings() *Timings {
	enabled := os.Getenv("CEDANA_PROFILING_ENABLED") == "true"

	return &Timings{
		enabled: enabled,
		data:    make(map[string]int64),
		timers:  make(map[OperationType]time.Time),
	}
}

func (t *Timings) Start(name OperationType) {
	if !t.enabled {
		return
	}

	t.timers[name] = time.Now()
}

func (t *Timings) Stop(name OperationType) {
	if !t.enabled {
		return
	}

	elapsed := time.Since(t.timers[name])
	t.data[string(name)] = elapsed.Nanoseconds()
}

func (t *Timings) Flush() error {
	if t.enabled {
		jsonData, err := json.Marshal(t.data)
		if err != nil {
			return err
		}
		_ = os.WriteFile("/var/log/cedana-profile.json", jsonData, 0644)
	}

	return nil
}

// TODO NR - add memory profiling
// TODO NR - do we need to play with sampling rate here?
var stop func() error

func setupProfilerHandlers() {
	// Handler to start CPU profiling
	http.HandleFunc("/start-profiling", func(w http.ResponseWriter, r *http.Request) {
		var filename string
		prefix := r.URL.Query().Get("prefix")
		if prefix == "" {
			filename = "cpu.pprof"
		} else {
			filename = prefix + ".pprof"
		}

		cpu, err := os.Create(filename)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		stop = fgprof.Start(cpu, fgprof.FormatPprof)

		// TODO NR - add memory profile
		fmt.Println("Started CPU profiling")

		w.WriteHeader(http.StatusOK)
	})

	// Handler to stop CPU profiling
	http.HandleFunc("/stop-profiling", func(w http.ResponseWriter, r *http.Request) {

		filename := r.URL.Query().Get("filename")
		if filename == "" {
			// Set the header before writing the response body
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintln(w, "Filename parameter is required")
			return
		}

		stop()
		f, err := os.Open(filename + ".pprof")
		if err != nil {
			// Set the header before writing the response body
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "Error opening file: %v", err)
			return
		}
		defer f.Close()

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Stopped CPU profiling for file: %s\n", filename+".pprof")
	})
}

func StartPprofServer() {
	setupProfilerHandlers()
	go http.ListenAndServe("localhost:6060", nil)
}
