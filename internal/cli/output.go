// Package cli implements SnipGo's command-line interface.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

// Failure contains only messages intended for the caller.
type Failure struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

type response struct {
	OK      bool     `json:"ok"`
	Command string   `json:"command"`
	Data    any      `json:"data,omitempty"`
	Error   *Failure `json:"error,omitempty"`
}

func exitCode(f *Failure) int {
	if f == nil {
		return 0
	}
	switch f.Code {
	case "INVALID_ARGS":
		return 2
	case "PERMISSION_DENIED", "AGENT_IPC_DISABLED":
		return 3
	case "HOST_NOT_RUNNING":
		return 4
	case "NOT_FOUND":
		return 5
	case "CANCELED":
		return 130
	default:
		return 6
	}
}

func writeResponse(stdout, stderr io.Writer, command string, data any, failure *Failure, asJSON bool) int {
	if failure != nil {
		data = nil
	}
	if asJSON {
		if err := json.NewEncoder(stdout).Encode(response{failure == nil, command, data, failure}); err != nil {
			fmt.Fprintln(stderr, err)
			return 6
		}
	} else if failure != nil {
		fmt.Fprintln(stderr, failure.Message)
	} else {
		if result, ok := data.(map[string]any); ok && command == "ocr" {
			key := "text"
			if result["has_table"] == true {
				switch result["format"] {
				case "markdown":
					key = "table_markdown"
				case "tsv":
					key = "table_tsv"
				}
			}
			fmt.Fprintln(stdout, result[key])
		} else {
			pretty := json.NewEncoder(stdout)
			pretty.SetIndent("", "  ")
			if err := pretty.Encode(data); err != nil {
				return 6
			}
		}
	}
	return exitCode(failure)
}
