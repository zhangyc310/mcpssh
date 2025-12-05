# Project Overview
`mcpssh` is a Go-based Model Context Protocol (MCP) server designed to facilitate SSH session management for AI agents. It enables an AI to interact with remote servers via SSH by creating persistent sessions, sending commands, and reading outputs through a pseudo-terminal (PTY) interface.


## Architecture & Key Components
The project consists of a single main file `mcpssh.go` which defines the MCP server and its tools.

### Core Tools
- **`start_session`**: Initiates a new SSH connection to a specified host using a PTY. Returns a `session_id`.
- **`interact_session`**: Sends input to and reads output from an active SSH session. This allows for interactive shell usage (e.g., handling prompts, running scripts).
- **`close_session`**: Terminates an active SSH session and cleans up resources.

### Dependencies
- `github.com/mark3labs/mcp-go`: MCP server SDK.
- `github.com/creack/pty`: PTY management for interactive SSH sessions.
- `github.com/google/uuid`: Session ID generation.

## Building and Running

### Prerequisites
- Go 1.25.0 or later.
- ssh  installed on system. I'm using it in macos.
- `~/.ssh/config` file 

### Build Command
```bash
go build -o mcpssh
```
## Install
###  codex mcp add mcpssh -- /your_path_to_mcpssh/mcpssh
### gemini edit file `~/.gemini/settings.json`
```
"mcpServers": {
    "chrome-devtools": {
      "command": "npx",
      "args": ["chrome-devtools-mcp@latest"]
    },
    "ssh": {
      "command": "/your_path_to_mcpssh/mcpssh",
      "args": []
    }
  },
```
