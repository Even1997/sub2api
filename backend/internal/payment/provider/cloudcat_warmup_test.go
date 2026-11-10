package provider

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

func waitCloudcatWarmSlots(t *testing.T, p *cloudcatWarmPool, count int) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		p.mu.Lock()
		ready := len(p.ready)
		p.mu.Unlock()
		if ready == count {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("ready slots = %d, want %d", ready, count)
		case <-ticker.C:
		}
	}
}

func TestCloudcatWarmPoolLifecycle(t *testing.T) {
	t.Setenv("CLOUDCAT_PROXY_URL", "")
	t.Setenv("CLOUDCAT_API_PROXY_URL", "")
	var opened, closed, probes atomic.Int32
	cookies := make(chan string, 8)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			probes.Add(1)
			if r.Header.Get("Cookie") != "" {
				t.Error("warmup must never send payment cookies")
			}
			http.SetCookie(w, &http.Cookie{Name: "warmup", Value: "discard", Path: "/"})
			return
		}
		cookies <- r.Header.Get("Cookie")
		http.SetCookie(w, &http.Cookie{Name: "payment", Value: "private", Path: "/"})
		_, _ = io.WriteString(w, "ok")
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			opened.Add(1)
		}
		if state == http.StateClosed {
			closed.Add(1)
		}
	}
	server.Start()
	defer server.Close()
	p := newCloudcatWarmPool(cloudcatWarmPoolKey{}, server.URL, server.URL)
	p.start(context.Background())
	cloudcatWarmPools.Store(p.key, p)
	t.Cleanup(func() {
		cloudcatWarmPools.CompareAndDelete(p.key, p)
		p.cancel()
		<-p.done
	})
	waitCloudcatWarmSlots(t, p, 2)
	if opened.Load() != 4 {
		t.Fatalf("two warm connection pairs opened %d connections, want 4", opened.Load())
	}
	config := map[string]string{"channelId": "1", "products": `[{"amount":"5.00","itemUrl":"https://catfk.com/item/ee9c69"}]`}
	base, err := NewCloudcat("warm-test", config)
	if err != nil {
		t.Fatal(err)
	}
	client, release := base.withWarmConnections()
	defer release()
	if client == base || !client.usingWarmSlot || client.httpClient.Jar != client.checkoutClient.Jar || client.httpClient.Jar == base.httpClient.Jar {
		t.Fatal("lease must use a provider copy with a new isolated session")
	}
	// The nested QR resolver must keep the same lease, not consume a second slot.
	nested, nestedRelease := client.withWarmConnections()
	nestedRelease()
	if nested != client {
		t.Fatal("nested checkout acquired another slot")
	}
	for _, httpClient := range []*http.Client{client.httpClient, client.checkoutClient} {
		var reused bool
		ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused }})
		r, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		resp, err := httpClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if err != nil || !reused {
			t.Fatalf("payment did not reuse a warm connection: reused=%t err=%v", reused, err)
		}
	}
	if first, second := <-cookies, <-cookies; first != "" || second != "payment=private" {
		t.Fatalf("warmup/payment cookie isolation failed: first=%q second=%q", first, second)
	}
	other, err := NewCloudcat("other", config)
	if err != nil {
		t.Fatal(err)
	}
	if other.httpClient.Jar == client.httpClient.Jar {
		t.Fatal("payment instances share cookies")
	}
	waitCloudcatWarmSlots(t, p, 2)
	if opened.Load() != 6 {
		t.Fatalf("claimed slot was not replaced: opened=%d, want 6", opened.Load())
	}
	release()
	deadline := time.Now().Add(time.Second)
	for closed.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if closed.Load() != 2 {
		t.Fatalf("used pair did not close: closed=%d", closed.Load())
	}
	// A periodic refresh must keep the standby connections alive, not replace them.
	p.mu.Lock()
	before := append([]*cloudcatWarmSlot(nil), p.ready...)
	p.mu.Unlock()
	p.maintain(context.Background(), true)
	p.maintain(context.Background(), true)
	if opened.Load() != 6 || probes.Load() != 10 {
		t.Fatalf("keepalive opened extra connections or missed a probe: opened=%d probes=%d", opened.Load(), probes.Load())
	}
	p.mu.Lock()
	for _, slot := range p.ready {
		if slot != before[0] && slot != before[1] {
			t.Error("keepalive replaced a healthy slot")
		}
	}
	p.mu.Unlock()
	p.cancel()
	<-p.done
	waitCloudcatWarmSlots(t, p, 0)
}

func TestCloudcatWarmPoolFailureAndFallback(t *testing.T) {
	var calls atomic.Int32
	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer fail.Close()
	p := newCloudcatWarmPool(cloudcatWarmPoolKey{}, fail.URL, fail.URL)
	p.maintain(context.Background(), false)
	waitCloudcatWarmSlots(t, p, 0)
	if calls.Load() < 2 || calls.Load() > 4 {
		t.Fatalf("unexpected warmup request count %d", calls.Load())
	}
	// A broken probe is retried on a later maintenance cycle, not published.
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ok.Close()
	p.apiURL, p.checkoutURLs = ok.URL, []string{ok.URL}
	p.maintain(context.Background(), false)
	waitCloudcatWarmSlots(t, p, 2)
	first, second := p.take(), p.take()
	defer first.close()
	defer second.close()
	if first == second || p.take() != nil {
		t.Fatal("slots must be exclusively acquired")
	}
	cloudcatWarmPools.Store(p.key, p)
	defer cloudcatWarmPools.CompareAndDelete(p.key, p)
	t.Setenv("CLOUDCAT_PROXY_URL", "")
	t.Setenv("CLOUDCAT_API_PROXY_URL", "")
	c, err := NewCloudcat("fallback", map[string]string{"channelId": "1", "products": `[{"amount":"5.00","itemUrl":"https://catfk.com/item/ee9c69"}]`})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	client, release := c.withWarmConnections()
	defer release()
	if !client.usingWarmSlot || time.Since(start) > time.Second {
		t.Fatal("empty pool must immediately provide an independently owned cold connection pair")
	}
	if client.checkoutClient.Transport == first.checkout || client.checkoutClient.Transport == second.checkout {
		t.Fatal("fallback reused another payment's occupied slot")
	}
	// Different proxy identities must not consume this pool's slots.
	p.ready = append(p.ready, first)
	c.warmPoolKey.checkoutProxy = "http://different-proxy.invalid:80"
	unmatched, unmatchedRelease := c.withWarmConnections()
	unmatchedRelease()
	if unmatched.usingWarmSlot || unmatched.checkoutClient.Transport == c.checkoutClient.Transport || len(p.ready) != 1 {
		t.Fatal("provider consumed a slot configured for another proxy or reused the shared checkout transport")
	}
	p.ready = nil
}

func TestCloudcatCheckoutFallbackDoesNotShareTransport(t *testing.T) {
	t.Setenv("CLOUDCAT_PROXY_URL", "")
	t.Setenv("CLOUDCAT_API_PROXY_URL", "")
	c, err := NewCloudcat("isolated", map[string]string{"channelId": "1", "products": `[{"amount":"5.00","itemUrl":"https://catfk.com/item/ee9c69"}]`})
	if err != nil {
		t.Fatal(err)
	}
	c.warmPoolKey.checkoutProxy = "http://isolated-fallback.invalid:9"
	first, releaseFirst := c.withWarmConnections()
	second, releaseSecond := c.withWarmConnections()
	defer releaseFirst()
	defer releaseSecond()
	if first == c || second == c || first == second {
		t.Fatal("fallback payments must not share a client")
	}
	if first.usingWarmSlot || second.usingWarmSlot || !first.isolatedCheckout || !second.isolatedCheckout {
		t.Fatal("fallback must isolate checkout without taking a warm slot")
	}
	if first.checkoutClient.Transport == c.checkoutClient.Transport || second.checkoutClient.Transport == c.checkoutClient.Transport || first.checkoutClient.Transport == second.checkoutClient.Transport {
		t.Fatal("fallback payments must not share a req.Transport")
	}
	nested, nestedRelease := first.withWarmConnections()
	nestedRelease()
	if nested != first || nested.checkoutClient.Transport != first.checkoutClient.Transport {
		t.Fatal("nested checkout must keep the isolated transport")
	}
}

func TestCloudcatWarmPoolTimerAndCancellation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	p := newCloudcatWarmPool(cloudcatWarmPoolKey{}, server.URL, server.URL)
	p.interval = 15 * time.Millisecond
	p.start(context.Background())
	defer func() { p.cancel(); <-p.done }()
	deadline := time.Now().Add(time.Second)
	for calls.Load() < 8 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if calls.Load() < 8 {
		t.Fatal("periodic keepalive did not probe both standby pairs")
	}
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer blocked.Close()
	q := newCloudcatWarmPool(cloudcatWarmPoolKey{}, blocked.URL, blocked.URL)
	q.start(context.Background())
	q.cancel()
	select {
	case <-q.done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel warmup promptly")
	}
}

func TestCloudcatWarmPoolConcurrentPayments(t *testing.T) {
	t.Setenv("CLOUDCAT_PROXY_URL", "")
	t.Setenv("CLOUDCAT_API_PROXY_URL", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" {
			t.Error("concurrent payments shared cookies")
		}
		http.SetCookie(w, &http.Cookie{Name: "payment", Value: "private"})
	}))
	defer server.Close()
	p := newCloudcatWarmPool(cloudcatWarmPoolKey{}, server.URL, server.URL)
	p.start(context.Background())
	cloudcatWarmPools.Store(p.key, p)
	defer func() {
		cloudcatWarmPools.CompareAndDelete(p.key, p)
		p.cancel()
		<-p.done
	}()
	waitCloudcatWarmSlots(t, p, 2)
	c, err := NewCloudcat("shared-provider", map[string]string{"channelId": "1", "products": `[{"amount":"5.00","itemUrl":"https://catfk.com/item/ee9c69"}]`})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	active := make(map[http.RoundTripper]bool)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client, release := c.withWarmConnections()
			defer release()
			transport := client.checkoutClient.Transport
			mu.Lock()
			if active[transport] {
				t.Error("two payments acquired the same transport")
			}
			active[transport] = true
			mu.Unlock()
			resp, err := client.checkoutClient.Get(server.URL)
			if err != nil {
				t.Error(err)
			} else {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
			mu.Lock()
			delete(active, transport)
			mu.Unlock()
		}()
	}
	wg.Wait()
	waitCloudcatWarmSlots(t, p, 2)
}

func TestCloudcatWarmPoolTLSConnectionReuse(t *testing.T) {
	for _, http2 := range []bool{false, true} {
		t.Run(map[bool]string{false: "http1", true: "http2"}[http2], func(t *testing.T) {
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			server.EnableHTTP2 = http2
			server.StartTLS()
			defer server.Close()
			slot, err := newCloudcatWarmSlot(cloudcatWarmPoolKey{})
			if err != nil {
				t.Fatal(err)
			}
			defer slot.close()
			roots := x509.NewCertPool()
			roots.AddCert(server.Certificate())
			slot.api.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
			slot.checkout.TLSClientConfig.RootCAs = roots
			p := newCloudcatWarmPool(cloudcatWarmPoolKey{}, server.URL, server.URL)
			if err := p.warm(context.Background(), slot); err != nil {
				t.Fatal(err)
			}
			for _, transport := range []http.RoundTripper{slot.api, slot.checkout} {
				var reused bool
				ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused }})
				r, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, strings.NewReader("signed-form"))
				resp, err := (&http.Client{Transport: transport}).Do(r)
				if err != nil {
					t.Fatal(err)
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if !reused {
					t.Fatal("payment POST did not reuse the prewarmed TLS connection")
				}
			}
		})
	}
}

func TestCloudcatWarmPoolReusesEveryCheckoutHost(t *testing.T) {
	for _, http2 := range []bool{false, true} {
		t.Run(map[bool]string{false: "http1", true: "http2"}[http2], func(t *testing.T) {
			t.Setenv("CLOUDCAT_PROXY_URL", "")
			t.Setenv("CLOUDCAT_API_PROXY_URL", "")
			roots := x509.NewCertPool()
			var targets []string
			var probes, closed atomic.Int32
			for range 3 {
				server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodHead {
						probes.Add(1)
						if r.Header.Get("Cookie") != "" {
							t.Error("standby probes sent cookies")
						}
						http.SetCookie(w, &http.Cookie{Name: "warmup", Value: "discard", Path: "/"})
					} else if strings.Contains(r.Header.Get("Cookie"), "warmup=") {
						t.Error("checkout retained standby cookies")
					}
				}))
				server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
					if state == http.StateClosed {
						closed.Add(1)
					}
				}
				server.EnableHTTP2 = http2
				server.StartTLS()
				t.Cleanup(server.Close)
				roots.AddCert(server.Certificate())
				targets = append(targets, server.URL)
			}
			slot, err := newCloudcatWarmSlot(cloudcatWarmPoolKey{})
			if err != nil {
				t.Fatal(err)
			}
			defer slot.close()
			slot.api.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
			slot.checkout.TLSClientConfig.RootCAs = roots
			p := newCloudcatWarmPool(cloudcatWarmPoolKey{}, targets[0], targets...)
			for range 2 {
				if err := p.warm(context.Background(), slot); err != nil {
					t.Fatal(err)
				}
			}
			if probes.Load() != 8 {
				t.Fatalf("warmup and refresh probed %d targets, want 8", probes.Load())
			}
			p.ready = []*cloudcatWarmSlot{slot}
			cloudcatWarmPools.Store(p.key, p)
			defer cloudcatWarmPools.CompareAndDelete(p.key, p)
			base, err := NewCloudcat("checkout-hosts", map[string]string{"channelId": "1", "products": `[{"amount":"5.00","itemUrl":"https://catfk.com/item/test"}]`})
			if err != nil {
				t.Fatal(err)
			}
			client, release := base.withWarmConnections()
			defer release()
			for i, target := range append([]string{p.apiURL}, targets...) {
				httpClient := client.checkoutClient
				if i == 0 {
					httpClient = client.httpClient
				}
				var reused bool
				ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused }})
				r, _ := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader("payment"))
				resp, err := httpClient.Do(r)
				if err != nil {
					t.Fatal(err)
				}
				_, err = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if err != nil || !reused {
					t.Fatalf("target %d did not reuse its warm connection: reused=%t error=%v", i, reused, err)
				}
			}
			release()
			deadline := time.Now().Add(time.Second)
			for closed.Load() < 4 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if closed.Load() != 4 {
				t.Fatalf("released lease closed %d connections, want 4", closed.Load())
			}
		})
	}
}

func TestCloudcatWarmPoolDiscardsPartiallyWarmedSlots(t *testing.T) {
	var opened, closed, lastHostCalls atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/last" {
			lastHostCalls.Add(1)
			if fail.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
		}
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			opened.Add(1)
		}
		if state == http.StateClosed {
			closed.Add(1)
		}
	}
	server.Start()
	defer server.Close()
	p := newCloudcatWarmPool(cloudcatWarmPoolKey{}, server.URL, server.URL+"/first", server.URL+"/last")
	defer func() {
		for _, slot := range p.ready {
			slot.close()
		}
	}()
	p.maintain(context.Background(), false)
	if len(p.ready) != 0 || lastHostCalls.Load() != 2 {
		t.Fatalf("partial warmup published slots or missed final probes: ready=%d calls=%d", len(p.ready), lastHostCalls.Load())
	}
	deadline := time.Now().Add(time.Second)
	for closed.Load() < opened.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if opened.Load() != 4 || closed.Load() != opened.Load() {
		t.Fatalf("partial warmup leaked connections: opened=%d closed=%d", opened.Load(), closed.Load())
	}
	fail.Store(false)
	p.maintain(context.Background(), false)
	if len(p.ready) != cloudcatWarmSlots {
		t.Fatalf("warmup did not recover after final host became available: ready=%d", len(p.ready))
	}
}

func TestCloudcatWarmPoolReleasesFailedAndCancelledPayments(t *testing.T) {
	for _, mode := range []string{"preflight failure", "creation cancelled", "QR cancelled"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("CLOUDCAT_PROXY_URL", "")
			t.Setenv("CLOUDCAT_API_PROXY_URL", "")
			var closed atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodHead {
					_, _ = io.WriteString(w, `{"code":0,"msg":"unavailable"}`)
				}
			}))
			server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateClosed {
					closed.Add(1)
				}
			}
			server.Start()
			defer server.Close()
			p := newCloudcatWarmPool(cloudcatWarmPoolKey{}, server.URL, server.URL)
			p.maintain(context.Background(), false)
			waitCloudcatWarmSlots(t, p, 2)
			cloudcatWarmPools.Store(p.key, p)
			defer func() {
				cloudcatWarmPools.CompareAndDelete(p.key, p)
				for _, slot := range p.ready {
					slot.close()
				}
			}()
			c, err := NewCloudcat("release", map[string]string{"channelId": "1", "products": `[{"amount":"5.00","itemUrl":"https://catfk.com/item/ee9c69"}]`})
			if err != nil {
				t.Fatal(err)
			}
			c.apiBase = server.URL
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode != "preflight failure" {
				cancel()
			}
			if mode == "QR cancelled" {
				_, err = c.ResolvePaymentQRCode(ctx, "https://catfk.com/pay/test")
			} else {
				_, err = c.CreatePayment(ctx, payment.CreatePaymentRequest{Amount: "5.00", PayerEmail: "buyer@example.com"})
			}
			if err == nil || len(p.ready) != 1 {
				t.Fatalf("payment did not exclusively consume one slot: err=%v remaining=%d", err, len(p.ready))
			}
			deadline := time.Now().Add(time.Second)
			for closed.Load() < 2 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if closed.Load() != 2 {
				t.Fatalf("failed/cancelled payment left its connections open: closed=%d", closed.Load())
			}
		})
	}
}

// Opt-in diagnostics issue only unsigned HEAD requests, never create orders.
func TestCloudcatWarmPoolLive(t *testing.T) {
	if os.Getenv("CLOUDCAT_WARMUP_LIVE_TEST") != "1" {
		t.Skip("set CLOUDCAT_WARMUP_LIVE_TEST=1 to probe the configured route")
	}
	checkoutProxy, apiProxy, err := cloudcatProxyURLs()
	if err != nil {
		t.Fatal(err)
	}
	slot, err := newCloudcatWarmSlot(cloudcatWarmPoolKey{apiProxy, checkoutProxy})
	if err != nil {
		t.Fatal(err)
	}
	defer slot.close()
	p := newCloudcatWarmPool(cloudcatWarmPoolKey{}, cloudcatAPIBase+"/", "https://openapi.alipay.com/")
	for round := range 2 {
		for i, transport := range []http.RoundTripper{slot.api, slot.checkout} {
			target := []string{p.apiURL, p.checkoutURLs[0]}[i]
			var reused bool
			ctx, cancel := context.WithTimeout(context.Background(), cloudcatCheckoutTimeout)
			ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused }})
			r, _ := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
			r.Header.Set("User-Agent", cloudcatCheckoutUA)
			start := time.Now()
			resp, err := (&http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(r)
			if err != nil {
				cancel()
				t.Fatalf("round %d target %d: request failed", round, i)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			cancel()
			t.Logf("round=%d target=%d status=%d protocol=%s reused=%t close=%t duration=%s", round, i, resp.StatusCode, resp.Proto, reused, resp.Close, time.Since(start))
			if resp.Close || (round == 1 && !reused) {
				t.Fatal("route did not retain a reusable connection")
			}
		}
	}
}

func TestCloudcatWarmPoolLiveKeepalive(t *testing.T) {
	if os.Getenv("CLOUDCAT_WARMUP_LIVE_TEST") != "1" || os.Getenv("CLOUDCAT_WARMUP_LIVE_IDLE_TEST") != "1" {
		t.Skip("opt-in 95-second standby probe")
	}
	stop, err := StartCloudcatWarmPool(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	checkoutProxy, apiProxy, _ := cloudcatProxyURLs()
	cached, _ := cloudcatWarmPools.Load(cloudcatWarmPoolKey{apiProxy, checkoutProxy})
	p := cached.(*cloudcatWarmPool)
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		ready := len(p.ready)
		p.mu.Unlock()
		if ready == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	waitCloudcatWarmSlots(t, p, 2)
	p.mu.Lock()
	initial := append([]*cloudcatWarmSlot(nil), p.ready...)
	p.mu.Unlock()
	t.Log("standby ready; verifying connections after 95 seconds without payments")
	<-time.After(95 * time.Second)
	slot := p.take()
	if slot == nil {
		t.Fatal("no standby slot after long idle")
	}
	defer slot.close()
	if slot != initial[0] && slot != initial[1] {
		t.Fatal("standby connection pair had to be replaced during idle")
	}
	for i, transport := range []http.RoundTripper{slot.api, slot.checkout} {
		var reused bool
		ctx, cancel := context.WithTimeout(context.Background(), cloudcatCheckoutTimeout)
		ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused }})
		r, _ := http.NewRequestWithContext(ctx, http.MethodHead, []string{p.apiURL, p.checkoutURLs[0]}[i], nil)
		r.Header.Set("User-Agent", cloudcatCheckoutUA)
		resp, err := (&http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(r)
		if err != nil {
			cancel()
			t.Fatalf("target %d: request failed after idle", i)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		cancel()
		t.Logf("after_idle target=%d status=%d reused=%t close=%t", i, resp.StatusCode, reused, resp.Close)
		if !reused || resp.Close {
			t.Fatal("keepalive did not retain a reusable connection past the default idle timeout")
		}
	}
}
