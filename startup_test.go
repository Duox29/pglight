package main

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func TestParseStartupOptionsDefault(t *testing.T) {
	options, err := parseStartupOptions(nil, func(string) string { return "" })
	if err != nil {
		t.Fatalf("parse default options: %v", err)
	}
	if options.port != defaultHTTPPort || options.portSet || options.noBrowser {
		t.Fatalf("unexpected default options: %+v", options)
	}
}

func TestParseStartupOptionsPrecedenceAndAliases(t *testing.T) {
	getenv := func(string) string { return "8123" }
	for _, args := range [][]string{{"--p", "8124"}, {"--port=8124"}, {"-p", "8124"}} {
		options, err := parseStartupOptions(args, getenv)
		if err != nil {
			t.Fatalf("parse %v: %v", args, err)
		}
		if options.port != 8124 || !options.portSet {
			t.Fatalf("CLI port did not override PORT for %v: %+v", args, options)
		}
	}

	options, err := parseStartupOptions([]string{"--no-browser"}, getenv)
	if err != nil {
		t.Fatalf("parse PORT: %v", err)
	}
	if options.port != 8123 || !options.portSet || !options.noBrowser {
		t.Fatalf("unexpected PORT options: %+v", options)
	}
}

func TestParseStartupOptionsRejectsInvalidOrDuplicatePorts(t *testing.T) {
	for _, args := range [][]string{
		{"--p", "0"},
		{"--port", "65536"},
		{"--p", "8081", "--port", "8082"},
	} {
		if _, err := parseStartupOptions(args, func(string) string { return "" }); err == nil {
			t.Fatalf("expected error for %v", args)
		}
	}

	if _, err := parseStartupOptions(nil, func(string) string { return "not-a-port" }); err == nil {
		t.Fatal("expected invalid PORT error")
	}
}

func TestListenHTTPFallsBackWhenDefaultPortIsInUse(t *testing.T) {
	blocked, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer blocked.Close()
	port := blocked.Addr().(*net.TCPAddr).Port
	if port >= 65535 {
		t.Skip("reserved port leaves no candidate for fallback")
	}

	listener, selected, err := listenHTTP(startupOptions{port: port})
	if err != nil {
		t.Fatalf("fallback listen: %v", err)
	}
	defer listener.Close()
	if selected != port+1 {
		t.Fatalf("selected port %d, want %d", selected, port+1)
	}
}

func TestListenHTTPExplicitPortDoesNotFallback(t *testing.T) {
	blocked, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer blocked.Close()
	port := blocked.Addr().(*net.TCPAddr).Port

	_, _, err = listenHTTP(startupOptions{port: port, portSet: true})
	if err == nil {
		t.Fatal("expected explicit port failure")
	}
	if !strings.Contains(err.Error(), "listen on port "+strconv.Itoa(port)) {
		t.Fatalf("unexpected explicit port error: %v", err)
	}
}

func TestListenHTTPStopsAfterMaxPortAttempts(t *testing.T) {
	called := 0
	listen := func(string, string) (net.Listener, error) {
		called++
		return nil, syscall.EADDRINUSE
	}

	_, _, err := listenHTTPWith(startupOptions{port: 9000}, listen)
	if err == nil {
		t.Fatal("expected exhausted port error")
	}
	if called != maxPortAttempts {
		t.Fatalf("tried %d ports, want %d", called, maxPortAttempts)
	}
	if !errors.Is(err, syscall.EADDRINUSE) {
		t.Fatalf("expected address-in-use cause, got %v", err)
	}
}

func TestListenHTTPUsesAllInterfacesForIPFiltering(t *testing.T) {
	var got string
	listener, _, err := listenHTTPWith(startupOptions{port: 9000, portSet: true}, func(network, address string) (net.Listener, error) {
		got = address
		return net.Listen(network, "127.0.0.1:0")
	})
	if err != nil {
		t.Fatalf("listen loopback: %v", err)
	}
	defer listener.Close()
	if got != ":9000" {
		t.Fatalf("listen address = %q, want :9000", got)
	}
}

func TestBrowserCommand(t *testing.T) {
	tests := []struct {
		goos string
		name string
		args []string
	}{
		{goos: "windows", name: "rundll32", args: []string{"url.dll,FileProtocolHandler", "http://127.0.0.1:8080"}},
		{goos: "darwin", name: "open", args: []string{"http://127.0.0.1:8080"}},
		{goos: "linux", name: "xdg-open", args: []string{"http://127.0.0.1:8080"}},
	}
	for _, test := range tests {
		name, args, err := browserCommand(test.goos, test.args[len(test.args)-1])
		if err != nil {
			t.Fatalf("browser command for %s: %v", test.goos, err)
		}
		if name != test.name || strings.Join(args, "|") != strings.Join(test.args, "|") {
			t.Errorf("browser command for %s = %s %v, want %s %v", test.goos, name, args, test.name, test.args)
		}
	}

	if _, _, err := browserCommand("freebsd", "http://127.0.0.1:8080"); err == nil {
		t.Fatal("expected unsupported OS error")
	}
}
