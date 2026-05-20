package netproxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"
)

func HTTPClient(rawProxy string, timeout time.Duration) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	proxyURL, err := Parse(rawProxy)
	if err != nil {
		return nil, err
	}
	if proxyURL != nil {
		switch strings.ToLower(proxyURL.Scheme) {
		case "http", "https":
			transport.Proxy = http.ProxyURL(proxyURL)
		case "socks5", "socks5h", "sock5":
			socksURL := *proxyURL
			socksURL.Scheme = "socks5"
			dialer, err := xproxy.FromURL(&socksURL, xproxy.Direct)
			if err != nil {
				return nil, fmt.Errorf("网络代理地址无效")
			}
			contextDialer, ok := dialer.(xproxy.ContextDialer)
			if ok {
				transport.DialContext = contextDialer.DialContext
			} else {
				transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
					return dialer.Dial(network, address)
				}
			}
		}
	}
	return &http.Client{Timeout: timeout, Transport: transport}, nil
}

func Parse(rawProxy string) (*url.URL, error) {
	rawProxy = strings.TrimSpace(rawProxy)
	if rawProxy == "" {
		return nil, nil
	}
	proxyURL, err := url.Parse(rawProxy)
	if err != nil || proxyURL.Scheme == "" || proxyURL.Host == "" {
		return nil, errors.New("网络代理地址无效")
	}
	switch strings.ToLower(proxyURL.Scheme) {
	case "http", "https", "socks5", "socks5h", "sock5":
		return proxyURL, nil
	default:
		return nil, errors.New("网络代理协议必须是 http、https、socks5、socks5h 或 sock5")
	}
}
