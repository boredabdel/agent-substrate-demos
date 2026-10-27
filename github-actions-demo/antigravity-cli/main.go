package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	localActorDaemon = "http://127.0.0.1:80"
	defaultGateway   = "http://runner-gateway.ate-demo-github.svc.cluster.local:8080"
)

func getGatewayURL() string {
	if v := os.Getenv("RUNNER_GATEWAY_URL"); v != "" {
		return v
	}
	return defaultGateway
}

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
	ActorName      string         `json:"actorName"`
	Atespace       string         `json:"atespace"`
	Kernel         string         `json:"kernel"`
	WorkerIP       string         `json:"workerIp"`
	RAMWakeCount   int64          `json:"ramWakeCount"`
	RAMReviewCount int64          `json:"ramReviewCount"`
	DurableCount   int            `json:"durableCount"`
	RunnerActive   bool           `json:"runnerActive"`
	History        []ReviewRecord `json:"history"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: antigravity-cli <review|suspend-after-job> [flags]")
		os.Exit(1)
	}

	subcmd := os.Args[1]
	switch subcmd {
	case "review":
		fs := flag.NewFlagSet("review", flag.ExitOnError)
		repo := fs.String("repo", os.Getenv("GITHUB_REPOSITORY"), "GitHub owner/repo")
		pr := fs.Int("pr", 0, "Pull request number")
		sha := fs.String("sha", os.Getenv("GITHUB_SHA"), "Commit SHA")
		_ = fs.Parse(os.Args[2:])

		if *repo == "" || *pr == 0 {
			fmt.Fprintln(os.Stderr, "Error: --repo and --pr are required")
			os.Exit(1)
		}
		if err := runReview(*repo, *pr, *sha); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "suspend-after-job":
		if err := runSuspendAfterJob(); err != nil {
			fmt.Fprintf(os.Stderr, "Error scheduling suspend: %v\n", err)
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand %q\n", subcmd)
		os.Exit(1)
	}
}

func getLocalGitDiff() string {
	out, err := exec.Command("git", "diff", "HEAD~1..HEAD").Output()
	if err == nil && len(out) > 0 {
		return string(out)
	}
	return ""
}

func runReview(repo string, pr int, sha string) error {
	fmt.Println("╔══════════════════════════════════════════════════════════════════╗")
	fmt.Println("║   🚀 Antigravity CLI v1.4.0 (Agent-Substrate GKE Edition)        ║")
	fmt.Println("║   Model: Vertex AI Gemini 2.5 Flash (boredabdel-lab)             ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════════╝")
	fmt.Printf("-> Target Repository : %s\n", repo)
	fmt.Printf("-> Pull Request      : #%d (commit %s)\n", pr, sha)

	// 1. Record the review in the local Actor Daemon (increments both RAM & durable volume state)
	recPayload, _ := json.Marshal(map[string]any{
		"repo": repo,
		"pr":   pr,
		"sha":  sha,
	})
	resp, err := http.Post(localActorDaemon+"/record-review", "application/json", bytes.NewReader(recPayload))
	if err != nil {
		return fmt.Errorf("failed to contact local actor-daemon: %w", err)
	}
	defer resp.Body.Close()

	var telemetry TelemetryResponse
	if err := json.NewDecoder(resp.Body).Decode(&telemetry); err != nil {
		return fmt.Errorf("failed to decode actor telemetry: %w", err)
	}

	fmt.Println("\n[1/3] Agent-Substrate Actor Telemetry (Restored from GCS Snapshot):")
	fmt.Printf("      • Actor Identity       : %s/%s\n", telemetry.Atespace, telemetry.ActorName)
	fmt.Printf("      • Sandbox Kernel       : %s\n", telemetry.Kernel)
	fmt.Printf("      • Worker Pod IP        : %s\n", telemetry.WorkerIP)
	fmt.Printf("      • Preserved RAM Wakes  : %d (survives Full memory checkpoint/restore)\n", telemetry.RAMWakeCount)
	fmt.Printf("      • Preserved RAM Reviews: %d (in-memory counter)\n", telemetry.RAMReviewCount)
	fmt.Printf("      • Durable Disk History : %d entries in /home/runner/state/history.json\n", telemetry.DurableCount)

	// 2. Request Vertex AI Gemini review + GitHub PR comment via runner-gateway
	fmt.Println("\n[2/3] Analyzing PR diff with Vertex AI Gemini 2.5 Flash...")
	reviewReq, _ := json.Marshal(map[string]any{
		"repo":      repo,
		"pr":        pr,
		"sha":       sha,
		"localDiff": getLocalGitDiff(),
		"telemetry": telemetry,
	})

	client := &http.Client{Timeout: 60 * time.Second}
	gwResp, err := client.Post(getGatewayURL()+"/internal/ai-review", "application/json", bytes.NewReader(reviewReq))
	if err != nil {
		return fmt.Errorf("failed to call runner-gateway /internal/ai-review: %w", err)
	}
	defer gwResp.Body.Close()

	bodyBytes, _ := io.ReadAll(gwResp.Body)
	if gwResp.StatusCode != http.StatusOK {
		return fmt.Errorf("runner-gateway returned %d: %s", gwResp.StatusCode, string(bodyBytes))
	}

	var gwResult struct {
		ReviewMarkdown string `json:"reviewMarkdown"`
		CommentURL     string `json:"commentUrl"`
		Model          string `json:"model"`
	}
	if err := json.Unmarshal(bodyBytes, &gwResult); err != nil {
		return fmt.Errorf("failed to parse ai-review response: %w", err)
	}

	fmt.Println("\n[3/3] Antigravity AI Code Review Output:")
	fmt.Println(strings.Repeat("─", 68))
	fmt.Println(gwResult.ReviewMarkdown)
	fmt.Println(strings.Repeat("─", 68))
	fmt.Printf("✅ Posted PR Review Comment to GitHub: %s\n", gwResult.CommentURL)
	return nil
}

func runSuspendAfterJob() error {
	fmt.Println("⏳ Scheduling automatic Actor suspend back to GCS (8s grace period for runner completion)...")
	resp, err := http.Post(localActorDaemon+"/trigger-suspend", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("✅ %s\n", strings.TrimSpace(string(body)))
	return nil
}
