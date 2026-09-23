package db

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestSSHTunnelRequiresPinnedHostAndForwards(t *testing.T) {
	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	go func() {
		for {
			c, acceptErr := backend.Accept()
			if acceptErr != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	serverConfig := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if c.User() != "tester" || string(password) != "secret" {
				return nil, fmt.Errorf("invalid credentials")
			}
			return nil, nil
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if c.User() != "tester" || !bytes.Equal(key.Marshal(), signer.PublicKey().Marshal()) {
				return nil, fmt.Errorf("invalid public key")
			}
			return nil, nil
		},
	}
	serverConfig.AddHostKey(signer)
	sshListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer sshListener.Close()
	go func() {
		for {
			raw, acceptErr := sshListener.Accept()
			if acceptErr != nil {
				return
			}
			go serveTestSSH(raw, serverConfig)
		}
	}()

	sshAddr := sshListener.Addr().(*net.TCPAddr)
	backendAddr := backend.Addr().(*net.TCPAddr)
	cfg := SSHTunnelOptions{Enabled: true, Host: "127.0.0.1", Port: sshAddr.Port, User: "tester", AuthMethod: "password", Password: "secret", DestinationHost: "127.0.0.1", DestinationPort: backendAddr.Port}
	_, _, _, err = StartSSHTunnel(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "SSH host key is not trusted; fingerprint: SHA256:") {
		t.Fatalf("untrusted host error = %v", err)
	}
	cfg.HostKeySHA256 = "SHA256:changed-key-pin"
	_, _, _, err = StartSSHTunnel(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "SSH host key changed") {
		t.Fatalf("changed host key error = %v", err)
	}
	cfg.HostKeySHA256 = ssh.FingerprintSHA256(signer.PublicKey())
	tunnel, localHost, localPort, err := StartSSHTunnel(context.Background(), cfg)
	if err != nil {
		t.Fatalf("start pinned tunnel: %v", err)
	}
	defer tunnel.Close()
	client, err := net.Dial("tcp", net.JoinHostPort(localHost, fmt.Sprint(localPort)))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.Write([]byte("forwarded")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len("forwarded"))
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "forwarded" {
		t.Fatalf("echo = %q", buf)
	}
	_ = client.Close()
	tunnel.Close()
	if probe, probeErr := net.Dial("tcp", net.JoinHostPort(localHost, fmt.Sprint(localPort))); probeErr == nil {
		probe.Close()
		t.Fatal("local listener remained open after tunnel close")
	}
	keyBlock, err := ssh.MarshalPrivateKeyWithPassphrase(privateKey, "test", []byte("key passphrase"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.AuthMethod, cfg.Password, cfg.PrivateKey, cfg.Passphrase = "private_key", "", string(pem.EncodeToMemory(keyBlock)), "key passphrase"
	keyTunnel, _, _, err := StartSSHTunnel(context.Background(), cfg)
	if err != nil {
		t.Fatalf("start encrypted private-key tunnel: %v", err)
	}
	keyTunnel.Close()
}

func serveTestSSH(raw net.Conn, config *ssh.ServerConfig) {
	conn, chans, requests, err := ssh.NewServerConn(raw, config)
	if err != nil {
		_ = raw.Close()
		return
	}
	go ssh.DiscardRequests(requests)
	defer conn.Close()
	for ch := range chans {
		if ch.ChannelType() != "direct-tcpip" {
			_ = ch.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}
		var target struct {
			Host       string
			Port       uint32
			OriginHost string
			OriginPort uint32
		}
		if err := ssh.Unmarshal(ch.ExtraData(), &target); err != nil {
			_ = ch.Reject(ssh.ConnectionFailed, "invalid target")
			continue
		}
		remote, err := net.Dial("tcp", net.JoinHostPort(target.Host, fmt.Sprint(target.Port)))
		if err != nil {
			_ = ch.Reject(ssh.ConnectionFailed, "target unavailable")
			continue
		}
		channel, requests, err := ch.Accept()
		if err != nil {
			_ = remote.Close()
			continue
		}
		go ssh.DiscardRequests(requests)
		go func() {
			defer channel.Close()
			defer remote.Close()
			done := make(chan struct{}, 1)
			go func() { _, _ = io.Copy(channel, remote); done <- struct{}{} }()
			_, _ = io.Copy(remote, channel)
			<-done
		}()
	}
}
