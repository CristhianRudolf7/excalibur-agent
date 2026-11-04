package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// A brand-new Quick Tunnel hostname can be negatively cached by the local
// resolver before Cloudflare publishes its record. Retry that specific failure
// against Cloudflare's public DNS service, retaining normal HTTP Host and TLS
// certificate verification for the original hostname.
func previewHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := previewDialer{direct: (&net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		lookup: func(ctx context.Context, host string) ([]string, error) {
			return lookupPreviewIPs(ctx, host, &http.Client{Timeout: 2 * time.Second, CheckRedirect: previewNoRedirect})
		},
	}
	transport.DialContext = dialer.dial
	return &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: previewNoRedirect}
}
func previewNoRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

type previewDialer struct {
	direct func(context.Context, string, string) (net.Conn, error)
	lookup func(context.Context, string) ([]string, error)
}

func (d previewDialer) dial(ctx context.Context, network, address string) (net.Conn, error) {
	connection, err := d.direct(ctx, network, address)
	if err == nil {
		return connection, nil
	}
	var dns *net.DNSError
	host, port, splitErr := net.SplitHostPort(address)
	if splitErr != nil || !strings.HasSuffix(host, ".trycloudflare.com") || !errors.As(err, &dns) || !dns.IsNotFound {
		return nil, err
	}
	ips, lookupErr := d.lookup(ctx, host)
	if lookupErr != nil {
		return nil, err
	}
	for _, ip := range ips {
		if net.ParseIP(ip) == nil {
			continue
		}
		connection, dialErr := d.direct(ctx, network, net.JoinHostPort(ip, port))
		if dialErr == nil {
			return connection, nil
		}
	}
	return nil, err
}

func lookupPreviewIPs(ctx context.Context, host string, client *http.Client) ([]string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://cloudflare-dns.com/dns-query?name="+url.QueryEscape(host)+"&type=A", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/dns-json")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("public DNS lookup failed")
	}
	var result struct {
		Status int `json:"Status"`
		Answer []struct {
			Type int    `json:"type"`
			Data string `json:"data"`
		} `json:"Answer"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result); err != nil {
		return nil, err
	}
	ips := []string{}
	if result.Status == 0 {
		for _, answer := range result.Answer {
			if answer.Type == 1 && net.ParseIP(answer.Data) != nil {
				ips = append(ips, answer.Data)
			}
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("public preview hostname is not yet resolvable")
	}
	return ips, nil
}
