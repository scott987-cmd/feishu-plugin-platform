// Command shortcutgen compiles a field-shortcut DSL (JSON) into a buildable
// basekit project. Reads the DSL from a file argument or stdin.
//
//	go run ./cmd/shortcutgen -out /tmp/exchange-rate internal/shortcut/testdata/exchange_rate.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/dushibing/feishu-plugin-platform/internal/generator"
	"github.com/dushibing/feishu-plugin-platform/internal/shortcut"
)

func main() {
	out := flag.String("out", "", "output directory for the scaffolded project (required)")
	nl := flag.String("nl", "", "natural-language request; generate the DSL via DeepSeek instead of reading JSON (needs DEEPSEEK_API_KEY)")
	dump := flag.Bool("dump", false, "also print the generated DSL JSON to stderr")
	action := flag.Bool("action", false, "treat the JSON input as an automation Action (addAction) instead of a field shortcut")
	flag.Parse()

	if *out == "" {
		fmt.Fprintln(os.Stderr, "error: -out is required")
		os.Exit(2)
	}

	// Action track: read an Action DSL JSON (or generate via NL) and scaffold.
	if *action {
		var a shortcut.Action
		if *nl != "" {
			gen, ok, err := generator.GenerateAction(*nl)
			if err != nil {
				fmt.Fprintln(os.Stderr, "generation error:", err)
				os.Exit(1)
			}
			if !ok {
				fmt.Fprintln(os.Stderr, "generation unavailable (set DEEPSEEK_API_KEY)")
				os.Exit(1)
			}
			a = gen
		} else {
			data, err := readInput(flag.Arg(0))
			if err != nil {
				fmt.Fprintln(os.Stderr, "read error:", err)
				os.Exit(1)
			}
			if err := json.Unmarshal(data, &a); err != nil {
				fmt.Fprintln(os.Stderr, "json error:", err)
				os.Exit(1)
			}
		}
		if *dump {
			bts, _ := json.MarshalIndent(a, "", "  ")
			fmt.Fprintln(os.Stderr, string(bts))
		}
		if err := a.Validate(); err != nil {
			fmt.Fprintln(os.Stderr, "invalid action DSL:\n", err)
			os.Exit(1)
		}
		if err := shortcut.ScaffoldAction(a, *out); err != nil {
			fmt.Fprintln(os.Stderr, "scaffold error:", err)
			os.Exit(1)
		}
		fmt.Printf("scaffolded action %q -> %s\n", a.Title.ZhCN, *out)
		return
	}

	var f shortcut.FieldShortcut

	if *nl != "" {
		// Natural-language track: NL → DSL via DeepSeek (forced tool call + repair).
		gen, ok, err := generator.GenerateShortcut(*nl)
		if err != nil {
			fmt.Fprintln(os.Stderr, "generation error:", err)
			os.Exit(1)
		}
		if !ok {
			fmt.Fprintln(os.Stderr, "generation unavailable (set DEEPSEEK_API_KEY)")
			os.Exit(1)
		}
		f = gen
	} else {
		// JSON track: read a hand-written / pre-generated DSL.
		var data []byte
		var err error
		if arg := flag.Arg(0); arg != "" {
			data, err = os.ReadFile(arg)
		} else {
			data, err = io.ReadAll(os.Stdin)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "read error:", err)
			os.Exit(1)
		}
		if err := json.Unmarshal(data, &f); err != nil {
			fmt.Fprintln(os.Stderr, "json error:", err)
			os.Exit(1)
		}
	}

	if err := f.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "invalid DSL:\n", err)
		os.Exit(1)
	}
	if *dump {
		b, _ := json.MarshalIndent(f, "", "  ")
		fmt.Fprintln(os.Stderr, string(b))
	}
	if err := shortcut.Scaffold(f, *out); err != nil {
		fmt.Fprintln(os.Stderr, "scaffold error:", err)
		os.Exit(1)
	}
	fmt.Printf("scaffolded %q -> %s\n", f.Title.ZhCN, *out)
}

// readInput reads from a file path or, if empty, stdin.
func readInput(arg string) ([]byte, error) {
	if arg != "" {
		return os.ReadFile(arg)
	}
	return io.ReadAll(os.Stdin)
}
