package provider

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
	"github.com/imroc/req/v3"
	"golang.org/x/net/html"
	"golang.org/x/sync/errgroup"
)

const (
	cloudcatAPIBase         = "https://catfk.com"
	cloudcatHTTPTimeout     = 12 * time.Second
	cloudcatCreateTimeout   = 50 * time.Second
	cloudcatCheckoutTimeout = 8 * time.Second
	cloudcatQRCodeTimeout   = 12 * time.Second
	cloudcatMaxResponseSize = 1 << 20
	cloudcatCheckoutUA      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

var cloudcatCheckoutTransports sync.Map

type cloudcatProductConfig struct {
	Amount  string `json:"amount"`
	ItemURL string `json:"itemUrl"`
}

type cloudcatProduct struct {
	amountMinor int64
	goodsKey    string
	itemURL     string
}

type cloudcatPaymentForm struct {
	action string
	values url.Values
}

// Cloudcat implements the undocumented buyer-side CatFK JSON contract.
// It deliberately has no webhook or refund support; Sub2API reconciles the
// upstream order until CatFK exposes the purchased redeem code.
type Cloudcat struct {
	instanceID       string
	channelID        int64
	products         map[int64]cloudcatProduct
	apiBase          string
	httpClient       *http.Client
	checkoutClient   *http.Client
	visitorID        string
	warmPoolKey      cloudcatWarmPoolKey
	usingWarmSlot    bool
	isolatedCheckout bool
}

func NewCloudcat(instanceID string, config map[string]string) (*Cloudcat, error) {
	channelID, err := strconv.ParseInt(strings.TrimSpace(config["channelId"]), 10, 64)
	if err != nil || channelID <= 0 {
		return nil, fmt.Errorf("cloudcat config channelId must be a positive integer")
	}

	var configured []cloudcatProductConfig
	if err := json.Unmarshal([]byte(strings.TrimSpace(config["products"])), &configured); err != nil || len(configured) == 0 {
		return nil, fmt.Errorf("cloudcat config products must be a non-empty JSON array")
	}
	products := make(map[int64]cloudcatProduct, len(configured))
	for _, item := range configured {
		amountMinor, err := payment.AmountToMinorUnit(item.Amount, payment.DefaultPaymentCurrency)
		if err != nil || amountMinor <= 0 {
			return nil, fmt.Errorf("cloudcat product amount %q is invalid", item.Amount)
		}
		goodsKey, normalizedURL, err := parseCloudcatItemURL(item.ItemURL)
		if err != nil {
			return nil, err
		}
		if _, duplicate := products[amountMinor]; duplicate {
			return nil, fmt.Errorf("cloudcat product amount %q is duplicated", item.Amount)
		}
		products[amountMinor] = cloudcatProduct{amountMinor: amountMinor, goodsKey: goodsKey, itemURL: normalizedURL}
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("cloudcat cookie jar: %w", err)
	}
	proxyURL, apiProxyURL, err := cloudcatProxyURLs()
	if err != nil {
		return nil, err
	}
	apiClient := &http.Client{Timeout: cloudcatHTTPTimeout, Jar: jar}
	if apiProxyURL != "" {
		shared, err := httpclient.GetClient(httpclient.Options{ProxyURL: apiProxyURL, Timeout: cloudcatHTTPTimeout})
		if err != nil {
			return nil, fmt.Errorf("cloudcat proxy client: %w", err)
		}
		// Reuse connections without sharing checkout cookies between provider instances.
		client := *shared
		client.Jar = jar
		apiClient = &client
	}
	checkoutClient := &http.Client{
		Transport:     cloudcatCheckoutTransport(proxyURL),
		Timeout:       cloudcatCheckoutTimeout,
		Jar:           jar,
		CheckRedirect: cloudcatCheckoutRedirectPolicy,
	}
	return &Cloudcat{
		instanceID:     instanceID,
		channelID:      channelID,
		products:       products,
		apiBase:        cloudcatAPIBase,
		httpClient:     apiClient,
		checkoutClient: checkoutClient,
		visitorID:      newCloudcatVisitorID(instanceID),
		warmPoolKey:    cloudcatWarmPoolKey{apiProxyURL, proxyURL},
	}, nil
}

func cloudcatProxyURLs() (string, string, error) {
	proxyURL, _, err := proxyurl.Parse(os.Getenv("CLOUDCAT_PROXY_URL"))
	if err != nil {
		return "", "", fmt.Errorf("cloudcat CLOUDCAT_PROXY_URL is invalid")
	}
	apiProxyURL := proxyURL
	if raw := strings.TrimSpace(os.Getenv("CLOUDCAT_API_PROXY_URL")); raw != "" {
		apiProxyURL, _, err = proxyurl.Parse(raw)
		if err != nil {
			return "", "", fmt.Errorf("cloudcat CLOUDCAT_API_PROXY_URL is invalid")
		}
	}
	return proxyURL, apiProxyURL, nil
}

func newCloudcatCheckoutTransport(proxyURL string) *req.Transport {
	checkout := req.C().ImpersonateChrome()
	if proxyURL != "" {
		checkout.SetProxyURL(proxyURL)
	}
	return checkout.Transport
}

func cloudcatCheckoutTransport(proxyURL string) *req.Transport {
	if cached, ok := cloudcatCheckoutTransports.Load(proxyURL); ok {
		return cached.(*req.Transport)
	}
	// Configure once; only connections are shared, never provider cookies.
	// Payment fallbacks must not use this value: req writes HTTP/2 settings into it.
	actual, _ := cloudcatCheckoutTransports.LoadOrStore(proxyURL, newCloudcatCheckoutTransport(proxyURL))
	return actual.(*req.Transport)
}

func cloudcatCheckoutTransportIsShared(transport http.RoundTripper) bool {
	shared := false
	cloudcatCheckoutTransports.Range(func(_, value any) bool {
		if value == transport {
			shared = true
			return false
		}
		return true
	})
	return shared
}

func (c *Cloudcat) Name() string        { return "Cloudcat" }
func (c *Cloudcat) ProviderKey() string { return payment.TypeCloudcat }
func (c *Cloudcat) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeAlipay, payment.TypeWxpay}
}

func (c *Cloudcat) MerchantIdentityMetadata() map[string]string {
	if c == nil || c.channelID <= 0 {
		return nil
	}
	return map[string]string{"channel_id": strconv.FormatInt(c.channelID, 10)}
}

func (c *Cloudcat) CreatePayment(ctx context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, cloudcatCreateTimeout)
	defer cancel()
	paymentType := payment.GetBasePaymentType(strings.TrimSpace(req.PaymentType))
	if paymentType == "" {
		paymentType = payment.TypeAlipay
	}
	if paymentType != payment.TypeAlipay && paymentType != payment.TypeWxpay {
		return nil, fmt.Errorf("cloudcat unsupported payment type %q", req.PaymentType)
	}
	if strings.TrimSpace(req.PayerEmail) == "" {
		return nil, fmt.Errorf("cloudcat payer email is required")
	}
	if _, err := url.ParseRequestURI("mailto:" + strings.TrimSpace(req.PayerEmail)); err != nil || !strings.Contains(req.PayerEmail, "@") {
		return nil, fmt.Errorf("cloudcat payer email is invalid")
	}
	minor, err := payment.AmountToMinorUnit(req.Amount, payment.DefaultPaymentCurrency)
	if err != nil {
		return nil, fmt.Errorf("cloudcat amount: %w", err)
	}
	product, ok := c.products[minor]
	if !ok {
		return nil, fmt.Errorf("cloudcat has no product mapped for amount %s", req.Amount)
	}
	c, release := c.withWarmConnections()
	defer release()

	goods, err := c.loadGoods(ctx, product.goodsKey)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(goods.GoodsType), payment.TypeCard) {
		return nil, fmt.Errorf("cloudcat product %s is not a card product", product.goodsKey)
	}
	if goods.Status != "" && goods.Status != "1" {
		return nil, fmt.Errorf("cloudcat product %s is not active", product.goodsKey)
	}
	if goods.Stock != "" {
		stock, stockErr := strconv.ParseInt(goods.Stock, 10, 64)
		if stockErr == nil && stock <= 0 {
			return nil, fmt.Errorf("cloudcat product %s is out of stock", product.goodsKey)
		}
	}
	goodsMinor, err := payment.AmountToMinorUnit(goods.Price, payment.DefaultPaymentCurrency)
	if err != nil || goodsMinor != product.amountMinor {
		return nil, fmt.Errorf("cloudcat product %s price does not match configured amount", product.goodsKey)
	}
	if strings.TrimSpace(goods.Token) == "" {
		return nil, fmt.Errorf("cloudcat product %s merchant token is missing", product.goodsKey)
	}
	// Both read-only checks must pass before the non-idempotent order request.
	preflight, preflightCtx := errgroup.WithContext(ctx)
	preflight.Go(func() error {
		return c.validatePaymentChannel(preflightCtx, goods.Token, paymentType)
	})
	preflight.Go(func() error {
		quotedAmount, err := c.loadGoodsPrice(preflightCtx, product.goodsKey)
		if err != nil {
			return err
		}
		quotedMinor, err := payment.AmountToMinorUnit(quotedAmount, payment.DefaultPaymentCurrency)
		if err != nil || quotedMinor != minor {
			return fmt.Errorf("cloudcat checkout amount does not match configured amount")
		}
		return nil
	})
	if err := preflight.Wait(); err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < cloudcatHTTPTimeout {
		return nil, fmt.Errorf("cloudcat preflight timed out before order creation")
	}

	var created cloudcatOrderData
	if err := c.post(ctx, "/shopApi/Pay/order", map[string]any{
		"goods_key":        product.goodsKey,
		"quantity":         1,
		"coupon_code":      "",
		"channel_id":       c.channelID,
		"contact":          strings.TrimSpace(req.PayerEmail),
		"query_password":   "",
		"select_cards_ids": []string{},
		"extend":           map[string]any{},
	}, &created); err != nil {
		return nil, fmt.Errorf("cloudcat create order: %w", err)
	}
	created = created.normalized()
	if created.Code != 1 {
		return nil, fmt.Errorf("cloudcat create order failed: %s", firstNonEmptyString(created.Msg, "unexpected response"))
	}
	if strings.TrimSpace(created.TradeNo) == "" {
		return nil, fmt.Errorf("cloudcat create order returned an empty trade number")
	}
	if err := ValidateCloudcatPaymentURL(created.PayURL); err != nil {
		return nil, err
	}
	createdMinor, err := payment.AmountToMinorUnit(created.TotalAmount.String(), payment.DefaultPaymentCurrency)
	if err != nil || createdMinor != minor {
		return nil, fmt.Errorf("cloudcat created order amount does not match configured amount")
	}
	qrCode := ""
	if paymentType == payment.TypeAlipay {
		qrCtx, qrCancel := context.WithTimeout(ctx, cloudcatQRCodeTimeout)
		defer qrCancel()
		var qrErr error
		qrCode, qrErr = c.ResolvePaymentQRCode(qrCtx, created.PayURL)
		if qrErr != nil {
			slog.Warn("resolve cloudcat QR code after order creation failed", "tradeNo", created.TradeNo, "error", qrErr)
		}
	}
	return &payment.CreatePaymentResponse{TradeNo: created.TradeNo, PayURL: created.PayURL, QRCode: qrCode}, nil
}

// ResolvePaymentQRCode follows CatFK's hosted checkout to Alipay and extracts
// the original QR payload. Callers keep PayURL as a fallback when this fails.
func (c *Cloudcat) ResolvePaymentQRCode(ctx context.Context, payURL string) (string, error) {
	if c == nil || c.httpClient == nil || c.checkoutClient == nil {
		return "", fmt.Errorf("cloudcat checkout client is unavailable")
	}
	if err := ValidateCloudcatPaymentURL(payURL); err != nil {
		return "", err
	}
	c, release := c.withWarmConnections()
	defer release()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(payURL), nil)
	if err != nil {
		return "", fmt.Errorf("cloudcat checkout request: %w", err)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("User-Agent", cloudcatCheckoutUA)
	// CatFK is more reliable with standard HTTP; keep Chrome impersonation for Alipay.
	checkoutClient := *c.httpClient
	checkoutClient.CheckRedirect = cloudcatCheckoutRedirectPolicy
	resp, err := checkoutClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("cloudcat checkout: %w", err)
	}
	qrCode, form, err := parseCloudcatCheckoutResponse(resp)
	if err != nil {
		return "", err
	}
	if qrCode != "" {
		return qrCode, nil
	}
	if form == nil {
		return "", fmt.Errorf("cloudcat checkout did not contain a payment form")
	}

	formReq, err := http.NewRequestWithContext(ctx, http.MethodPost, form.action, strings.NewReader(form.values.Encode()))
	if err != nil {
		return "", fmt.Errorf("cloudcat Alipay form request: %w", err)
	}
	formReq.Header.Set("Accept", "text/html,application/xhtml+xml")
	formReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	formReq.Header.Set("User-Agent", cloudcatCheckoutUA)
	resp, err = c.checkoutClient.Do(formReq)
	if err != nil {
		return "", fmt.Errorf("cloudcat Alipay checkout: %w", err)
	}
	qrCode, _, err = parseCloudcatCheckoutResponse(resp)
	if err != nil {
		return "", err
	}
	if qrCode == "" {
		return "", fmt.Errorf("cloudcat Alipay checkout did not contain a QR code")
	}
	return qrCode, nil
}

func parseCloudcatCheckoutResponse(resp *http.Response) (string, *cloudcatPaymentForm, error) {
	if resp == nil || resp.Body == nil {
		return "", nil, fmt.Errorf("cloudcat checkout returned an empty response")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.Request == nil || resp.Request.URL == nil {
		return "", nil, fmt.Errorf("cloudcat checkout returned no final URL")
	}
	if err := ValidateCloudcatPaymentURL(resp.Request.URL.String()); err != nil {
		return "", nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", nil, fmt.Errorf("cloudcat checkout HTTP %d", resp.StatusCode)
	}
	qrCode, form, err := parseCloudcatCheckout(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("parse cloudcat checkout from %s%s: %w", resp.Request.URL.Hostname(), resp.Request.URL.EscapedPath(), err)
	}
	return qrCode, form, nil
}

func (c *Cloudcat) QueryOrder(ctx context.Context, tradeNo string) (*payment.QueryOrderResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" {
		return nil, fmt.Errorf("cloudcat trade number is required")
	}
	var query cloudcatOrderData
	if err := c.post(ctx, "/shopApi/Pay/query", map[string]any{"trade_no": tradeNo}, &query); err != nil {
		return nil, fmt.Errorf("cloudcat query order: %w", err)
	}
	query = query.normalized()
	if query.Code != 1 {
		status := payment.ProviderStatusPending
		msg := strings.ToLower(query.Msg)
		if strings.Contains(msg, "fail") || strings.Contains(msg, "关闭") || strings.Contains(msg, "过期") {
			status = payment.ProviderStatusFailed
		}
		return &payment.QueryOrderResponse{TradeNo: tradeNo, Status: status}, nil
	}

	var info cloudcatOrderData
	if err := c.post(ctx, "/shopApi/Order/info", map[string]any{
		"trade_no": tradeNo, "query_password": "", "dump": 1,
	}, &info); err != nil {
		return nil, fmt.Errorf("cloudcat load paid order: %w", err)
	}
	info = info.normalized()
	if info.Code != 1 {
		return nil, fmt.Errorf("cloudcat paid order details are not available yet")
	}
	for _, returnedTradeNo := range []string{query.TradeNo, info.TradeNo} {
		if returnedTradeNo != "" && returnedTradeNo != tradeNo {
			return nil, fmt.Errorf("cloudcat returned a different order")
		}
	}
	amountText := firstNonEmptyString(info.TotalAmount.String(), query.TotalAmount.String())
	minor, err := payment.AmountToMinorUnit(amountText, payment.DefaultPaymentCurrency)
	if err != nil || minor <= 0 {
		return nil, fmt.Errorf("cloudcat paid order amount is invalid")
	}
	metadata := c.MerchantIdentityMetadata()
	if code, err := singleCloudcatCard(info.Cards); err != nil {
		return nil, err
	} else if code != "" {
		metadata[payment.MetadataCloudcatRechargeCode] = code
	}
	return &payment.QueryOrderResponse{
		TradeNo:  firstNonEmptyString(info.TradeNo, query.TradeNo, tradeNo),
		Status:   payment.ProviderStatusPaid,
		Amount:   payment.MinorUnitToAmount(minor, payment.DefaultPaymentCurrency),
		PaidAt:   firstNonEmptyString(info.PaidAt, query.PaidAt),
		Metadata: metadata,
	}, nil
}

func (c *Cloudcat) VerifyNotification(context.Context, string, map[string]string) (*payment.PaymentNotification, error) {
	return nil, nil
}

func (c *Cloudcat) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	return nil, errors.New("cloudcat refunds are not supported")
}

type cloudcatGoodsData struct {
	GoodsType string          `json:"goods_type"`
	Price     cloudcatScalar  `json:"price"`
	Status    json.RawMessage `json:"status"`
	Stock     json.RawMessage `json:"stock"`
	Inventory json.RawMessage `json:"inventory"`
	Token     string          `json:"token"`
	User      *struct {
		Token string `json:"token"`
	} `json:"user"`
	Goods    *cloudcatGoodsData `json:"goods"`
	Response *cloudcatGoodsData `json:"response"`
}

type cloudcatGoodsResponse struct {
	Code int               `json:"code"`
	Msg  string            `json:"msg"`
	Data cloudcatGoodsData `json:"data"`
	cloudcatGoodsData
}

type cloudcatGoods struct {
	GoodsType string
	Price     string
	Status    string
	Stock     string
	Token     string
}

func (c *Cloudcat) loadGoods(ctx context.Context, goodsKey string) (cloudcatGoods, error) {
	var response cloudcatGoodsResponse
	if err := c.post(ctx, "/shopApi/Shop/goodsInfo", map[string]any{"goods_key": goodsKey}, &response); err != nil {
		return cloudcatGoods{}, fmt.Errorf("cloudcat load product: %w", err)
	}
	if response.Code != 1 {
		return cloudcatGoods{}, fmt.Errorf("cloudcat load product failed: %s", firstNonEmptyString(response.Msg, "unexpected response"))
	}
	candidates := []*cloudcatGoodsData{&response.cloudcatGoodsData, &response.Data, response.Data.Goods, response.Data.Response}
	var out cloudcatGoods
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		out.GoodsType = firstNonEmptyString(out.GoodsType, candidate.GoodsType)
		out.Price = firstNonEmptyString(out.Price, candidate.Price.String())
		out.Status = firstNonEmptyString(out.Status, rawJSONScalar(candidate.Status))
		out.Stock = firstNonEmptyString(out.Stock, rawJSONScalar(candidate.Stock), rawJSONScalar(candidate.Inventory))
		out.Token = firstNonEmptyString(out.Token, candidate.Token)
		if candidate.User != nil {
			out.Token = firstNonEmptyString(out.Token, candidate.User.Token)
		}
	}
	return out, nil
}

type cloudcatChannel struct {
	ID     cloudcatScalar  `json:"id"`
	Code   string          `json:"code"`
	Name   string          `json:"name"`
	Status json.RawMessage `json:"status"`
}

type cloudcatChannelsResponse struct {
	Code     int               `json:"code"`
	Msg      string            `json:"msg"`
	Data     json.RawMessage   `json:"data"`
	Response []cloudcatChannel `json:"response"`
}

func (c *Cloudcat) validatePaymentChannel(ctx context.Context, token, paymentType string) error {
	var response cloudcatChannelsResponse
	if err := c.post(ctx, "/shopApi/Shop/getUserChannel", map[string]any{"token": token}, &response); err != nil {
		return fmt.Errorf("cloudcat load payment channels: %w", err)
	}
	if response.Code != 1 {
		return fmt.Errorf("cloudcat load payment channels failed: %s", firstNonEmptyString(response.Msg, "unexpected response"))
	}
	channels := append([]cloudcatChannel(nil), response.Response...)
	if len(response.Data) > 0 {
		var direct []cloudcatChannel
		if err := json.Unmarshal(response.Data, &direct); err == nil {
			channels = append(channels, direct...)
		} else {
			var nested struct {
				Response []cloudcatChannel `json:"response"`
			}
			if err := json.Unmarshal(response.Data, &nested); err == nil {
				channels = append(channels, nested.Response...)
			}
		}
	}
	for _, channel := range channels {
		id, _ := strconv.ParseInt(channel.ID.String(), 10, 64)
		if id != c.channelID {
			continue
		}
		if status := rawJSONScalar(channel.Status); status != "" && status != "1" {
			return fmt.Errorf("cloudcat channel %d is not active", c.channelID)
		}
		label := strings.ToLower(channel.Code + " " + channel.Name)
		matches := strings.Contains(label, "alipay") || strings.Contains(label, "支付宝")
		if paymentType == payment.TypeWxpay {
			matches = strings.Contains(label, "wxpay") || strings.Contains(label, "wechat") || strings.Contains(label, "weixin") || strings.Contains(label, "微信")
		}
		if !matches {
			return fmt.Errorf("cloudcat channel %d does not support %s", c.channelID, paymentType)
		}
		return nil
	}
	return fmt.Errorf("cloudcat channel %d was not found", c.channelID)
}

type cloudcatPriceData struct {
	TotalAmount cloudcatScalar     `json:"total_amount"`
	Response    *cloudcatPriceData `json:"response"`
}

type cloudcatPriceResponse struct {
	Code int               `json:"code"`
	Msg  string            `json:"msg"`
	Data cloudcatPriceData `json:"data"`
	cloudcatPriceData
}

func (c *Cloudcat) loadGoodsPrice(ctx context.Context, goodsKey string) (string, error) {
	var response cloudcatPriceResponse
	if err := c.post(ctx, "/shopApi/Shop/getGoodsPrice", map[string]any{
		"goods_key": goodsKey, "quantity": 1, "coupon_code": "", "channel_id": c.channelID,
	}, &response); err != nil {
		return "", fmt.Errorf("cloudcat quote product: %w", err)
	}
	if response.Code != 1 {
		return "", fmt.Errorf("cloudcat quote product failed: %s", firstNonEmptyString(response.Msg, "unexpected response"))
	}
	amount := firstNonEmptyString(response.TotalAmount.String(), response.Data.TotalAmount.String())
	if response.Data.Response != nil {
		amount = firstNonEmptyString(amount, response.Data.Response.TotalAmount.String())
	}
	return amount, nil
}

type cloudcatOrderData struct {
	Code        int                `json:"code"`
	Msg         string             `json:"msg"`
	TradeNo     string             `json:"trade_no"`
	PayURL      string             `json:"payurl"`
	PayURLAlt   string             `json:"pay_url"`
	TotalAmount cloudcatScalar     `json:"total_amount"`
	PaidAt      string             `json:"paid_at"`
	Cards       []string           `json:"cards"`
	Data        *cloudcatOrderData `json:"data"`
	Response    *cloudcatOrderData `json:"response"`
}

func (d cloudcatOrderData) normalized() cloudcatOrderData {
	out := d
	for _, nested := range []*cloudcatOrderData{d.Data, d.Response} {
		if nested == nil {
			continue
		}
		n := nested.normalized()
		out.TradeNo = firstNonEmptyString(out.TradeNo, n.TradeNo)
		out.PayURL = firstNonEmptyString(out.PayURL, out.PayURLAlt, n.PayURL, n.PayURLAlt)
		out.TotalAmount = cloudcatScalar(firstNonEmptyString(out.TotalAmount.String(), n.TotalAmount.String()))
		out.PaidAt = firstNonEmptyString(out.PaidAt, n.PaidAt)
		if len(out.Cards) == 0 {
			out.Cards = n.Cards
		}
	}
	out.PayURL = firstNonEmptyString(out.PayURL, out.PayURLAlt)
	return out
}

func (c *Cloudcat) post(ctx context.Context, path string, payload any, dest any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	attempts := 1
	// These POST endpoints only read state. Never replay Pay/order: an EOF may
	// arrive after CatFK created the order, and it has no idempotency contract.
	switch path {
	case "/shopApi/Shop/goodsInfo", "/shopApi/Shop/getUserChannel", "/shopApi/Shop/getGoodsPrice", "/shopApi/Pay/query", "/shopApi/Order/info":
		attempts = 3
	}
	timeout := cloudcatHTTPTimeout
	if attempts > 1 {
		timeout *= 2
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(time.Duration(attempt) * 200 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		var retry bool
		retry, err = c.postOnce(ctx, path, body, dest)
		if err == nil || !retry || ctx.Err() != nil {
			return err
		}
	}
	return err
}

func (c *Cloudcat) postOnce(ctx context.Context, path string, body []byte, dest any) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.apiBase, "/")+path, strings.NewReader(string(body)))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", cloudcatCheckoutUA)
	req.Header.Set("Origin", cloudcatAPIBase)
	req.Header.Set("Referer", cloudcatAPIBase+"/")
	req.Header.Set("Visitorid", c.visitorID)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return true, err
	}
	defer func() { _ = resp.Body.Close() }()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, cloudcatMaxResponseSize+1))
	if err != nil {
		return true, err
	}
	if len(responseBody) > cloudcatMaxResponseSize {
		return false, fmt.Errorf("cloudcat API response is too large")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		retry := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusGatewayTimeout
		return retry, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	decoder := json.NewDecoder(strings.NewReader(string(responseBody)))
	decoder.UseNumber()
	if err := decoder.Decode(dest); err != nil {
		return false, fmt.Errorf("decode response: %w", err)
	}
	return false, nil
}

func parseCloudcatItemURL(raw string) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "catfk.com") || parsed.User != nil {
		return "", "", fmt.Errorf("cloudcat itemUrl must be an https://catfk.com/item/... URL")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", fmt.Errorf("cloudcat itemUrl must not contain a query or fragment")
	}
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(parts) != 2 || parts[0] != "item" {
		return "", "", fmt.Errorf("cloudcat itemUrl must be an https://catfk.com/item/... URL")
	}
	goodsKey, err := url.PathUnescape(parts[1])
	if err != nil || goodsKey == "" || strings.ContainsAny(goodsKey, "/\\") {
		return "", "", fmt.Errorf("cloudcat itemUrl has an invalid product key")
	}
	parsed.RawPath = ""
	parsed.Path = "/item/" + goodsKey
	return goodsKey, parsed.String(), nil
}

// ValidateCloudcatPaymentURL limits hosted checkout redirects to CatFK and
// Alipay-controlled HTTPS hosts.
func ValidateCloudcatPaymentURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" {
		return fmt.Errorf("cloudcat returned an invalid payment URL")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "catfk.com" && !strings.HasSuffix(host, ".catfk.com") && host != "alipay.com" && !strings.HasSuffix(host, ".alipay.com") {
		return fmt.Errorf("cloudcat returned a payment URL on an untrusted host")
	}
	return nil
}

func cloudcatCheckoutRedirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("cloudcat checkout stopped after 10 redirects")
	}
	return ValidateCloudcatPaymentURL(req.URL.String())
}

func extractCloudcatPaymentQRCode(body io.Reader) (string, error) {
	qrCode, _, err := parseCloudcatCheckout(body)
	if err != nil {
		return "", err
	}
	if qrCode == "" {
		return "", fmt.Errorf("cloudcat checkout did not contain an Alipay QR code")
	}
	return qrCode, nil
}

func parseCloudcatCheckout(body io.Reader) (string, *cloudcatPaymentForm, error) {
	limited := &io.LimitedReader{R: body, N: cloudcatMaxResponseSize + 1}
	tokenizer := html.NewTokenizer(limited)
	var form *cloudcatPaymentForm
	inPaymentForm := false
	formFieldCount := 0
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			if limited.N <= 0 {
				return "", nil, fmt.Errorf("cloudcat checkout response is too large")
			}
			if errors.Is(tokenizer.Err(), io.EOF) {
				if form != nil && len(form.values) > 0 {
					return "", form, nil
				}
				return "", nil, fmt.Errorf("cloudcat checkout did not contain an Alipay QR code")
			}
			return "", nil, fmt.Errorf("parse cloudcat checkout: %w", tokenizer.Err())
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if strings.EqualFold(token.Data, "form") && form == nil {
				var action, method string
				for _, attr := range token.Attr {
					switch {
					case strings.EqualFold(attr.Key, "action"):
						action = strings.TrimSpace(attr.Val)
					case strings.EqualFold(attr.Key, "method"):
						method = strings.TrimSpace(attr.Val)
					}
				}
				if strings.EqualFold(method, http.MethodPost) && ValidateCloudcatAlipayFormURL(action) == nil {
					form = &cloudcatPaymentForm{action: action, values: make(url.Values)}
					inPaymentForm = true
				}
				continue
			}
			if !strings.EqualFold(token.Data, "input") {
				continue
			}
			var id, name, value string
			for _, attr := range token.Attr {
				switch {
				case strings.EqualFold(attr.Key, "id"):
					id = attr.Val
				case strings.EqualFold(attr.Key, "name"):
					name = attr.Val
				case strings.EqualFold(attr.Key, "value"):
					value = attr.Val
				}
			}
			if id == "J_qrCode" {
				if err := ValidateCloudcatQRCode(value); err != nil {
					return "", nil, err
				}
				return strings.TrimSpace(value), nil, nil
			}
			if inPaymentForm && name != "" {
				if formFieldCount >= 64 {
					return "", nil, fmt.Errorf("cloudcat checkout returned too many form fields")
				}
				form.values.Add(name, value)
				formFieldCount++
			}
		case html.EndTagToken:
			if strings.EqualFold(tokenizer.Token().Data, "form") {
				inPaymentForm = false
			}
		}
	}
}

func ValidateCloudcatQRCode(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Host != "qr.alipay.com" || parsed.Path == "" || parsed.Path == "/" || parsed.Fragment != "" {
		return fmt.Errorf("cloudcat checkout returned an invalid Alipay QR code")
	}
	return nil
}

func ValidateCloudcatAlipayFormURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" || parsed.Fragment != "" {
		return fmt.Errorf("cloudcat checkout returned an invalid Alipay form URL")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "alipay.com" && !strings.HasSuffix(host, ".alipay.com") {
		return fmt.Errorf("cloudcat checkout returned an untrusted Alipay form URL")
	}
	return nil
}

func singleCloudcatCard(cards []string) (string, error) {
	code := ""
	for _, card := range cards {
		card = strings.TrimSpace(card)
		if card == "" {
			continue
		}
		if code != "" && card != code {
			return "", fmt.Errorf("cloudcat order returned multiple cards for quantity one")
		}
		code = card
	}
	return code, nil
}

func rawJSONScalar(raw json.RawMessage) string {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return ""
	}
	if len(text) >= 2 && text[0] == '"' && text[len(text)-1] == '"' {
		unquoted, err := strconv.Unquote(text)
		if err == nil {
			return strings.TrimSpace(unquoted)
		}
	}
	if text == "true" {
		return "1"
	}
	if text == "false" {
		return "0"
	}
	return text
}

type cloudcatScalar string

func (s *cloudcatScalar) UnmarshalJSON(raw []byte) error {
	value := rawJSONScalar(raw)
	if value == "" && strings.TrimSpace(string(raw)) != "null" {
		return fmt.Errorf("invalid scalar")
	}
	*s = cloudcatScalar(value)
	return nil
}

func (s cloudcatScalar) String() string { return string(s) }

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func newCloudcatVisitorID(instanceID string) string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	sum := sha256.Sum256([]byte("sub2api-cloudcat:" + strings.TrimSpace(instanceID)))
	buf := make([]byte, 9)
	for i := range buf {
		buf[i] = alphabet[int(sum[i])%len(alphabet)]
	}
	return string(buf)
}

var (
	_ payment.Provider                 = (*Cloudcat)(nil)
	_ payment.MerchantIdentityProvider = (*Cloudcat)(nil)
)
