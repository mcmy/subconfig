package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"subconfig/internal/app"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	forceServe := false
	if len(args) > 0 {
		switch args[0] {
		case "serve":
			forceServe = true
			args = args[1:]
		case "convert":
			args = args[1:]
		case "version":
			fmt.Fprintln(stdout, "subconfig", version)
			return nil
		case "help":
			args = []string{"--help"}
		}
	}
	o, err := app.ParseOptions(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if o.Version.Or(false) {
		fmt.Fprintln(stdout, "subconfig", version)
		return nil
	}
	if forceServe && o.Port == "" && o.Listen == "" {
		o.Listen = ":25500"
	}
	if o.SecretFile != "" {
		if o.Secret != "" {
			return fmt.Errorf("choose --secret or --secret-file")
		}
		b, err := os.ReadFile(o.SecretFile)
		if err != nil {
			return err
		}
		o.Secret = strings.TrimSpace(string(b))
		if o.Secret == "" {
			return fmt.Errorf("secret file is empty")
		}
	}
	if o.Secret == "" {
		o.Secret = os.Getenv("SUBCONFIG_SECRET")
	}
	if (o.Listen != "" || o.Port != "") && o.Secret == "" && !o.Public.Or(false) {
		return fmt.Errorf("HTTP service requires --secret/-s, --secret-file or SUBCONFIG_SECRET; --public explicitly disables authentication")
	}
	e, err := app.NewEngine(o, stdin)
	if err != nil {
		return err
	}
	if o.Listen != "" || o.Port != "" {
		if o.Secret == "" && !o.Public.Or(false) {
			return fmt.Errorf("HTTP service requires --secret/-s, --secret-file or SUBCONFIG_SECRET; --public explicitly disables authentication")
		}
		if o.Output != "" {
			return fmt.Errorf("HTTP service cannot use --output")
		}
		if len(o.Files)+len(o.URLs) > 0 {
			if _, err = e.Convert(context.Background(), o); err != nil {
				return fmt.Errorf("startup validation: %w", err)
			}
		}
		addr := o.Listen
		if addr == "" {
			addr = net.JoinHostPort(o.Host, o.Port)
		}
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		srv := &http.Server{Handler: app.Handler(e, o, version, stderr), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: o.RequestTimeout + 15*time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		done := make(chan struct{})
		defer close(done)
		go func() {
			select {
			case <-ctx.Done():
				shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_ = srv.Shutdown(shutdown)
			case <-done:
			}
		}()
		fmt.Fprintf(stderr, "subconfig %s listening on %s\n", version, listener.Addr())
		err = srv.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
	r, err := e.Convert(context.Background(), o)
	if err != nil {
		return err
	}
	if o.UserinfoOutput != "" {
		metadataPath, _ := filepath.Abs(o.UserinfoOutput)
		outputPath, _ := filepath.Abs(o.Output)
		if o.Output != "" && o.Output != "-" && metadataPath == outputPath {
			return fmt.Errorf("metadata and config output files must differ")
		}
		var b strings.Builder
		if err = app.WriteMetadata(&b, r); err != nil {
			return err
		}
		if err = writeAtomic(o.UserinfoOutput, []byte(b.String())); err != nil {
			return err
		}
	}
	if o.Output == "" || o.Output == "-" {
		_, err = stdout.Write(r.Data)
	} else {
		err = writeAtomic(o.Output, r.Data)
	}
	if err != nil {
		return err
	}
	app.Report(stderr, o, r)
	if r.Userinfo != "" && !o.Quiet.Or(false) {
		fmt.Fprintln(stderr, "Subscription-Userinfo:", r.Userinfo)
	}
	return nil
}

// Atomic replacement prevents readers from observing partial configs; profiles
// contain credentials, so files are private by default.
func writeAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".subconfig-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
