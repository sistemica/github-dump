package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// DirRequest represents a directory or file to be processed
type DirRequest struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive,omitempty"`
	Exclude   bool   `json:"exclude,omitempty"`
}

// RepoRequest represents the request payload for cloning a repository
type RepoRequest struct {
	RepoURL   string       `json:"repo_url"`
	IsPrivate bool         `json:"is_private"`
	Dirs      []DirRequest `json:"dirs,omitempty"`
}

// RepoResponse represents the response with file content and directory tree
type RepoResponse struct {
	Tree     string            `json:"tree"`
	Contents map[string]string `json:"contents"`
	Markdown string            `json:"markdown"`
}

const (
	tempDir     = "./temp_repos"
	outputDir   = "./output"
	maxFileSize = 10 * 1024 * 1024 // 10MB limit for file content
)

func main() {
	// Configure logging with timestamps
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Starting GitHub Repository Analyzer microservice")

	// Load environment variables from .env file
	err := godotenv.Load()
	if err != nil {
		log.Println("Warning: Error loading .env file:", err)
	} else {
		log.Println("Successfully loaded .env configuration")
	}

	// Create required directories
	if err := createDirectories(); err != nil {
		log.Fatalf("Failed to create directories: %v", err)
	}
	log.Println("Temporary directories created successfully")

	// Set up HTTP handlers with logging middleware
	http.HandleFunc("/analyze", loggingMiddleware(handleAnalyzeRepo))
	http.HandleFunc("/health", loggingMiddleware(handleHealthCheck))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server started on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// loggingMiddleware logs HTTP requests
func loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()
		log.Printf("Request received: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)

		// Create a response wrapper to capture status code
		rw := &responseWriter{w, http.StatusOK}
		next(rw, r)

		duration := time.Since(startTime)
		log.Printf("Request completed: %s %s - Status: %d - Duration: %v",
			r.Method, r.URL.Path, rw.statusCode, duration)
	}
}

// responseWriter is a wrapper for http.ResponseWriter that captures the status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader captures the status code before writing the header
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// handleHealthCheck provides a simple health check endpoint
func handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"healthy"}`))
}

// createDirectories creates necessary directories for the application
func createDirectories() error {
	dirs := []string{tempDir, outputDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}

// handleAnalyzeRepo handles the HTTP request to analyze a GitHub repository
func handleAnalyzeRepo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var req RepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Error parsing request body: %v", err)
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate request
	if req.RepoURL == "" {
		log.Printf("Invalid request: missing repository URL")
		http.Error(w, "Repository URL is required", http.StatusBadRequest)
		return
	}

	// Get the format parameter (default to markdown)
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "markdown" // Default to markdown
	}
	log.Printf("Requested response format: %s", format)

	// Create a unique directory for this repository
	repoID := fmt.Sprintf("%d", time.Now().UnixNano())
	repoDir := filepath.Join(tempDir, repoID)
	defer cleanupRepo(repoDir) // Clean up after processing

	// Clone the repository
	log.Printf("Cloning repository: %s", req.RepoURL)
	if err := cloneRepo(req.RepoURL, repoDir, req.IsPrivate); err != nil {
		log.Printf("Failed to clone repository: %v", err)
		http.Error(w, "Failed to clone repository: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Generate repository analysis
	log.Printf("Analyzing repository...")
	resp, err := analyzeRepo(repoDir, req.Dirs)
	if err != nil {
		log.Printf("Failed to analyze repository: %v", err)
		http.Error(w, "Failed to analyze repository: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Check if we have any file contents
	if len(resp.Contents) == 0 {
		log.Printf("Warning: No file contents were collected, possibly due to exclusion rules")
	}

	// Save output to a file
	if err := saveOutputToFile(repoID, resp); err != nil {
		log.Printf("Warning: Failed to save output to file: %v", err)
	}

	// Return the response based on requested format
	switch format {
	case "json":
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)

	case "text", "txt":
		w.Header().Set("Content-Type", "text/plain")
		repoName := extractRepoName(req.RepoURL)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-analysis.txt", repoName))

		// First add the directory tree
		w.Write([]byte("# Directory Tree\n\n" + resp.Tree + "\n\n"))
		// Then add the markdown content without the initial heading
		markdownWithoutHeader := strings.Replace(resp.Markdown, "# Repository Analysis\n\n", "# File Contents\n\n", 1)
		w.Write([]byte(markdownWithoutHeader))

	default: // markdown or any other value defaults to markdown
		w.Header().Set("Content-Type", "text/markdown")
		repoName := extractRepoName(req.RepoURL)
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-analysis.md", repoName))
		w.Write([]byte(resp.Markdown))
	}

	log.Printf("Response sent successfully with %d files", len(resp.Contents))
}

// extractRepoName extracts a repository name from its URL
func extractRepoName(repoURL string) string {
	// Remove trailing .git if present
	repoURL = strings.TrimSuffix(repoURL, ".git")

	// Extract the last part of the URL path
	parts := strings.Split(repoURL, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}

	return "repo"
}

// cloneRepo clones a GitHub repository to the specified directory
func cloneRepo(repoURL, repoDir string, isPrivate bool) error {
	log.Printf("Cloning repository %s to %s", repoURL, repoDir)

	// Construct git clone command
	// Use --config core.autocrlf=input to normalize line endings
	cmd := exec.Command("git", "clone", "--config", "core.autocrlf=input", repoURL, repoDir)

	// If it's a private repository, set up authentication
	if isPrivate {
		githubToken := os.Getenv("GITHUB_TOKEN")
		if githubToken == "" {
			return fmt.Errorf("GITHUB_TOKEN environment variable not set for private repository")
		}

		// Format URL with token for authentication
		parsedURL := strings.Replace(repoURL, "https://", fmt.Sprintf("https://%s@", githubToken), 1)
		cmd = exec.Command("git", "clone", "--config", "core.autocrlf=input", parsedURL, repoDir)
	}

	// Execute the command
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %v - %s", err, string(output))
	}

	return nil
}

// analyzeRepo analyzes a repository and returns its directory tree and file contents
func analyzeRepo(repoDir string, dirRequests []DirRequest) (*RepoResponse, error) {
	// Generate directory tree
	tree, err := generateDirectoryTree(repoDir)
	if err != nil {
		return nil, fmt.Errorf("failed to generate directory tree: %v", err)
	}

	// Extract file contents
	contents, err := extractFileContents(repoDir, dirRequests)
	if err != nil {
		return nil, fmt.Errorf("failed to extract file contents: %v", err)
	}

	// Generate markdown document with tree included
	markdown := generateMarkdownDocument(tree, contents)

	return &RepoResponse{
		Tree:     tree,
		Contents: contents,
		Markdown: markdown,
	}, nil
}

// generateMarkdownDocument creates a markdown document with directory tree and all file contents
func generateMarkdownDocument(tree string, contents map[string]string) string {
	var builder strings.Builder

	// Add title
	builder.WriteString("# Repository Analysis\n\n")

	// Add directory tree
	builder.WriteString("## Directory Tree\n\n```\n")
	builder.WriteString(tree)
	builder.WriteString("\n```\n\n")

	// Add file contents section
	builder.WriteString("## File Contents\n\n")

	// Get sorted keys for consistent output
	keys := make([]string, 0, len(contents))
	for k := range contents {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Add each file with markdown formatting
	for _, path := range keys {
		content := contents[path]

		// Add file header with horizontal rule
		builder.WriteString("---\n\n")
		builder.WriteString(fmt.Sprintf("### %s\n\n", path))

		// Determine language for syntax highlighting
		language := determineLanguage(path)

		// Add content with code fence
		if language != "" {
			builder.WriteString(fmt.Sprintf("```%s\n%s\n```\n\n", language, content))
		} else {
			builder.WriteString(fmt.Sprintf("```\n%s\n```\n\n", content))
		}
	}

	return builder.String()
}

// determineLanguage determines the language for syntax highlighting based on file extension
func determineLanguage(path string) string {
	extension := strings.ToLower(filepath.Ext(path))

	// Common file extensions mapped to their languages
	extensionMap := map[string]string{
		".go":    "go",
		".js":    "javascript",
		".py":    "python",
		".java":  "java",
		".sh":    "bash",
		".md":    "markdown",
		".html":  "html",
		".css":   "css",
		".json":  "json",
		".yaml":  "yaml",
		".yml":   "yaml",
		".xml":   "xml",
		".sql":   "sql",
		".c":     "c",
		".cpp":   "cpp",
		".h":     "c",
		".ts":    "typescript",
		".rb":    "ruby",
		".php":   "php",
		".rs":    "rust",
		".swift": "swift",
		".kt":    "kotlin",
	}

	if language, ok := extensionMap[extension]; ok {
		return language
	}

	return ""
}

// generateDirectoryTree generates a text representation of the repository directory structure
func generateDirectoryTree(repoDir string) (string, error) {
	log.Printf("Generating directory tree for %s", repoDir)

	// Use the external 'tree' command or a custom implementation
	cmd := exec.Command("tree", "-a", "-I", ".git", "--gitignore", repoDir)
	output, err := cmd.CombinedOutput()

	// If 'tree' command is not available, use a custom implementation
	if err != nil {
		log.Println("External 'tree' command failed, using custom implementation")
		return generateCustomDirectoryTree(repoDir)
	}

	return string(output), nil
}

// generateCustomDirectoryTree creates a directory tree structure without external dependencies
func generateCustomDirectoryTree(rootDir string) (string, error) {
	var builder strings.Builder
	rootDirName := filepath.Base(rootDir)
	builder.WriteString(rootDirName + "\n")

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Continue despite errors
		}

		// Skip .git directory and all git-related files
		if info.IsDir() && (info.Name() == ".git" || strings.HasPrefix(info.Name(), ".git")) {
			return filepath.SkipDir
		}

		// Skip git-related files
		if strings.HasPrefix(info.Name(), ".git") {
			return nil
		}

		// Skip the root directory itself
		if path == rootDir {
			return nil
		}

		// Calculate depth and indentation
		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return nil
		}

		depth := len(strings.Split(rel, string(os.PathSeparator)))
		indent := strings.Repeat("│   ", depth-1)

		// Add the appropriate prefix
		if info.IsDir() {
			builder.WriteString(indent + "├── " + info.Name() + "/\n")
		} else {
			builder.WriteString(indent + "├── " + info.Name() + "\n")
		}

		return nil
	})

	return builder.String(), err
}

// shouldIgnoreFile checks if a file should be ignored based on .gitignore rules
func shouldIgnoreFile(path string) bool {
	// Always skip .git directory and all subdirectories/files
	if strings.Contains(path, "/.git/") || strings.HasSuffix(path, "/.git") || strings.HasPrefix(filepath.Base(path), ".git") {
		return true
	}

	// Basic check for binary files (could be improved)
	ext := strings.ToLower(filepath.Ext(path))
	binaryExtensions := []string{".exe", ".dll", ".so", ".dylib", ".obj", ".o", ".a", ".lib",
		".bin", ".dat", ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".tiff", ".ico",
		".mp3", ".mp4", ".mov", ".avi", ".wav", ".flac", ".zip", ".tar", ".gz", ".7z", ".rar"}

	for _, binaryExt := range binaryExtensions {
		if ext == binaryExt {
			return true
		}
	}

	return false
}

// extractFileContents extracts the contents of the files in the specified directories
func extractFileContents(repoDir string, dirRequests []DirRequest) (map[string]string, error) {
	contents := make(map[string]string)

	// Collect exclude paths (directories and files)
	var excludePaths []string
	var includePaths []DirRequest

	// Separate include and exclude paths
	for _, dirReq := range dirRequests {
		if dirReq.Exclude {
			excludePaths = append(excludePaths, dirReq.Path)
		} else {
			includePaths = append(includePaths, dirReq)
		}
	}

	// Always exclude .git directory
	excludePaths = append(excludePaths, ".git")

	// If we only have exclusions but no inclusions, include everything except exclusions
	if len(includePaths) == 0 {
		includePaths = append(includePaths, DirRequest{
			Path:      ".",
			Recursive: true,
		})
	}

	// Process include directories/files
	for _, dirReq := range includePaths {
		fullPath := filepath.Join(repoDir, dirReq.Path)

		// Check if path exists
		fileInfo, err := os.Stat(fullPath)
		if os.IsNotExist(err) {
			continue
		}

		// Handle file vs directory differently
		if !fileInfo.IsDir() {
			// It's a single file
			relPath, err := filepath.Rel(repoDir, fullPath)
			if err != nil {
				continue
			}

			// Check if file should be excluded by name
			shouldExclude := false
			for _, excludePath := range excludePaths {
				if relPath == excludePath {
					shouldExclude = true
					break
				}
			}

			if shouldExclude || shouldIgnoreFile(fullPath) || fileInfo.Size() > maxFileSize {
				continue
			}

			// Read file content
			data, err := ioutil.ReadFile(fullPath)
			if err == nil {
				contents[relPath] = string(data)
			}
			continue
		}

		// It's a directory, walk it
		recursive := dirReq.Recursive != false

		filepath.Walk(fullPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // Continue to other files
			}

			// Skip if it's a directory
			if info.IsDir() {
				// Skip .git directory
				if info.Name() == ".git" || strings.HasPrefix(info.Name(), ".git") {
					return filepath.SkipDir
				}

				// Skip subdirectories if non-recursive
				if !recursive && path != fullPath {
					return filepath.SkipDir
				}

				return nil
			}

			// Get relative path for comparison
			relPath, err := filepath.Rel(repoDir, path)
			if err != nil {
				return nil
			}

			// Skip .git files
			if strings.HasPrefix(relPath, ".git/") || strings.Contains(relPath, "/.git/") {
				return nil
			}

			// Check if path should be excluded - either exact match or in excluded directory
			for _, excludePath := range excludePaths {
				if relPath == excludePath || strings.HasPrefix(relPath, excludePath+"/") {
					return nil
				}
			}

			// Skip binary and large files
			if shouldIgnoreFile(path) || info.Size() > maxFileSize {
				return nil
			}

			// Read file content
			data, err := ioutil.ReadFile(path)
			if err != nil {
				return nil
			}

			contents[relPath] = string(data)
			return nil
		})
	}

	return contents, nil
}

// cleanupRepo removes the temporary repository directory
func cleanupRepo(repoDir string) {
	log.Printf("Cleaning up repository at %s", repoDir)
	if err := os.RemoveAll(repoDir); err != nil {
		log.Printf("Failed to remove directory %s: %v", repoDir, err)
	}
}

// saveOutputToFile saves the analysis output to a file
func saveOutputToFile(repoID string, resp *RepoResponse) error {
	// Save tree to a text file
	treeFile := filepath.Join(outputDir, repoID+"_tree.txt")
	if err := ioutil.WriteFile(treeFile, []byte(resp.Tree), 0644); err != nil {
		return fmt.Errorf("failed to save tree file: %v", err)
	}
	log.Printf("Tree output saved to %s", treeFile)

	// Save markdown to a markdown file
	mdFile := filepath.Join(outputDir, repoID+"_analysis.md")
	if err := ioutil.WriteFile(mdFile, []byte(resp.Markdown), 0644); err != nil {
		return fmt.Errorf("failed to save markdown file: %v", err)
	}
	log.Printf("Markdown output saved to %s", mdFile)

	// Save full JSON response as well
	jsonData, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON response: %v", err)
	}

	jsonFile := filepath.Join(outputDir, repoID+"_response.json")
	if err := ioutil.WriteFile(jsonFile, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to save JSON file: %v", err)
	}
	log.Printf("JSON output saved to %s", jsonFile)

	return nil
}
