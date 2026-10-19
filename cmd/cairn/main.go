package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"cairn/internal/engine"
	"cairn/internal/store"
	"cairn/internal/tui"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "cairn: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	st, err := store.Open(store.DefaultRootDir())
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	eng := engine.New(st)

	if len(args) == 0 {
		if _, err := st.SyncIndex(false); err != nil {
			return err
		}
		return tui.Run(st, eng, "")
	}

	switch args[0] {
	case "drop":
		return runDrop(st, args[1:])
	case "query":
		return runQuery(st, eng, args[1:])
	case "blaze":
		return runBlaze(st, args[1:])
	case "index":
		return runIndex(st, args[1:])
	case "-h", "--help", "help":
		printUsage()
		return nil
	default:
		if strings.HasPrefix(args[0], "-") {
			printUsage()
			return fmt.Errorf("unknown flag %q", args[0])
		}
		if _, err := st.SyncIndex(false); err != nil {
			return err
		}
		return tui.Run(st, eng, args[0])
	}
}

func runDrop(st *store.Store, args []string) error {
	fs := flag.NewFlagSet("drop", flag.ContinueOnError)
	useStdin := fs.Bool("stdin", false, "Read stone content from stdin")
	tether := fs.String("tether", "", "Tether first created stone to an existing note ID")
	if err := fs.Parse(reorderFlagsFirst(args)); err != nil {
		return err
	}

	var raw string
	if *useStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		raw = string(data)
	} else {
		tmp, err := os.CreateTemp("", "cairn-drop-*.md")
		if err != nil {
			return fmt.Errorf("create temp file: %w", err)
		}
		tmpPath := tmp.Name()
		_ = tmp.Close()
		defer func() { _ = os.Remove(tmpPath) }()

		editor := strings.TrimSpace(os.Getenv("EDITOR"))
		if editor == "" {
			editor = "vi"
		}
		cmd := exec.Command(editor, tmpPath)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("editor failed: %w", err)
		}

		data, err := os.ReadFile(tmpPath)
		if err != nil {
			return fmt.Errorf("read temp file: %w", err)
		}
		raw = string(data)
	}

	if strings.TrimSpace(raw) == "" {
		return nil
	}

	createdIDs, err := st.CleaveAndWrite(raw, *tether)
	if err != nil {
		return err
	}
	if _, err := st.SyncIndex(false); err != nil {
		return err
	}
	for _, id := range createdIDs {
		fmt.Println(id)
	}
	return nil
}

func runQuery(st *store.Store, eng *engine.Engine, args []string) error {
	fs := flag.NewFlagSet("query", flag.ContinueOnError)
	limit := fs.Int("limit", 8, "Maximum number of results to return")
	asJSON := fs.Bool("json", false, "Output indented JSON")
	if err := fs.Parse(reorderFlagsFirst(args)); err != nil {
		return err
	}

	queryText := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if _, err := st.SyncIndex(false); err != nil {
		return err
	}

	hits := eng.QueryHorizon(queryText, nil, *limit)
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(hits)
	}

	for idx, h := range hits {
		fmt.Printf("<!-- stone:%s | category:%s | score:%.4f -->\n%s\n",
			h.Note.ID, h.Category, h.Score, strings.TrimSpace(h.Note.Body))
		if idx < len(hits)-1 {
			fmt.Println()
		}
	}
	return nil
}

func runBlaze(st *store.Store, args []string) error {
	if _, err := st.SyncIndex(false); err != nil {
		return err
	}
	trailName := ""
	if len(args) > 0 {
		trailName = strings.TrimSpace(args[0])
	}
	sess, err := st.LoadSession(trailName)
	if err != nil {
		return err
	}
	compiled, err := st.WriteTrailFile(sess)
	if err != nil {
		return err
	}
	fmt.Print(compiled)
	return nil
}

func runIndex(st *store.Store, args []string) error {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	rebuild := fs.Bool("rebuild", false, "Rebuild the SQLite index from scratch")
	if err := fs.Parse(reorderFlagsFirst(args)); err != nil {
		return err
	}

	count, err := st.SyncIndex(*rebuild)
	if err != nil {
		return err
	}
	fmt.Printf("Indexed %d notes into %s/quarry.db\n", count, st.RootDir)
	return nil
}

// reorderFlagsFirst places flag arguments (--foo=bar, --flag) ahead of positional
// arguments so standard library flag.FlagSet parses flags regardless of position.
func reorderFlagsFirst(args []string) []string {
	var flags []string
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			// Handle space-separated value flags (--limit 3 or --tether ID)
			if !strings.Contains(a, "=") && (a == "-limit" || a == "--limit" || a == "-tether" || a == "--tether") && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		} else {
			pos = append(pos, a)
		}
	}
	return append(flags, pos...)
}

func printUsage() {
	fmt.Println(`Usage:
  cairn [trail-name]                    Launch 3-pane Bubble Tea workbench
  cairn drop [--stdin] [--tether=<id>]  Drop and cleave one or more stones
  cairn query "<text>" [--limit=8] [--json]
                                        Run deterministic Horizon retrieval
  cairn blaze [trail-name]              Compile session to .cairn/trail.md and stdout
  cairn index [--rebuild]               Synchronize SQLite FTS5 index with disk`)
}
