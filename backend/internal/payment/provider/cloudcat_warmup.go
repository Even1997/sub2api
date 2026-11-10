package provider

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
	"github.com/imroc/req/v3"
	"golang.org/x/sync/errgroup"
)

const cloudcatWarmSlots = 2

type cloudcatWarmPoolKey struct {
	apiProxy, checkoutProxy string
}

var cloudcatWarmPools sync.Map

type cloudcatWarmSlot struct {
	api      *http.Transport
	checkout *req.Transport
}

func newCloudcatWarmSlot(key cloudcatWarmPoolKey) (*cloudcatWarmSlot, error) {
	api, err := httpclient.NewTransport(httpclient.Options{ProxyURL: key.apiProxy})
	if err != nil {
		return nil, err
	}
	return &cloudcatWarmSlot{api: api, checkout: newCloudcatCheckoutTransport(key.checkoutProxy)}, nil
}

func (s *cloudcatWarmSlot) close() {
	s.api.CloseIdleConnections()
	s.checkout.CloseIdleConnections()
}

type cloudcatWarmPool struct {
	key          cloudcatWarmPoolKey
	apiURL       string
	checkoutURLs []string
	interval     time.Duration
	mu           sync.Mutex
	ready        []*cloudcatWarmSlot
	wake         chan struct{}
	cancel       context.CancelFunc
	done         chan struct{}
	lastReady    int
}

// StartCloudcatWarmPool keeps two isolated connection pairs ready without
// creating orders or retaining cookies. The payment runtime owns its lifetime.
func StartCloudcatWarmPool(ctx context.Context) (func(), error) {
	checkoutProxy, apiProxy, err := cloudcatProxyURLs()
	if err != nil {
		return nil, err
	}
	p := newCloudcatWarmPool(cloudcatWarmPoolKey{apiProxy, checkoutProxy}, cloudcatAPIBase+"/",
		"https://openapi.alipay.com/", "https://unitradeprod.alipay.com/", "https://excashier.alipay.com/")
	if _, loaded := cloudcatWarmPools.LoadOrStore(p.key, p); loaded {
		return nil, fmt.Errorf("cloudcat warm pool is already running")
	}
	p.start(ctx)
	var once sync.Once
	return func() {
		once.Do(func() {
			cloudcatWarmPools.CompareAndDelete(p.key, p)
			p.cancel()
			<-p.done
		})
	}, nil
}

func newCloudcatWarmPool(key cloudcatWarmPoolKey, apiURL string, checkoutURLs ...string) *cloudcatWarmPool {
	return &cloudcatWarmPool{
		key: key, apiURL: apiURL, checkoutURLs: checkoutURLs,
		interval: 15 * time.Second, wake: make(chan struct{}, 1), done: make(chan struct{}), lastReady: -1,
	}
}

func (p *cloudcatWarmPool) start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	p.cancel = cancel
	go func() {
		defer close(p.done)
		defer func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			for _, slot := range p.ready {
				slot.close()
			}
			p.ready = nil
		}()
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()
		p.maintain(ctx, false)
		for {
			select {
			case <-ctx.Done():
				return
			case <-p.wake:
				p.maintain(ctx, false)
			case <-ticker.C:
				p.maintain(ctx, true)
			}
		}
	}()
}

func (p *cloudcatWarmPool) take() *cloudcatWarmSlot {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.ready) == 0 {
		return nil
	}
	slot := p.ready[len(p.ready)-1]
	p.ready = p.ready[:len(p.ready)-1]
	select {
	case p.wake <- struct{}{}:
	default:
	}
	return slot
}

func (p *cloudcatWarmPool) maintain(ctx context.Context, refresh bool) {
	p.mu.Lock()
	var slots []*cloudcatWarmSlot
	if refresh && len(p.ready) > 0 {
		// Refresh one pair at a time, keeping the other available to payments.
		slots, p.ready = []*cloudcatWarmSlot{p.ready[0]}, p.ready[1:]
	}
	for len(slots)+len(p.ready) < cloudcatWarmSlots {
		slots = append(slots, nil)
	}
	p.mu.Unlock()
	var jobs errgroup.Group
	for _, slot := range slots {
		jobs.Go(func() error {
			if slot == nil {
				var err error
				slot, err = newCloudcatWarmSlot(p.key)
				if err != nil {
					return nil
				}
			}
			if err := p.warm(ctx, slot); err != nil {
				slot.close()
				return nil
			}
			p.mu.Lock()
			defer p.mu.Unlock()
			if ctx.Err() != nil {
				slot.close()
			} else {
				p.ready = append(p.ready, slot)
			}
			return nil
		})
	}
	_ = jobs.Wait()
	p.mu.Lock()
	defer p.mu.Unlock()
	if count := len(p.ready); ctx.Err() == nil && count != p.lastReady {
		p.lastReady = count
		slog.Info("cloudcat connection warmup standby", "ready", count, "target", cloudcatWarmSlots)
	}
}

func (p *cloudcatWarmPool) warm(ctx context.Context, slot *cloudcatWarmSlot) error {
	ctx, cancel := context.WithTimeout(ctx, cloudcatCheckoutTimeout)
	defer cancel()
	jobs, ctx := errgroup.WithContext(ctx)
	probe := func(target string, transport http.RoundTripper) error {
		r, err := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
		if err != nil {
			return err
		}
		r.Header.Set("User-Agent", cloudcatCheckoutUA)
		client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, err := client.Do(r)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.Close || resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
			return fmt.Errorf("cloudcat warmup connection is unavailable")
		}
		_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, cloudcatMaxResponseSize+1))
		return err
	}
	jobs.Go(func() error { return probe(p.apiURL, slot.api) })
	jobs.Go(func() error {
		// req mutates shared HTTP/2 settings when establishing a new connection.
		for _, target := range p.checkoutURLs {
			if err := probe(target, slot.checkout); err != nil {
				return err
			}
		}
		return nil
	})
	return jobs.Wait()
}

func (c *Cloudcat) withWarmConnections() (*Cloudcat, func()) {
	if c.usingWarmSlot || c.isolatedCheckout {
		return c, func() {}
	}
	cached, ok := cloudcatWarmPools.Load(c.warmPoolKey)
	if !ok {
		return c.withIsolatedCheckout()
	}
	slot := cached.(*cloudcatWarmPool).take()
	if slot == nil {
		// Never wait for background warmup on the payment's critical path.
		var err error
		slot, err = newCloudcatWarmSlot(c.warmPoolKey)
		if err != nil {
			return c.withIsolatedCheckout()
		}
	}
	client := *c
	api, checkout := *c.httpClient, *c.checkoutClient
	jar, _ := cookiejar.New(nil)
	api.Jar, checkout.Jar = jar, jar
	api.Transport = servertiming.WrapRoundTripper(slot.api)
	checkout.Transport = slot.checkout
	client.httpClient, client.checkoutClient = &api, &checkout
	client.usingWarmSlot = true
	return &client, slot.close
}

// withIsolatedCheckout gives one payment its own req.Transport. The cached
// transport is safe to construct once, but req mutates its HTTP/2 settings
// while opening a connection, so concurrent checkouts cannot share it.
func (c *Cloudcat) withIsolatedCheckout() (*Cloudcat, func()) {
	if c == nil || c.checkoutClient == nil || !cloudcatCheckoutTransportIsShared(c.checkoutClient.Transport) {
		return c, func() {}
	}
	transport := newCloudcatCheckoutTransport(c.warmPoolKey.checkoutProxy)
	client := *c
	checkout := *c.checkoutClient
	checkout.Transport = transport
	client.checkoutClient = &checkout
	client.isolatedCheckout = true
	return &client, transport.CloseIdleConnections
}
