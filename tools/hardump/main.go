// Command hardump extracts RSC (Flight) response bodies from a Chrome HAR.
//
// Record a HAR by opening a profile in Chrome with DevTools > Network open
// (scroll to the bottom first so every card loads), then "Export HAR".
//
//	go run ./tools/hardump www.linkedin.com.har dump/
//	LINKEDIN_FIXTURES=dump/ go test ./internal/linkedin/ -run RealCapture
//
// HARs contain live session cookies. Never commit one.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type har struct {
	Log struct {
		Entries []struct {
			Request struct {
				URL string `json:"url"`
			} `json:"request"`
			Response struct {
				Content struct {
					Text     string `json:"text"`
					Encoding string `json:"encoding"`
				} `json:"content"`
			} `json:"response"`
		} `json:"entries"`
	} `json:"log"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: hardump <file.har> <outdir>")
		os.Exit(2)
	}
	raw, err := os.ReadFile(os.Args[1])
	check(err)
	var parsed har
	check(json.Unmarshal(raw, &parsed))
	check(os.MkdirAll(os.Args[2], 0o755))

	written := 0
	for i, entry := range parsed.Log.Entries {
		text := entry.Response.Content.Text
		if text == "" {
			continue
		}
		body := []byte(text)
		if entry.Response.Content.Encoding == "base64" {
			body, err = base64.StdEncoding.DecodeString(text)
			if err != nil {
				continue
			}
		}
		if !looksLikeFlight(body) {
			continue
		}
		name := label(entry.Request.URL, i)
		check(os.WriteFile(filepath.Join(os.Args[2], name+".flight"), body, 0o644))
		fmt.Printf("%3d  %9d  %s\n", i, len(body), name)
		written++
	}
	fmt.Printf("\n%d Flight responses -> %s\n", written, os.Args[2])
}

func looksLikeFlight(body []byte) bool {
	head := body
	if len(head) > 2000 {
		head = head[:2000]
	}
	trimmed := strings.TrimLeft(string(head), " \n\r\t")
	return strings.HasPrefix(trimmed, "0:") || strings.HasPrefix(trimmed, "1:") ||
		strings.HasPrefix(trimmed, "2:") || strings.Contains(string(head), ":I[")
}

func label(url string, index int) string {
	if _, after, found := strings.Cut(url, "componentId="); found {
		component, _, _ := strings.Cut(after, "&")
		return component[strings.LastIndex(component, ".")+1:]
	}
	if strings.Contains(url, "/flagship-web/in/") {
		return "shell"
	}
	return fmt.Sprintf("%02d-other", index)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
