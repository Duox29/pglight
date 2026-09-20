package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

const (
	defaultHTTPPort = 8080
	maxPortAttempts = 100
)

type startupOptions struct {
	port      int
	portSet   bool
	noBrowser bool
}

func parseStartupOptions(args []string, getenv func(string) string) (startupOptions, error) {
	fs := flag.NewFlagSet("pglight", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	p := fs.Int("p", 0, "HTTP port")
	port := fs.Int("port", 0, "HTTP port")
	noBrowser := fs.Bool("no-browser", false, "do not open the application in a browser")
	if err := fs.Parse(args); err != nil {
		return startupOptions{}, err
	}
	if fs.NArg() > 0 {
		return startupOptions{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}

	pSet, portSet := false, false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "p":
			pSet = true
		case "port":
			portSet = true
		}
	})
	if pSet && portSet {
		return startupOptions{}, errors.New("use only one of --p, --port, or -p")
	}
	if pSet || portSet {
		value := *p
		if portSet {
			value = *port
		}
		if err := validatePort(value); err != nil {
			return startupOptions{}, err
		}
		return startupOptions{port: value, portSet: true, noBrowser: *noBrowser}, nil
	}

	if raw := strings.TrimSpace(getenv("PORT")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return startupOptions{}, fmt.Errorf("invalid PORT %q: %w", raw, err)
		}
		if err := validatePort(value); err != nil {
			return startupOptions{}, fmt.Errorf("invalid PORT: %w", err)
		}
		return startupOptions{port: value, portSet: true, noBrowser: *noBrowser}, nil
	}

	return startupOptions{port: defaultHTTPPort, noBrowser: *noBrowser}, nil
}

func validatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", port)
	}
	return nil
}

func listenHTTP(options startupOptions) (net.Listener, int, error) {
	return listenHTTPWith(options, net.Listen)
}

type listenFunc func(network, address string) (net.Listener, error)

func listenHTTPWith(options startupOptions, listen listenFunc) (net.Listener, int, error) {
	if options.portSet {
		listener, err := listen("tcp", ":"+strconv.Itoa(options.port))
		if err != nil {
			return nil, 0, fmt.Errorf("listen on port %d: %w", options.port, err)
		}
		return listener, options.port, nil
	}

	var lastErr error
	for offset := 0; offset < maxPortAttempts; offset++ {
		candidate := options.port + offset
		if candidate > 65535 {
			break
		}
		listener, err := listen("tcp", ":"+strconv.Itoa(candidate))
		if err == nil {
			return listener, candidate, nil
		}
		if !isAddressInUse(err) {
			return nil, 0, fmt.Errorf("listen on port %d: %w", candidate, err)
		}
		lastErr = err
	}

	if lastErr != nil {
		return nil, 0, fmt.Errorf("could not find an available port from %d after %d attempts: %w", options.port, maxPortAttempts, lastErr)
	}
	return nil, 0, fmt.Errorf("could not find an available port from %d after %d attempts", options.port, maxPortAttempts)
}

func isAddressInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	if runtime.GOOS != "windows" {
		return false
	}

	var errno syscall.Errno
	return errors.As(err, &errno) && errno == syscall.Errno(10048) // WSAEADDRINUSE
}

func openBrowser(url string) error {
	name, args, err := browserCommand(runtime.GOOS, url)
	if err != nil {
		return err
	}
	return exec.Command(name, args...).Start()
}

func browserCommand(goos, url string) (string, []string, error) {
	switch goos {
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}, nil
	case "darwin":
		return "open", []string{url}, nil
	case "linux":
		return "xdg-open", []string{url}, nil
	default:
		return "", nil, fmt.Errorf("automatic browser opening is not supported on %s", goos)
	}
}
