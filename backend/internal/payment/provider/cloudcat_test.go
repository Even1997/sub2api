package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/imroc/req/v3"
)

func TestCloudcatCreatePaymentUsesMappedProductAndEmail(t *testing.T) {
	provider, requests, closeServer := newCloudcatTestProvider(t, map[string]string{
		"/shopApi/Shop/goodsInfo":      `{"code":1,"data":{"goods_type":"card","price":5,"status":1,"stock":8,"user":{"token":"merchant-1"}}}`,
		"/shopApi/Shop/getUserChannel": `{"code":1,"data":[{"id":1,"code":"AlipayPc","status":1}]}`,
		"/shopApi/Shop/getGoodsPrice":  `{"code":1,"data":{"total_amount":5}}`,
		"/shopApi/Pay/order":           `{"code":1,"data":{"trade_no":"CAT-1","payurl":"https://catfk.com/pay/CAT-1","total_amount":5}}`,
		"/pay/CAT-1":                   `<form action="https://openapi.alipay.com/gateway.do?charset=UTF-8" method="POST"><input name="method" value="alipay.trade.page.pay"><input name="sign" value="signed"></form>`,
		"/gateway.do":                  `<html><input value="https://qr.alipay.com/cloudcat-test-1" id="J_qrCode"></html>`,
	})
	defer closeServer()
	transport := provider.checkoutClient.Transport
	provider.checkoutClient.Transport = cloudcatTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		// A healthy relayed checkout can exceed the old three-second QR budget.
		timer := time.NewTimer(3100 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-r.Context().Done():
			return nil, r.Context().Err()
		case <-timer.C:
			return transport.RoundTrip(r)
		}
	})

	result, err := provider.CreatePayment(context.Background(), payment.CreatePaymentRequest{
		Amount: "5.00", PaymentType: payment.TypeAlipay, PayerEmail: "buyer@example.com",
	})
	if err != nil {
		t.Fatalf("CreatePayment returned error: %v", err)
	}
	if result.TradeNo != "CAT-1" || result.PayURL != "https://catfk.com/pay/CAT-1" || result.QRCode != "https://qr.alipay.com/cloudcat-test-1" {
		t.Fatalf("unexpected result: %+v", result)
	}
	orderRequest := requests["/shopApi/Pay/order"]
	if orderRequest["goods_key"] != "ee9c69" || orderRequest["contact"] != "buyer@example.com" {
		t.Fatalf("unexpected order payload: %#v", orderRequest)
	}
	if orderRequest["quantity"] != json.Number("1") || orderRequest["channel_id"] != json.Number("1") {
		t.Fatalf("unexpected quantity/channel payload: %#v", orderRequest)
	}
}

func TestCloudcatCreatePaymentUsesWechatChannelWithoutResolvingAlipayQRCode(t *testing.T) {
	provider, requests, closeServer := newCloudcatTestProvider(t, map[string]string{
		"/shopApi/Shop/goodsInfo":      `{"code":1,"data":{"goods_type":"card","price":5,"status":1,"stock":8,"user":{"token":"merchant-1"}}}`,
		"/shopApi/Shop/getUserChannel": `{"code":1,"data":[{"id":1,"code":"WechatPay","status":1}]}`,
		"/shopApi/Shop/getGoodsPrice":  `{"code":1,"data":{"total_amount":5}}`,
		"/shopApi/Pay/order":           `{"code":1,"data":{"trade_no":"CAT-WX-1","payurl":"https://catfk.com/pay/CAT-WX-1","total_amount":5}}`,
	})
	defer closeServer()
	transport := provider.httpClient.Transport
	var checkoutRequests atomic.Int32
	provider.httpClient.Transport = cloudcatTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			checkoutRequests.Add(1)
		}
		return transport.RoundTrip(r)
	})

	result, err := provider.CreatePayment(context.Background(), payment.CreatePaymentRequest{
		Amount: "5.00", PaymentType: payment.TypeWxpay, PayerEmail: "buyer@example.com",
	})
	if err != nil {
		t.Fatalf("CreatePayment returned error: %v", err)
	}
	if result.TradeNo != "CAT-WX-1" || result.PayURL != "https://catfk.com/pay/CAT-WX-1" || result.QRCode != "" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if checkoutRequests.Load() != 0 {
		t.Fatalf("WeChat payment made %d checkout requests, want 0", checkoutRequests.Load())
	}
	if requests["/shopApi/Pay/order"]["channel_id"] != json.Number("1") {
		t.Fatalf("unexpected channel payload: %#v", requests["/shopApi/Pay/order"])
	}
	if got := provider.SupportedTypes(); len(got) != 2 || got[0] != payment.TypeAlipay || got[1] != payment.TypeWxpay {
		t.Fatalf("SupportedTypes() = %v", got)
	}
}

func TestCloudcatCreatePaymentRejectsMismatchedChannelType(t *testing.T) {
	provider, requests, closeServer := newCloudcatTestProvider(t, map[string]string{
		"/shopApi/Shop/goodsInfo":      `{"code":1,"data":{"goods_type":"card","price":5,"status":1,"stock":8,"user":{"token":"merchant-1"}}}`,
		"/shopApi/Shop/getUserChannel": `{"code":1,"data":[{"id":1,"code":"AlipayPc","status":1}]}`,
		"/shopApi/Shop/getGoodsPrice":  `{"code":1,"data":{"total_amount":5}}`,
	})
	defer closeServer()

	_, err := provider.CreatePayment(context.Background(), payment.CreatePaymentRequest{
		Amount: "5.00", PaymentType: payment.TypeWxpay, PayerEmail: "buyer@example.com",
	})
	if err == nil || !strings.Contains(err.Error(), "does not support wxpay") {
		t.Fatalf("error = %v", err)
	}
	if _, created := requests["/shopApi/Pay/order"]; created {
		t.Fatal("order was created with a mismatched channel")
	}
}

func TestCloudcatCreatePaymentParallelPreflight(t *testing.T) {
	for _, tt := range []struct {
		name        string
		failurePath string
		failureBody string
		wantError   string
	}{
		{name: "success"},
		{"inactive channel", "/shopApi/Shop/getUserChannel", `{"code":1,"data":[{"id":1,"code":"AlipayPc","status":0}]}`, "not active"},
		{"price mismatch", "/shopApi/Shop/getGoodsPrice", `{"code":1,"data":{"total_amount":6}}`, "amount does not match"},
		{"quote rejected", "/shopApi/Shop/getGoodsPrice", `{"code":0,"msg":"quote rejected"}`, "quote rejected"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			responses := map[string]string{
				"/shopApi/Shop/goodsInfo":      `{"code":1,"data":{"goods_type":"card","price":5,"status":1,"stock":8,"user":{"token":"merchant-1"}}}`,
				"/shopApi/Shop/getUserChannel": `{"code":1,"data":[{"id":1,"code":"AlipayPc","status":1}]}`,
				"/shopApi/Shop/getGoodsPrice":  `{"code":1,"data":{"total_amount":5}}`,
				"/shopApi/Pay/order":           `{"code":1,"data":{"trade_no":"CAT-1","payurl":"https://catfk.com/pay/CAT-1","total_amount":5}}`,
				"/pay/CAT-1":                   `<input id="J_qrCode" value="https://qr.alipay.com/cloudcat-test-1">`,
			}
			if tt.failurePath != "" {
				responses[tt.failurePath] = tt.failureBody
			}
			p, _, closeServer := newCloudcatTestProvider(t, responses)
			defer closeServer()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			// Release a serial implementation instead of hanging the regression test.
			timer := time.AfterFunc(5*time.Second, cancel)
			defer timer.Stop()
			bothStarted := make(chan struct{})
			var started, finished, orders atomic.Int32
			var siblingCancelled atomic.Bool
			transport := p.httpClient.Transport
			p.httpClient.Transport = cloudcatTestRoundTripper(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case "/shopApi/Shop/getUserChannel", "/shopApi/Shop/getGoodsPrice":
					if started.Add(1) == 2 {
						close(bothStarted)
					}
					select {
					case <-bothStarted:
					case <-r.Context().Done():
						return nil, r.Context().Err()
					}
					if tt.failurePath != "" && r.URL.Path != tt.failurePath {
						<-r.Context().Done()
						siblingCancelled.Store(true)
						return nil, r.Context().Err()
					}
					resp, err := transport.RoundTrip(r)
					finished.Add(1)
					return resp, err
				case "/shopApi/Pay/order":
					orders.Add(1)
					if finished.Load() != 2 {
						t.Error("order started before both preflight requests completed")
					}
				}
				return transport.RoundTrip(r)
			})

			result, err := p.CreatePayment(ctx, payment.CreatePaymentRequest{Amount: "5.00", PayerEmail: "buyer@example.com"})
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) || orders.Load() != 0 || !siblingCancelled.Load() || ctx.Err() != nil {
					t.Fatalf("error=%v orders=%d siblingCancelled=%t context=%v", err, orders.Load(), siblingCancelled.Load(), ctx.Err())
				}
				return
			}
			if err != nil || orders.Load() != 1 || result == nil || result.QRCode != "https://qr.alipay.com/cloudcat-test-1" {
				t.Fatalf("result=%+v error=%v orders=%d", result, err, orders.Load())
			}
		})
	}
}

func TestCloudcatQueryOrderReturnsPaidBeforeAndAfterCardDelivery(t *testing.T) {
	tests := []struct {
		name     string
		infoBody string
		wantCode string
	}{
		{name: "awaiting delivery", infoBody: `{"code":1,"data":{"response":{"trade_no":"CAT-1","total_amount":5,"cards":[]}}}`},
		{name: "card delivered", infoBody: `{"code":1,"data":{"response":{"trade_no":"CAT-1","total_amount":5,"cards":["CDK-123"]}}}`, wantCode: "CDK-123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, _, closeServer := newCloudcatTestProvider(t, map[string]string{
				"/shopApi/Pay/query":  `{"code":1,"data":{"trade_no":"CAT-1","total_amount":5}}`,
				"/shopApi/Order/info": tt.infoBody,
			})
			defer closeServer()

			result, err := provider.QueryOrder(context.Background(), "CAT-1")
			if err != nil {
				t.Fatalf("QueryOrder returned error: %v", err)
			}
			if result.Status != payment.ProviderStatusPaid || result.Amount != 5 {
				t.Fatalf("unexpected result: %+v", result)
			}
			if got := result.Metadata[payment.MetadataCloudcatRechargeCode]; got != tt.wantCode {
				t.Fatalf("metadata code = %q, want %q", got, tt.wantCode)
			}
		})
	}
}

func TestCloudcatRejectsUntrustedPaymentURL(t *testing.T) {
	provider, _, closeServer := newCloudcatTestProvider(t, map[string]string{
		"/shopApi/Shop/goodsInfo":      `{"code":1,"data":{"goods_type":"card","price":5,"status":1,"stock":8,"user":{"token":"merchant-1"}}}`,
		"/shopApi/Shop/getUserChannel": `{"code":1,"data":[{"id":1,"code":"AlipayPc","status":1}]}`,
		"/shopApi/Shop/getGoodsPrice":  `{"code":1,"data":{"total_amount":5}}`,
		"/shopApi/Pay/order":           `{"code":1,"data":{"trade_no":"CAT-1","payurl":"https://evil.example/pay/CAT-1","total_amount":5}}`,
	})
	defer closeServer()

	if _, err := provider.CreatePayment(context.Background(), payment.CreatePaymentRequest{Amount: "5.00", PayerEmail: "buyer@example.com"}); err == nil {
		t.Fatal("CreatePayment should reject an untrusted payment URL")
	}
}

func TestCloudcatVisitorIDSurvivesProviderRecreation(t *testing.T) {
	config := map[string]string{
		"channelId": "1",
		"products":  `[{"amount":"5.00","itemUrl":"https://catfk.com/item/ee9c69"}]`,
	}
	first, err := NewCloudcat("1", config)
	if err != nil {
		t.Fatalf("NewCloudcat returned error: %v", err)
	}
	second, err := NewCloudcat("1", config)
	if err != nil {
		t.Fatalf("NewCloudcat returned error: %v", err)
	}
	other, err := NewCloudcat("2", config)
	if err != nil {
		t.Fatalf("NewCloudcat returned error: %v", err)
	}

	if first.visitorID != second.visitorID {
		t.Fatalf("visitor ID changed across provider recreation: %q != %q", first.visitorID, second.visitorID)
	}
	if first.visitorID == other.visitorID {
		t.Fatalf("different provider instances share visitor ID %q", first.visitorID)
	}
}

func TestExtractCloudcatPaymentQRCodeValidatesAlipayPayload(t *testing.T) {
	qrCode, err := extractCloudcatPaymentQRCode(strings.NewReader(`<input value="https://qr.alipay.com/order?a=1&amp;b=2" id="J_qrCode">`))
	if err != nil {
		t.Fatalf("extractCloudcatPaymentQRCode returned error: %v", err)
	}
	if qrCode != "https://qr.alipay.com/order?a=1&b=2" {
		t.Fatalf("qr code = %q", qrCode)
	}

	_, err = extractCloudcatPaymentQRCode(strings.NewReader(`<input id="J_qrCode" value="https://evil.example/order">`))
	if err == nil {
		t.Fatal("extractCloudcatPaymentQRCode should reject an untrusted QR host")
	}
}

func newCloudcatTestProvider(t *testing.T, responses map[string]string) (*Cloudcat, map[string]map[string]any, func()) {
	t.Helper()
	requests := make(map[string]map[string]any)
	var requestsMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.Header.Get("Content-Type"), "application/json") {
			var payload map[string]any
			decoder := json.NewDecoder(r.Body)
			decoder.UseNumber()
			if err := decoder.Decode(&payload); err != nil {
				t.Errorf("decode %s request: %v", r.URL.Path, err)
			}
			requestsMu.Lock()
			requests[r.URL.Path] = payload
			requestsMu.Unlock()
			if len(r.Header.Get("Visitorid")) != 9 {
				t.Errorf("Visitorid length = %d, want 9", len(r.Header.Get("Visitorid")))
			}
		}
		body, ok := responses[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/shopApi/") {
			w.Header().Set("Content-Type", "application/json")
		} else {
			w.Header().Set("Content-Type", "text/html")
		}
		_, _ = w.Write([]byte(body))
	}))
	provider, err := NewCloudcat("1", map[string]string{
		"channelId": "1",
		"products":  `[{"amount":"5.00","itemUrl":"https://catfk.com/item/ee9c69"}]`,
	})
	if err != nil {
		server.Close()
		t.Fatalf("NewCloudcat returned error: %v", err)
	}
	provider.apiBase = server.URL
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		server.Close()
		t.Fatalf("parse test server URL: %v", err)
	}
	provider.checkoutClient.Transport = cloudcatTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		cloned := r.Clone(r.Context())
		rewritten := *r.URL
		rewritten.Scheme = serverURL.Scheme
		rewritten.Host = serverURL.Host
		cloned.URL = &rewritten
		resp, err := http.DefaultTransport.RoundTrip(cloned)
		if resp != nil {
			resp.Request = r
		}
		return resp, err
	})
	provider.httpClient.Transport = provider.checkoutClient.Transport
	return provider, requests, server.Close
}

type cloudcatTestRoundTripper func(*http.Request) (*http.Response, error)

func (f cloudcatTestRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCloudcatCheckoutReusesConnectionsWithIsolatedCookies(t *testing.T) {
	t.Setenv("CLOUDCAT_PROXY_URL", "")
	t.Setenv("CLOUDCAT_API_PROXY_URL", "")
	var connections atomic.Int32
	cookies := make(chan string, 3)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookies <- r.Header.Get("Cookie")
		if r.URL.Path == "/first" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "first", Path: "/"})
		}
		_, _ = io.WriteString(w, "ok")
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	defer server.Close()
	config := map[string]string{"channelId": "1", "products": `[{"amount":"5.00","itemUrl":"https://catfk.com/item/ee9c69"}]`}
	first, err := NewCloudcat("first", config)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewCloudcat("second", config)
	if err != nil {
		t.Fatal(err)
	}
	if first.checkoutClient == second.checkoutClient || first.checkoutClient.Jar == second.checkoutClient.Jar {
		t.Fatal("checkout clients and cookie jars must remain independent")
	}
	if first.checkoutClient.Transport != second.checkoutClient.Transport {
		t.Fatal("same proxy configuration must reuse the browser transport")
	}
	transport, ok := first.checkoutClient.Transport.(*req.Transport)
	if !ok || transport.TLSHandshakeContext == nil {
		t.Fatal("checkout must retain the browser TLS transport")
	}
	for _, client := range []*http.Client{first.checkoutClient, second.checkoutClient} {
		if client.Timeout != cloudcatCheckoutTimeout {
			t.Fatalf("checkout timeout = %v", client.Timeout)
		}
		redirect, _ := http.NewRequest(http.MethodGet, "https://evil.example/", nil)
		if client.CheckRedirect == nil || client.CheckRedirect(redirect, nil) == nil {
			t.Fatal("checkout must reject untrusted redirects")
		}
	}
	for i, client := range []*http.Client{first.checkoutClient, second.checkoutClient, first.checkoutClient} {
		path := "/later"
		if i == 0 {
			path = "/first"
		}
		resp, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := connections.Load(); got != 1 {
		t.Fatalf("three requests across two providers opened %d connections, want 1", got)
	}
	for i, want := range []string{"", "", "session=first"} {
		if got := <-cookies; got != want {
			t.Fatalf("request %d cookie = %q, want %q", i, got, want)
		}
	}
}

func TestCloudcatCheckoutTransportProxyIsolation(t *testing.T) {
	t.Setenv("CLOUDCAT_API_PROXY_URL", "")
	config := map[string]string{"channelId": "1", "products": `[{"amount":"5.00","itemUrl":"https://catfk.com/item/ee9c69"}]`}
	requests := make(chan string, 3)
	proxies := make([]*httptest.Server, 2)
	for i := range proxies {
		proxies[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests <- fmt.Sprintf("%d %s", i, r.Header.Get("Proxy-Authorization"))
			_, _ = io.WriteString(w, "proxied")
		}))
		defer proxies[i].Close()
	}
	var providers []*Cloudcat
	for _, proxyURL := range []string{proxies[0].URL, strings.Replace(proxies[0].URL, "://", "://buyer:secret@", 1), proxies[1].URL} {
		t.Setenv("CLOUDCAT_PROXY_URL", proxyURL)
		p, err := NewCloudcat("proxy-isolation", config)
		if err != nil {
			t.Fatal(err)
		}
		for _, previous := range providers {
			if previous.checkoutClient.Transport == p.checkoutClient.Transport {
				t.Fatal("different proxy endpoints or credentials must not share transports")
			}
		}
		providers = append(providers, p)
	}
	for i, want := range []string{"0 ", "0 Basic YnV5ZXI6c2VjcmV0", "1 "} {
		resp, err := providers[i].checkoutClient.Get("http://cloudcat-checkout-test.invalid/")
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if got := <-requests; got != want {
			t.Fatalf("proxy route/auth = %q, want %q", got, want)
		}
	}
}

func TestCloudcatCheckoutTransportConcurrentConstruction(t *testing.T) {
	proxy := httptest.NewServer(http.NotFoundHandler())
	defer proxy.Close()
	t.Setenv("CLOUDCAT_PROXY_URL", proxy.URL)
	t.Setenv("CLOUDCAT_API_PROXY_URL", "")
	config := map[string]string{"channelId": "1", "products": `[{"amount":"5.00","itemUrl":"https://catfk.com/item/ee9c69"}]`}
	const count = 16
	providers := make([]*Cloudcat, count)
	var wg sync.WaitGroup
	for i := range providers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := NewCloudcat("concurrent", config)
			if err != nil {
				t.Error(err)
				return
			}
			providers[i] = p
		}()
	}
	wg.Wait()
	for _, p := range providers {
		if p == nil {
			t.Fatal("provider construction failed")
		}
	}
	for _, p := range providers[1:] {
		if p.checkoutClient.Transport != providers[0].checkoutClient.Transport {
			t.Fatal("concurrent construction must converge on the same transport")
		}
		if p.checkoutClient == providers[0].checkoutClient || p.checkoutClient.Jar == providers[0].checkoutClient.Jar {
			t.Fatal("concurrent construction must keep clients and cookies isolated")
		}
	}
}

func TestCloudcatProxyAppliesToBothClientsWithoutDirectFallback(t *testing.T) {
	requests := make(chan string, 8)
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Method + " " + r.Host
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxyServer.Close()
	t.Setenv("CLOUDCAT_PROXY_URL", proxyServer.URL)
	config := map[string]string{
		"channelId": "1",
		"products":  `[{"amount":"5.00","itemUrl":"https://catfk.com/item/ee9c69"}]`,
	}
	p, err := NewCloudcat("proxy-test", config)
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewCloudcat("other", config)
	if err != nil {
		t.Fatal(err)
	}
	if p.httpClient.Jar != p.checkoutClient.Jar || p.httpClient.Jar == other.httpClient.Jar {
		t.Fatal("checkout cookies must be shared within one provider, but isolated across providers")
	}
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("configured proxy was bypassed")
		w.WriteHeader(http.StatusOK)
	}))
	defer direct.Close()
	for _, client := range []*http.Client{p.httpClient, p.checkoutClient} {
		resp, err := client.Get(direct.URL)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("proxy response = %d", resp.StatusCode)
		}
		<-requests
		resp, err = client.Get("https://cloudcat-proxy-test.invalid/")
		if resp != nil {
			resp.Body.Close()
		}
		if err == nil {
			t.Fatal("failed HTTPS proxy must return an error")
		}
		if got := <-requests; got != "CONNECT cloudcat-proxy-test.invalid:443" {
			t.Fatalf("unexpected HTTPS proxy request: %s", got)
		}
	}
	t.Setenv("CLOUDCAT_PROXY_URL", "file:///invalid-proxy")
	if _, err := NewCloudcat("invalid", config); err == nil {
		t.Fatal("invalid proxy configuration must fail at construction")
	}
}

func TestCloudcatAPIProxyOverride(t *testing.T) {
	var apiCalls, checkoutCalls atomic.Int32
	apiProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer apiProxy.Close()
	checkoutProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkoutCalls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer checkoutProxy.Close()
	t.Setenv("CLOUDCAT_PROXY_URL", checkoutProxy.URL)
	t.Setenv("CLOUDCAT_API_PROXY_URL", apiProxy.URL)
	config := map[string]string{"channelId": "1", "products": `[{"amount":"5.00","itemUrl":"https://catfk.com/item/ee9c69"}]`}
	p, err := NewCloudcat("split-proxy-test", config)
	if err != nil {
		t.Fatal(err)
	}
	for _, client := range []*http.Client{p.httpClient, p.checkoutClient} {
		resp, err := client.Get("https://proxy-test.invalid/")
		if resp != nil {
			resp.Body.Close()
		}
		if err == nil {
			t.Fatal("failed proxy CONNECT must return an error")
		}
	}
	if apiCalls.Load() != 1 || checkoutCalls.Load() != 1 {
		t.Fatalf("wrong route: API calls=%d, checkout calls=%d", apiCalls.Load(), checkoutCalls.Load())
	}
	apiProxy.Close()
	if _, err := p.httpClient.Get("https://proxy-test.invalid/"); err == nil {
		t.Fatal("unavailable API proxy must not fall back")
	}
	if checkoutCalls.Load() != 1 {
		t.Fatal("unavailable API proxy fell back to the checkout proxy")
	}
	t.Setenv("CLOUDCAT_API_PROXY_URL", "file:///invalid-proxy")
	if _, err := NewCloudcat("invalid-api-proxy", config); err == nil {
		t.Fatal("invalid API proxy must fail at construction")
	}
}

func TestCloudcatCheckoutSeparatesCatFKAndAlipayTransports(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		t.Run(fmt.Sprintf("untrusted_redirect=%t", redirect), func(t *testing.T) {
			catCalls, alipayCalls := 0, 0
			p := &Cloudcat{
				httpClient: &http.Client{Transport: cloudcatTestRoundTripper(func(r *http.Request) (*http.Response, error) {
					catCalls++
					if r.URL.Host != "catfk.com" || r.Method != http.MethodGet {
						t.Fatalf("unexpected CatFK request: %s %s", r.Method, r.URL)
					}
					if redirect {
						return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://evil.example/pay"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
					}
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`<form action="https://openapi.alipay.com/gateway.do" method="POST"><input name="sign" value="signed"></form>`)), Request: r}, nil
				})},
				checkoutClient: &http.Client{Transport: cloudcatTestRoundTripper(func(r *http.Request) (*http.Response, error) {
					alipayCalls++
					if r.URL.Host != "openapi.alipay.com" || r.Method != http.MethodPost {
						t.Fatalf("unexpected Alipay request: %s %s", r.Method, r.URL)
					}
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`<input id="J_qrCode" value="https://qr.alipay.com/checkout-test">`)), Request: r}, nil
				})},
			}
			qrCode, err := p.ResolvePaymentQRCode(context.Background(), "https://catfk.com/pay/CAT-1")
			if catCalls != 1 {
				t.Fatalf("CatFK calls = %d, want 1", catCalls)
			}
			if redirect {
				if err == nil || alipayCalls != 0 {
					t.Fatalf("untrusted redirect: error=%v, Alipay calls=%d", err, alipayCalls)
				}
			} else if err != nil || qrCode != "https://qr.alipay.com/checkout-test" || alipayCalls != 1 {
				t.Fatalf("checkout: QR=%q, error=%v, Alipay calls=%d", qrCode, err, alipayCalls)
			}
		})
	}
}

func TestCloudcatRetriesOnlyTransientReadFailures(t *testing.T) {
	for _, tt := range []struct {
		name           string
		path           string
		status         int
		transportError bool
		wantCalls      int
		wantError      bool
	}{
		{"read EOF", "/shopApi/Shop/goodsInfo", 0, true, 3, false},
		{"read unavailable", "/shopApi/Pay/query", 503, false, 3, false},
		{"read rate limited", "/shopApi/Order/info", 429, false, 3, false},
		{"read rejected", "/shopApi/Shop/goodsInfo", 400, false, 1, true},
		{"create EOF is ambiguous", "/shopApi/Pay/order", 0, true, 1, true},
		{"create unavailable is not replayed", "/shopApi/Pay/order", 503, false, 1, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			p := &Cloudcat{apiBase: cloudcatAPIBase, httpClient: &http.Client{Transport: cloudcatTestRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls < 3 && tt.transportError {
					return nil, io.EOF
				}
				status := http.StatusOK
				if calls < 3 {
					status = tt.status
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"code":1}`)), Header: make(http.Header), Request: r}, nil
			})}}
			var result cloudcatOrderData
			err := p.post(context.Background(), tt.path, map[string]string{"trade_no": "CAT-1"}, &result)
			if (err != nil) != tt.wantError || calls != tt.wantCalls {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestCloudcatReadRetryStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	p := &Cloudcat{apiBase: cloudcatAPIBase, httpClient: &http.Client{Transport: cloudcatTestRoundTripper(func(*http.Request) (*http.Response, error) {
		calls++
		cancel()
		return nil, context.Canceled
	})}}
	var result cloudcatOrderData
	start := time.Now()
	err := p.post(ctx, "/shopApi/Pay/query", nil, &result)
	if !errors.Is(err, context.Canceled) || calls != 1 || time.Since(start) > time.Second {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
}

func TestCloudcatDoesNotStartAnOrderWithAnExhaustedPreflightBudget(t *testing.T) {
	p, requests, closeServer := newCloudcatTestProvider(t, map[string]string{
		"/shopApi/Shop/goodsInfo":      `{"code":1,"data":{"goods_type":"card","price":5,"status":1,"stock":8,"user":{"token":"merchant-1"}}}`,
		"/shopApi/Shop/getUserChannel": `{"code":1,"data":[{"id":1,"code":"AlipayPc","status":1}]}`,
		"/shopApi/Shop/getGoodsPrice":  `{"code":1,"data":{"total_amount":5}}`,
	})
	defer closeServer()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := p.CreatePayment(ctx, payment.CreatePaymentRequest{Amount: "5.00", PayerEmail: "buyer@example.com"})
	if err == nil || !strings.Contains(err.Error(), "preflight") {
		t.Fatalf("error=%v", err)
	}
	if _, created := requests["/shopApi/Pay/order"]; created {
		t.Fatal("order started without enough time to receive its trade number")
	}
}

func TestCloudcatPaidQueryRejectsInvalidOrderDetails(t *testing.T) {
	for _, info := range []string{
		`{"code":0,"data":{"total_amount":5,"cards":["CDK-123"]}}`,
		`{"code":1,"data":{"trade_no":"CAT-OTHER","total_amount":5,"cards":["CDK-123"]}}`,
	} {
		p, _, closeServer := newCloudcatTestProvider(t, map[string]string{
			"/shopApi/Pay/query":  `{"code":1,"data":{"trade_no":"CAT-1","total_amount":5}}`,
			"/shopApi/Order/info": info,
		})
		_, err := p.QueryOrder(context.Background(), "CAT-1")
		closeServer()
		if err == nil {
			t.Fatal("invalid paid order details accepted")
		}
	}
}
