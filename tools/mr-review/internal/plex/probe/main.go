// Command probe launches tuicr in a PTY for a few seconds (diagnostics only).
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"mr-review/internal/plex"
)

func main() {
	wt, variant, base, head := os.Args[1], os.Args[2], os.Args[3], os.Args[4]
	var args []string
	switch variant {
	case "twodot":
		args = []string{"-r", base + ".." + head}
	case "threedot":
		args = []string{"-r", base + "..." + head}
	case "single":
		args = []string{"-r", base}
	case "headonly":
		args = []string{"-r", head}
	case "pr":
		url := os.Args[5]
		if variant == "pr" && len(os.Args) > 6 && os.Args[6] == "range" {
			args = []string{"pr", url, "-r", base + ".." + head}
		} else {
			args = []string{"pr", url}
		}
	}
	p, err := plex.Start(context.Background(), wt, 100, 30, nil, "tuicr", args...)
	if err != nil {
		fmt.Println("ERR:", err)
		os.Exit(1)
	}
	time.Sleep(60 * time.Second)
	fmt.Println("spawned", variant)
	fmt.Println("--- pane screen at exit ---")
	fmt.Println(p.Render())
}
