# GitHub Dump

A microservice for cloning GitHub repositories and generating reports of their file contents and directory structure.

## Features

- Clone public or private GitHub repositories
- Generate directory tree structure
- Extract file contents from specified directories
- Include or exclude specific files or directories
- Support for Markdown, JSON, or plain text output
- Respect .gitignore rules and ignore Git metadata

## Installation

### Binary Installation

Download the appropriate binary for your system from the [Releases](https://github.com/sistemica/github-dump/releases) page.

**Linux (AMD64):**
```bash
curl -L -o github-dump https://github.com/sistemica/github-dump/releases/latest/download/github-dump-linux-amd64
chmod +x github-dump
```

**Linux (ARM64):**
```bash
curl -L -o github-dump https://github.com/sistemica/github-dump/releases/latest/download/github-dump-linux-arm64
chmod +x github-dump
```

**macOS (Intel):**
```bash
curl -L -o github-dump https://github.com/sistemica/github-dump/releases/latest/download/github-dump-darwin-amd64
chmod +x github-dump
```

**macOS (Apple Silicon):**
```bash
curl -L -o github-dump https://github.com/sistemica/github-dump/releases/latest/download/github-dump-darwin-arm64
chmod +x github-dump
```

### Docker Installation

Pull the image from GitHub Container Registry:

```bash
docker pull ghcr.io/sistemica/github-dump:latest
```

### Building from Source

```bash
git clone https://github.com/sistemica/github-dump.git
cd github-dump
go build
```

## Configuration

Create a `.env` file in the same directory as the binary:

```env
# GitHub credentials for accessing private repositories
GITHUB_TOKEN=your_github_personal_access_token

# Server configuration
PORT=8080

# File size limits (in bytes)
MAX_FILE_SIZE=10485760  # 10MB
```

## Usage

### Running as a Binary

```bash
./github-dump
```

The service will start on port 8080 (or the port specified in your .env file).

### Running with Docker

```bash
# Mount a .env file for configuration
docker run -p 8080:8080 \
  --env-file .env \
  ghcr.io/sistemica/github-dump:latest
```

Or with environment variables directly:

```bash
docker run -p 8080:8080 \
  -e GITHUB_TOKEN=your_github_personal_access_token \
  -e PORT=8080 \
  ghcr.io/sistemica/github-dump:latest
```

## API Endpoints

### POST /analyze

Analyzes a GitHub repository and returns the formatted output.

**URL Parameters:**
- `format`: (Optional) The response format. Available options:
  - `markdown` (default): Returns a Markdown document
  - `json`: Returns a JSON object with tree, contents, and markdown
  - `text`: Returns a plain text document with tree and file contents

**Request Body:**

```json
{
  "repo_url": "https://github.com/username/repo-name",
  "is_private": false,
  "dirs": [
    {
      "path": "src",
      "recursive": true
    },
    {
      "path": "docs",
      "recursive": false
    },
    {
      "path": "node_modules", 
      "exclude": true
    }
  ]
}
```

- `repo_url`: URL of the GitHub repository to clone
- `is_private`: Boolean indicating if the repository is private
- `dirs`: (Optional) Array of directories or files to include or exclude
  - `path`: Path to the directory or file relative to the repository root
  - `recursive`: Boolean indicating if subdirectories should be processed (defaults to true, applies only to directories)
  - `exclude`: Boolean indicating if this path should be excluded from analysis

If `dirs` is not provided, all files in the repository will be processed.

### Examples

#### Analyze a public repository (default markdown response)

```bash
curl -X POST http://localhost:8080/analyze \
  -H "Content-Type: application/json" \
  -d '{
    "repo_url": "https://github.com/username/repo-name",
    "is_private": false
  }' > repo-analysis.md
```

#### Analyze a specific file only

```bash
curl -X POST http://localhost:8080/analyze \
  -H "Content-Type: application/json" \
  -d '{
    "repo_url": "https://github.com/username/repo-name",
    "is_private": false,
    "dirs": [
      {
        "path": "README.md"
      }
    ]
  }' > readme-analysis.md
```

#### Exclude specific files only (includes everything except the specified files)

```bash
curl -X POST http://localhost:8080/analyze \
  -H "Content-Type: application/json" \
  -d '{
    "repo_url": "https://github.com/username/repo-name",
    "is_private": false,
    "dirs": [
      {
        "path": "package-lock.json",
        "exclude": true
      },
      {
        "path": "node_modules",
        "exclude": true
      }
    ]
  }' > repo-analysis.md
```

### GET /health

A simple health check endpoint that returns a 200 OK response if the service is running.

## License

[MIT License](LICENSE)