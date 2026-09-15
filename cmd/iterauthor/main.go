package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aipokalyptik/iterauthor/internal/application"
	"github.com/aipokalyptik/iterauthor/internal/model"
	"github.com/aipokalyptik/iterauthor/internal/project"
	"github.com/aipokalyptik/iterauthor/internal/tui"
	"github.com/aipokalyptik/iterauthor/internal/web"
)

var version = "0.2.0-test"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "iterauthor:", err)
		os.Exit(1)
	}
}
func run() (err error) {
	newProject := flag.Bool("new", false, "create a project in an empty directory")
	sample := flag.Bool("sample", false, "create with the sample story (requires --new)")
	demo := flag.Bool("demo", false, "use explicit synthetic model replies; no network calls")
	title := flag.String("title", "My story", "title for a new project")
	check := flag.Bool("check", false, "validate the project and print its summary without starting an interface")
	terminal := flag.Bool("tui", false, "use the optional terminal interface")
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP bind address; use 0.0.0.0:8080 or :8080 for network access")
	ver := flag.Bool("version", false, "print version")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: iterauthor [--new] [--sample] [--demo] [--check] [PROJECT_DIRECTORY]")
		fmt.Fprintln(os.Stderr, "PROJECT_DIRECTORY defaults to the current working directory.")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *ver {
		fmt.Println("iterauthor", version)
		return nil
	}
	if flag.NArg() > 1 {
		flag.Usage()
		return fmt.Errorf("provide at most one project directory")
	}
	if *sample && !*newProject {
		return fmt.Errorf("--sample requires --new")
	}
	dir := "."
	if flag.NArg() == 1 {
		dir = flag.Arg(0)
	}
	var s *project.Store
	if *newProject {
		s, err = project.Create(dir, *title, *sample)
	} else {
		s, err = project.Open(dir)
	}
	if err != nil {
		return err
	}
	if *check {
		defer s.Close()
		fmt.Printf("%s\nProject: %s\nOutline items: %d\nKnowledge entries: %d\nPassages: %d\nUnresolved changes: %d\n", s.Config.Title, s.Dir, len(s.Config.Nodes), len(s.Config.Knowledge), len(s.Config.Leaves(s.Config.Root)), len(s.State.Changes))
		return nil
	}
	var client model.Client = model.NewHTTP()
	if *demo {
		client = model.Demo{}
	}
	core := application.New(s, client, *demo)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err = errors.Join(err, core.Shutdown(ctx))
	}()
	if !*terminal {
		core.SetLogger(slog.Default())
	}
	refreshCtx, stopRefresh := context.WithCancel(context.Background())
	defer stopRefresh()
	go core.RunModelRefresh(refreshCtx)
	if *terminal {
		return tui.New(core).Run()
	}
	listener, err := web.Listen(*listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	fmt.Printf("Iterauthor — %s\nListening on %s\n", core.View().Config.Title, listener.Addr())
	if addr, ok := listener.Addr().(*net.TCPAddr); ok && addr.IP.IsUnspecified() {
		fmt.Printf("Open http://localhost:%d on this machine, or use this server's hostname or IP and port %d from another machine.\n", addr.Port, addr.Port)
	} else {
		fmt.Printf("Open http://%s in your browser.\n", listener.Addr())
	}
	fmt.Println("Press Ctrl+C to stop.")
	return web.Serve(ctx, listener, web.New(core))
}
