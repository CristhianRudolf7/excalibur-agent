package daemon

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestPublicPreviewDNSFallback(t *testing.T) {
	host := "fresh-preview.trycloudflare.com"
	missing := &net.DNSError{Name: host, Err: "no such host", IsNotFound: true}
	connection, peer := net.Pipe()
	defer connection.Close()
	defer peer.Close()
	addresses := []string{}
	dialer := previewDialer{
		direct: func(_ context.Context, network, address string) (net.Conn, error) {
			addresses = append(addresses, address)
			if address == "198.51.100.8:443" {
				return connection, nil
			}
			return nil, missing
		},
		lookup: func(_ context.Context, name string) ([]string, error) {
			if name != host {
				t.Fatal(name)
			}
			return []string{"invalid", "198.51.100.7", "198.51.100.8"}, nil
		},
	}
	got, err := dialer.dial(context.Background(), "tcp", host+":443")
	if err != nil || got != connection {
		t.Fatal(got, err)
	}
	if !reflect.DeepEqual(addresses, []string{host + ":443", "198.51.100.7:443", "198.51.100.8:443"}) {
		t.Fatal(addresses)
	}
	for _, tc := range []struct {
		address string
		err     error
	}{
		{"example.com:443", missing}, {"fresh.trycloudflare.com.evil.test:443", missing},
		{host + ":443", context.Canceled}, {host + ":443", errors.New("connection refused")},
	} {
		dialer.direct = func(context.Context, string, string) (net.Conn, error) { return nil, tc.err }
		dialer.lookup = func(context.Context, string) ([]string, error) { t.Fatal("unexpected DNS fallback"); return nil, nil }
		if _, err = dialer.dial(context.Background(), "tcp", tc.address); err != tc.err {
			t.Fatal(err)
		}
	}
	dialer.direct = func(context.Context, string, string) (net.Conn, error) { return nil, missing }
	dialer.lookup = func(context.Context, string) ([]string, error) { return nil, errors.New("not published") }
	if _, err = dialer.dial(context.Background(), "tcp", host+":443"); err != missing {
		t.Fatal("original DNS error lost", err)
	}
}

func TestPublicPreviewPublicDNSAnswers(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		want       []string
	}{
		{"published", `{"Status":0,"Answer":[{"type":5,"data":"alias.example"},{"type":1,"data":"198.51.100.8"},{"type":1,"data":"not-an-ip"}]}`, 200, []string{"198.51.100.8"}},
		{"not yet published", `{"Status":3}`, 200, nil},
		{"empty", `{"Status":0,"Answer":[]}`, 200, nil},
		{"service failure", `{}`, 503, nil},
		{"malformed", `not-json`, 200, nil},
		{"too large", `{"Status":0,"Answer":[{"type":1,"data":"` + strings.Repeat("x", 64<<10) + `"}]}`, 200, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: previewTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "cloudflare-dns.com" || r.URL.Query().Get("name") != "fresh.trycloudflare.com" || r.URL.Query().Get("type") != "A" || r.Header.Get("Accept") != "application/dns-json" {
					t.Fatal("invalid DNS request", r.URL)
				}
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			ips, err := lookupPreviewIPs(context.Background(), "fresh.trycloudflare.com", client)
			if tc.want == nil {
				if err == nil {
					t.Fatal("invalid response accepted", ips)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(ips, tc.want) {
				t.Fatal(ips, err)
			}
		})
	}
}
