package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	stateDir       = "/home/runner/state"
	historyFile    = "/home/runner/state/history.json"
	runnerCfgDir   = "/home/runner/state/runner-config"
	runnerWorkDir  = "/home/runner/state/work"
	runnerHomeDir  = "/runner"
	defaultGateway = "http://runner-gateway.ate-demo-github.svc.cluster.local:8080"
)

type ReviewRecord struct {
	PR             int       `json:"pr"`
	Repo           string    `json:"repo"`
	SHA            string    `json:"sha"`
	Timestamp      time.Time `json:"timestamp"`
	RAMReviewCount int64     `json:"ramReviewCount"`
	RAMWakeCount   int64     `json:"ramWakeCount"`
	WorkerIP       string    `json:"workerIp"`
}

type TelemetryResponse struct {
	ActorName        string         `json:"actorName"`
	Atespace         string         `json:"atespace"`
	Kernel           string         `json:"kernel"`
	WorkerIP         string         `json:"workerIp"`
	RAMWakeCount     int64          `json:"ramWakeCount"`
	RAMReviewCount   int64          `json:"ramReviewCount"`
	DurableCount     int            `json:"durableCount"`
	RunnerActive     bool           `json:"runnerActive"`
	History          []ReviewRecord `json:"history"`
}

var (
	// Volatile in-memory counters preserved across gVisor SNAPSHOT_CONTENT_SCOPE_FULL suspend/resume!
	ramWakeCount   atomic.Int64
	ramReviewCount atomic.Int64

	runnerMu  sync.Mutex
	runnerCmd *exec.Cmd
)

func getGatewayURL() string {
	if v := os.Getenv("RUNNER_GATEWAY_URL"); v != "" {
		return v
	}
	return defaultGateway
}

func getKernelInfo() string {
	out, err := exec.Command("uname", "-sr").Output()
	if err != nil {
		return "Linux (gVisor)"
	}
	return strings.TrimSpace(string(out))
}

func getWorkerIP() string {
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "unknown"
}

func loadHistory() []ReviewRecord {
	data, err := os.ReadFile(historyFile)
	if err != nil {
		return nil
	}
	var records []ReviewRecord
	_ = json.Unmarshal(data, &records)
	return records
}

func saveHistory(records []ReviewRecord) error {
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(historyFile, data, 0644)
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0600)
}

func ensureRunnerStarted(forceReconfig bool) error {
	runnerMu.Lock()
	defer runnerMu.Unlock()

	if runnerCmd != nil && runnerCmd.Process != nil {
		// Check if still alive
		if err := runnerCmd.Process.Signal(syscall.Signal(0)); err == nil && !forceReconfig {
			log.Printf("GitHub Actions Runner is already active (pid=%d)", runnerCmd.Process.Pid)
			return nil
		}
	}

	_ = os.MkdirAll(runnerCfgDir, 0755)
	_ = os.MkdirAll(runnerWorkDir, 0755)

	cfgFiles := []string{".runner", ".credentials", ".credentials_rsaparams"}
	hasSavedCfg := true
	for _, f := range cfgFiles {
		if _, err := os.Stat(filepath.Join(runnerCfgDir, f)); err != nil {
			hasSavedCfg = false
			break
		}
	}

	if hasSavedCfg && !forceReconfig {
		log.Printf("Restoring persisted GitHub Runner credentials from durable volume %s...", runnerCfgDir)
		for _, f := range cfgFiles {
			_ = copyFile(filepath.Join(runnerCfgDir, f), filepath.Join(runnerHomeDir, f))
		}
	} else {
		log.Printf("Requesting fresh GitHub Runner registration token from %s...", getGatewayURL())
		resp, err := http.Get(getGatewayURL() + "/internal/runner-reg-token")
		if err != nil {
			return fmt.Errorf("failed to fetch reg token from gateway: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("gateway returned %d: %s", resp.StatusCode, string(body))
		}
		var tokResp struct {
			Token   string `json:"token"`
			RepoURL string `json:"repoUrl"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&tokResp); err != nil {
			return fmt.Errorf("failed to decode reg token: %w", err)
		}

		log.Printf("Configuring GitHub Actions Runner for %s...", tokResp.RepoURL)
		cfgCmd := exec.Command("./config.sh",
			"--unattended",
			"--replace",
			"--url", tokResp.RepoURL,
			"--token", tokResp.Token,
			"--name", "gh-runner-1",
			"--labels", "agent-substrate,gvisor",
			"--work", runnerWorkDir,
		)
		cfgCmd.Dir = runnerHomeDir
		cfgCmd.Env = append(os.Environ(), "RUNNER_ALLOW_RUNASROOT=1")
		cfgCmd.Stdout = os.Stdout
		cfgCmd.Stderr = os.Stderr
		if err := cfgCmd.Run(); err != nil {
			return fmt.Errorf("config.sh failed: %w", err)
		}

		for _, f := range cfgFiles {
			_ = copyFile(filepath.Join(runnerHomeDir, f), filepath.Join(runnerCfgDir, f))
		}
	}

	log.Printf("Starting GitHub Actions Runner (/runner/run.sh)...")
	cmd := exec.Command("./run.sh")
	cmd.Dir = runnerHomeDir
	cmd.Env = append(os.Environ(), "RUNNER_ALLOW_RUNASROOT=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start run.sh: %w", err)
	}
	runnerCmd = cmd

	go func(c *exec.Cmd) {
		err := c.Wait()
		log.Printf("GitHub Actions Runner process exited: %v", err)
		runnerMu.Lock()
		if runnerCmd == c {
			runnerCmd = nil
		}
		runnerMu.Unlock()
	}(cmd)

	return nil
}

func stopRunnerForSuspend() {
	runnerMu.Lock()
	defer runnerMu.Unlock()

	log.Printf("Stopping Runner.Listener cleanly before gVisor snapshot...")
	_ = exec.Command("pkill", "-TERM", "-f", "Runner.Listener").Run()
	_ = exec.Command("pkill", "-TERM", "-f", "run.sh").Run()
	if runnerCmd != nil && runnerCmd.Process != nil {
		_ = runnerCmd.Process.Signal(syscall.SIGTERM)
	}
	deadline := time.Now().Add(2500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if err := exec.Command("pgrep", "-f", "Runner.Listener").Run(); err != nil {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	_ = exec.Command("pkill", "-KILL", "-f", "Runner.Listener").Run()
	runnerCmd = nil
	log.Printf("Runner.Listener stopped; actor-daemon remains in RAM (wakeCount=%d, reviewCount=%d)",
		ramWakeCount.Load(), ramReviewCount.Load())
}

func buildTelemetry() TelemetryResponse {
	runnerMu.Lock()
	active := runnerCmd != nil && runnerCmd.Process != nil
	runnerMu.Unlock()

	hist := loadHistory()
	return TelemetryResponse{
		ActorName:      "gh-runner-1",
		Atespace:       "ate-demo-github",
		Kernel:         getKernelInfo(),
		WorkerIP:       getWorkerIP(),
		RAMWakeCount:   ramWakeCount.Load(),
		RAMReviewCount: ramReviewCount.Load(),
		DurableCount:   len(hist),
		RunnerActive:   active,
		History:        hist,
	}
}

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("/wake", func(w http.ResponseWriter, r *http.Request) {
		wakes := ramWakeCount.Add(1)
		force := r.URL.Query().Get("reconfig") == "1"
		log.Printf("Received /wake request (in-memory wake count = %d, forceReconfig = %v)", wakes, force)
		if err := ensureRunnerStarted(force); err != nil {
			log.Printf("Error starting runner: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(buildTelemetry())
	})

	mux.HandleFunc("/telemetry", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(buildTelemetry())
	})

	mux.HandleFunc("/record-review", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			PR   int    `json:"pr"`
			Repo string `json:"repo"`
			SHA  string `json:"sha"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		revCount := ramReviewCount.Add(1)
		wakes := ramWakeCount.Load()
		if wakes == 0 {
			wakes = ramWakeCount.Add(1)
		}

		hist := loadHistory()
		rec := ReviewRecord{
			PR:             req.PR,
			Repo:           req.Repo,
			SHA:            req.SHA,
			Timestamp:      time.Now().UTC(),
			RAMReviewCount: revCount,
			RAMWakeCount:   wakes,
			WorkerIP:       getWorkerIP(),
		}
		hist = append([]ReviewRecord{rec}, hist...)
		if len(hist) > 10 {
			hist = hist[:10]
		}
		if err := saveHistory(hist); err != nil {
			log.Printf("Warning: failed to save durable history: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(buildTelemetry())
	})

	mux.HandleFunc("/prepare-suspend", func(w http.ResponseWriter, r *http.Request) {
		stopRunnerForSuspend()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(buildTelemetry())
	})

	// Also allow triggering suspend-after-job via local proxy to runner-gateway
	mux.HandleFunc("/trigger-suspend", func(w http.ResponseWriter, r *http.Request) {
		resp, err := http.Post(getGatewayURL()+"/internal/suspend-after-job", "application/json", bytes.NewReader([]byte(`{}`)))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	})

	log.Printf("Starting Agent-Substrate GitHub Runner Actor Daemon on :80...")
	log.Fatal(http.ListenAndServe(":80", mux))
}
