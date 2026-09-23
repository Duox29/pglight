package db

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHTunnelOptions contains SSH transport configuration. Secret fields are
// runtime-only and must never be copied into connection metadata or logs.
type SSHTunnelOptions struct {
	Enabled         bool
	Host            string
	Port            int
	User            string
	AuthMethod      string
	Password        string
	PrivateKey      string
	Passphrase      string
	HostKeySHA256   string
	DestinationHost string
	DestinationPort int
}

type SSHTunnel struct {
	listener net.Listener
	client   *ssh.Client
	done     chan struct{}
	wg       sync.WaitGroup
	once     sync.Once
}

func StartSSHTunnel(ctx context.Context, cfg SSHTunnelOptions) (*SSHTunnel, string, int, error) {
	if !cfg.Enabled {
		return nil, "", 0, nil
	}
	cfg.Host = strings.TrimSpace(cfg.Host)
	cfg.User = strings.TrimSpace(cfg.User)
	if cfg.Host == "" || cfg.User == "" {
		return nil, "", 0, fmt.Errorf("SSH host and username are required")
	}
	if cfg.Port <= 0 {
		cfg.Port = 22
	}
	if cfg.Port > 65535 {
		return nil, "", 0, fmt.Errorf("invalid SSH port")
	}
	if cfg.DestinationHost == "" {
		cfg.DestinationHost = "localhost"
	}
	if cfg.DestinationPort <= 0 {
		cfg.DestinationPort = 5432
	}
	if cfg.DestinationPort > 65535 {
		return nil, "", 0, fmt.Errorf("invalid PostgreSQL port")
	}
	var auth ssh.AuthMethod
	switch strings.ToLower(strings.TrimSpace(cfg.AuthMethod)) {
	case "password":
		if cfg.Password == "" {
			return nil, "", 0, fmt.Errorf("SSH password is required")
		}
		auth = ssh.Password(cfg.Password)
	case "private_key":
		var signer ssh.Signer
		var err error
		if cfg.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(cfg.PrivateKey), []byte(cfg.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(cfg.PrivateKey))
		}
		if err != nil {
			return nil, "", 0, fmt.Errorf("invalid SSH private key or passphrase")
		}
		auth = ssh.PublicKeys(signer)
	default:
		return nil, "", 0, fmt.Errorf("SSH authentication must be password or private_key")
	}
	var fingerprint string
	clientConfig := &ssh.ClientConfig{
		User: cfg.User, Auth: []ssh.AuthMethod{auth}, Timeout: 8 * time.Second,
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			fingerprint = ssh.FingerprintSHA256(key)
			if cfg.HostKeySHA256 == "" {
				return fmt.Errorf("SSH host key is not trusted; fingerprint: %s", fingerprint)
			}
			if cfg.HostKeySHA256 != fingerprint {
				return fmt.Errorf("SSH host key changed; expected %s, received %s", cfg.HostKeySHA256, fingerprint)
			}
			return nil
		},
	}
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(cfg.Host, fmt.Sprint(cfg.Port)))
	if err != nil {
		return nil, "", 0, fmt.Errorf("SSH network: %w", err)
	}
	conn.SetDeadline(time.Now().Add(8 * time.Second))
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, net.JoinHostPort(cfg.Host, fmt.Sprint(cfg.Port)), clientConfig)
	if err != nil {
		conn.Close()
		return nil, fingerprint, 0, fmt.Errorf("SSH handshake: %w", err)
	}
	_ = conn.SetDeadline(time.Time{})
	client := ssh.NewClient(sshConn, chans, reqs)
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", "0"))
	if err != nil {
		client.Close()
		return nil, fingerprint, 0, fmt.Errorf("SSH local forward: %w", err)
	}
	t := &SSHTunnel{listener: listener, client: client, done: make(chan struct{})}
	t.wg.Add(1)
	go t.forward(ctx, net.JoinHostPort(cfg.DestinationHost, fmt.Sprint(cfg.DestinationPort)))
	return t, listener.Addr().(*net.TCPAddr).IP.String(), listener.Addr().(*net.TCPAddr).Port, nil
}

func (t *SSHTunnel) forward(ctx context.Context, destination string) {
	defer t.wg.Done()
	go func() {
		select {
		case <-ctx.Done():
			t.Close()
		case <-t.done:
		}
	}()
	for {
		local, err := t.listener.Accept()
		if err != nil {
			return
		}
		go func() {
			remote, err := t.client.Dial("tcp", destination)
			if err != nil {
				local.Close()
				return
			}
			var pipes sync.WaitGroup
			pipes.Add(2)
			go func() { defer pipes.Done(); _, _ = io.Copy(remote, local); _ = remote.Close() }()
			go func() { defer pipes.Done(); _, _ = io.Copy(local, remote); _ = local.Close() }()
			pipes.Wait()
		}()
	}
}

func (t *SSHTunnel) Close() {
	if t == nil {
		return
	}
	t.once.Do(func() {
		close(t.done)
		_ = t.listener.Close()
		_ = t.client.Close()
	})
	t.wg.Wait()
}
