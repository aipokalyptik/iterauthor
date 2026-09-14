package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/aipokalyptik/iterauthor/internal/project"
	"github.com/aipokalyptik/iterauthor/internal/tui"
)

var version = "0.1.0-test"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "iterauthor:", err)
		os.Exit(1)
	}
}
func run() error {
	newProject := flag.Bool("new", false, "create a project in an empty directory")
	sample := flag.Bool("sample", false, "create with the sample story (requires --new)")
	demo := flag.Bool("demo", false, "use explicit synthetic model replies; no network calls")
	title := flag.String("title", "My story", "title for a new project")
	check := flag.Bool("check", false, "validate the project and print its summary without a TUI")
	ver := flag.Bool("version", false, "print version")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: iterauthor [--new] [--sample] [--demo] [--check] PROJECT_DIRECTORY")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *ver {
		fmt.Println("iterauthor", version)
		return nil
	}
	if flag.NArg() != 1 {
		flag.Usage()
		return fmt.Errorf("provide one project directory")
	}
	if *sample && !*newProject {
		return fmt.Errorf("--sample requires --new")
	}
	var s *project.Store
	var err error
	if *newProject {
		s, err = project.Create(flag.Arg(0), *title, *sample)
	} else {
		s, err = project.Open(flag.Arg(0))
	}
	if err != nil {
		return err
	}
	if *check {
		defer s.Close()
		fmt.Printf("%s\nProject: %s\nOutline items: %d\nKnowledge entries: %d\nPassages: %d\nUnresolved changes: %d\n", s.Config.Title, s.Dir, len(s.Config.Nodes), len(s.Config.Knowledge), len(s.Config.Leaves(s.Config.Root)), len(s.State.Changes))
		return nil
	}
	u := tui.New(s, *demo)
	defer u.Close()
	return u.Run()
}
