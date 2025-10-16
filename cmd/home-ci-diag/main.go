package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	var (
		repoPathFlag = flag.String("repo", "", "Path to the repository to diagnose")
		helpFlag     = flag.Bool("help", false, "Show help")
	)
	flag.Parse()

	if *helpFlag {
		fmt.Println("Home-CI Repository Diagnostic Tool")
		fmt.Println("==================================")
		fmt.Println("")
		fmt.Println("Usage: home-ci-diag [options]")
		fmt.Println("")
		fmt.Println("Options:")
		flag.PrintDefaults()
		fmt.Println("")
		fmt.Println("Examples:")
		fmt.Println("  home-ci-diag -repo=/tmp/home-ci/e2e/dispatch-all/repo")
		fmt.Println("  home-ci-diag -repo=/path/to/my/project")
		return
	}

	if *repoPathFlag == "" {
		log.Fatal("❌ Repository path is required. Use -repo flag or -help for usage.")
	}

	// Validate repository path
	if _, err := os.Stat(*repoPathFlag); os.IsNotExist(err) {
		log.Fatalf("❌ Repository path does not exist: %s", *repoPathFlag)
	}

	// Check if it's a git repository
	gitDir := filepath.Join(*repoPathFlag, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		log.Fatalf("❌ Not a git repository: %s", *repoPathFlag)
	}

	log.Printf("🔍 Diagnosing repository: %s", *repoPathFlag)

	showGitBranches(*repoPathFlag)
	showProcessedCommits(*repoPathFlag)
	showHomeciState(*repoPathFlag)
}

// showGitBranches displays git branches from the repository
func showGitBranches(repoPath string) {
	fmt.Println("")
	fmt.Println("📊 Git branches:")

	cmd := exec.Command("git", "branch", "-a")
	cmd.Dir = repoPath
	if output, err := cmd.Output(); err == nil {
		fmt.Printf("%s", output)
	} else {
		fmt.Println("No branches found or git command failed")
	}
}

// showProcessedCommits displays commits that have been processed by home-ci
func showProcessedCommits(repoPath string) {
	fmt.Println("")
	fmt.Println("📋 Processed commits (JSON results):")

	homeciDir := filepath.Join(repoPath, ".home-ci")
	if _, err := os.Stat(homeciDir); os.IsNotExist(err) {
		fmt.Println("No .home-ci directory found")
		return
	}

	if files, err := filepath.Glob(filepath.Join(homeciDir, "*.json")); err == nil {
		var commits []string
		for _, file := range files {
			if filepath.Base(file) != "state.json" {
				// Extract branch and commit from filename like "20251016-192533_bugfix-timeout_a24b54c3.json"
				basename := filepath.Base(file)
				basename = strings.TrimSuffix(basename, ".json")
				parts := strings.Split(basename, "_")
				if len(parts) >= 3 {
					branch := parts[1]
					commit := parts[2]
					commits = append(commits, fmt.Sprintf("%s-%s", branch, commit))
				}
			}
		}
		if len(commits) > 0 {
			for _, commit := range commits {
				fmt.Println(commit)
			}
		} else {
			fmt.Println("No processed commits found")
		}
	} else {
		fmt.Println("No processed commits found")
	}
}

// showHomeciState displays the current state of home-ci for this repository
func showHomeciState(repoPath string) {
	fmt.Println("")
	fmt.Println("🏠 Home-CI State:")

	stateFile := filepath.Join(repoPath, ".home-ci", "state.json")
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		fmt.Println("No state.json found")
		return
	}

	if content, err := os.ReadFile(stateFile); err == nil {
		fmt.Printf("%s", content)
	} else {
		fmt.Printf("Error reading state.json: %v", err)
	}
}