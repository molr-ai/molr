//go:build linux && (amd64 || arm64)

package sandbox

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"golang.org/x/net/dns/dnsmessage"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"
)

func networkTestClient(args []string) error {
	for _, v := range os.Environ() {
		if strings.Contains(strings.ToLower(strings.SplitN(v, "=", 2)[0]), "proxy") {
			return fmt.Errorf("proxy environment leaked")
		}
	}
	if len(args) < 2 {
		return fmt.Errorf("client arguments required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	switch args[0] {
	case "get", "untrusted", "denied", "budget", "revoke":
		roots := x509.NewCertPool()
		ca, e := os.ReadFile("/work/ca.pem")
		if e != nil {
			return e
		}
		roots.AppendCertsFromPEM(ca)
		if args[0] == "untrusted" {
			roots = x509.NewCertPool()
		}
		transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, DisableKeepAlives: true}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport}
		req, _ := http.NewRequestWithContext(ctx, "GET", args[1], nil)
		response, e := client.Do(req)
		if args[0] == "denied" || args[0] == "untrusted" {
			if e == nil {
				response.Body.Close()
				return fmt.Errorf("unexpected access")
			}
			fmt.Println(args[0] + "-ok")
			return nil
		}
		if e != nil {
			return e
		}
		defer response.Body.Close()
		if args[0] == "revoke" {
			r := bufio.NewReader(response.Body)
			line, e := r.ReadString('\n')
			if e != nil || line != "connected\n" {
				return fmt.Errorf("stream never connected: %v", e)
			}
			_, e = r.ReadByte()
			if e == nil {
				return fmt.Errorf("revoked stream survived")
			}
			if _, e = client.Get(args[1]); e == nil {
				return fmt.Errorf("new connection survived revocation")
			}
			fmt.Println("active-and-new-tcp-revocation-ok")
			return nil
		}
		body, e := io.ReadAll(io.LimitReader(response.Body, 2<<20))
		if args[0] == "budget" {
			if e == nil || len(body) > 32768 {
				return fmt.Errorf("budget not enforced: bytes=%d err=%v", len(body), e)
			}
			fmt.Printf("budget-ok bytes=%d\n", len(body))
			return nil
		}
		if e != nil {
			return e
		}
		if string(body) != "tls-ok" {
			return fmt.Errorf("unexpected response %q", body)
		}
		fmt.Println("https-dns-without-proxy-ok")
	case "udp", "udp-denied", "udp-revoke":
		conn, e := net.DialTimeout("udp", args[1], time.Second)
		if e != nil {
			return e
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(1200 * time.Millisecond))
		payload := []byte("molr-udp-packet")
		if _, e = conn.Write(payload); e != nil {
			return e
		}
		buf := make([]byte, 2048)
		n, e := conn.Read(buf)
		if args[0] == "udp-denied" {
			if e == nil {
				return fmt.Errorf("unauthorized UDP replied")
			}
			fmt.Println("udp-denied-ok")
			return nil
		}
		if e != nil || string(buf[:n]) != string(payload) {
			return fmt.Errorf("UDP failed: %v", e)
		}
		if args[0] == "udp-revoke" {
			time.Sleep(200 * time.Millisecond)
			conn.SetDeadline(time.Now().Add(1200 * time.Millisecond))
			conn.Write(payload)
			if _, e = conn.Read(buf); e == nil {
				return fmt.Errorf("UDP flow survived revocation")
			}
			fmt.Println("udp-revocation-ok")
			return nil
		}
		fmt.Println("udp-without-proxy-ok")
	case "half-close":
		connection, e := net.DialTimeout("tcp", args[1], time.Second)
		if e != nil {
			return e
		}
		defer connection.Close()
		connection.SetDeadline(time.Now().Add(3 * time.Second))
		if _, e = connection.Write([]byte("request")); e != nil {
			return e
		}
		if e = connection.(*net.TCPConn).CloseWrite(); e != nil {
			return e
		}
		b, e := io.ReadAll(connection)
		if e != nil || string(b) != "half-close-ok" {
			return fmt.Errorf("half-close response: %q %v", b, e)
		}
		fmt.Println("tcp-half-close-ok")
	case "dns-tcp":
		connection, e := net.DialTimeout("tcp", "198.18.0.1:53", time.Second)
		if e != nil {
			return e
		}
		defer connection.Close()
		connection.SetDeadline(time.Now().Add(3 * time.Second))
		packet := dnsQuery(args[1], dnsmessage.TypeA)
		frame := append(binary.BigEndian.AppendUint16(nil, uint16(len(packet))), packet...)
		for _, b := range frame {
			if _, e = connection.Write([]byte{b}); e != nil {
				return e
			}
		}
		var prefix [2]byte
		if _, e = io.ReadFull(connection, prefix[:]); e != nil {
			return e
		}
		response := make([]byte, binary.BigEndian.Uint16(prefix[:]))
		if _, e = io.ReadFull(connection, response); e != nil {
			return e
		}
		var answer dnsmessage.Message
		if e = answer.Unpack(response); e != nil || len(answer.Answers) != 1 {
			return fmt.Errorf("TCP DNS failed: %v", e)
		}
		fmt.Println("tcp-dns-ok")
	case "dns-denied":
		if _, e := net.DefaultResolver.LookupHost(ctx, args[1]); e == nil {
			return fmt.Errorf("unauthorized DNS query succeeded")
		}
		fmt.Println("dns-denied-ok")
	case "connections":
		var held []net.Conn
		defer func() {
			for _, c := range held {
				c.Close()
			}
		}()
		for i := 0; i < 17; i++ {
			c, e := net.DialTimeout("tcp", args[1], time.Second)
			if i < 16 && e != nil {
				return fmt.Errorf("connection %d: %w", i, e)
			}
			if e == nil {
				held = append(held, c)
			}
			if i == 16 && e == nil {
				return fmt.Errorf("connection limit exceeded")
			}
		}
		fmt.Println("connection-limit-16-ok")
	case "boundaries":
		for _, path := range []string{"/molr/control", "/molr/tun", "/dev/net/tun", "/home", "/sys/fs/cgroup"} {
			if _, e := os.Stat(path); !os.IsNotExist(e) {
				return fmt.Errorf("host/setup path visible: %s", path)
			}
		}
		status, e := os.ReadFile("/proc/self/status")
		if e != nil {
			return e
		}
		for _, name := range []string{"CapInh:", "CapPrm:", "CapEff:", "CapBnd:", "CapAmb:"} {
			if !strings.Contains(string(status), name+"\t0000000000000000") {
				return fmt.Errorf("capability survived: %s", name)
			}
		}
		if !strings.Contains(string(status), "NoNewPrivs:\t1") || !strings.Contains(string(status), "Seccomp:\t2") {
			return fmt.Errorf("security filters missing")
		}
		fmt.Println("zero-capabilities-seccomp-no-new-privileges-ok")
	default:
		return fmt.Errorf("unknown client mode")
	}
	return nil
}

func networkTestIP(t *testing.T) string {
	t.Helper()
	interfaces, e := net.Interfaces()
	if e != nil {
		t.Fatal(e)
	}
	for _, it := range interfaces {
		if it.Flags&net.FlagUp == 0 || it.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, _ := it.Addrs()
		for _, a := range addresses {
			p, e := netip.ParsePrefix(a.String())
			if e == nil && p.Addr().Is4() && p.Addr().IsPrivate() {
				return p.Addr().String()
			}
		}
	}
	t.Fatal("a host IPv4 fixture address is required")
	return ""
}
func networkTestTLS(t *testing.T, ip string, handler http.Handler) (*httptest.Server, []byte) {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Molr isolated network lab"}, DNSNames: []string{"molr.test", "molr6.test", "host.molr.internal", "host6.molr.internal"}, IPAddresses: []net.IP{net.ParseIP(ip)}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	listener, e := net.Listen("tcp", net.JoinHostPort(ip, "0"))
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener.Close()
	server.Listener = listener
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
