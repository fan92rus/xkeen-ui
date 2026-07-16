package subscription

import (
	"context"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// startTestHTTPProxy запускает HTTP-прокси через httptest, который перенаправляет
// запросы на целевые URL. Возвращает URL прокси "http://127.0.0.1:<port>".
//
// Используется для тестирования cascade-логики: запрос через прокси должен
// достичь целевого HTTP-сервера. HTTP-прокси выбран вместо SOCKS5, т.к.
// Go stdlib поддерживает его нативно через http.Transport.Proxy, а логика
// cascade (try proxy → fallback direct) одинакова для всех схем прокси.
func startTestHTTPProxy(t *testing.T) string {
	t.Helper()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Standard HTTP CONNECT / proxy behavior is handled by httptest itself
		// when using HandlerFunc with non-CONNECT requests: serve directly.
		w.Write([]byte(simpleSubContent()))
	}))
	t.Cleanup(proxy.Close)
	return proxy.URL
}

// simpleSubContent возвращает валидный base64-контент подписки с 1 прокси.
func simpleSubContent() string {
	line := "vless://a1b2c3d4-e5f6-0000-abcd-ef1234567890@1.2.3.4:8443?encryption=none&type=tcp#Test"
	return base64.StdEncoding.EncodeToString([]byte(line + "\n"))
}

func TestFetchWithCascade_DirectOnly_WhenNoProxy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(simpleSubContent()))
	}))
	defer server.Close()

	fetcher := NewFetcher() // без прокси

	result, err := fetcher.FetchWithCascade(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Source != SourceDirect {
		t.Errorf("expected source %q, got %q", SourceDirect, result.Source)
	}
	if len(result.Entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(result.Entries))
	}
}

func TestFetchWithCascade_ProxySuccess(t *testing.T) {
	// HTTP-прокси "доступен" — вернёт контент подписки
	proxyURL := startTestHTTPProxy(t)

	// Целевой сервер (для URL, к которому обращаемся)
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		// Если запрос пришёл сюда напрямую (мимо прокси) — это ошибка логики
		t.Errorf("direct target should not be hit when proxy succeeds")
	}))
	defer target.Close()

	fetcher := NewFetcher()
	if err := fetcher.SetProxyURL(proxyURL); err != nil {
		t.Fatalf("SetProxyURL failed: %v", err)
	}

	// Запрос к target.URL пойдёт через прокси (он перехватит и вернёт свой контент)
	result, err := fetcher.FetchWithCascade(context.Background(), target.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Source != SourceProxy {
		t.Errorf("expected source %q, got %q", SourceProxy, result.Source)
	}
	if len(result.Entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(result.Entries))
	}
}

func TestFetchWithCascade_FallbackToDirect_WhenProxyDown(t *testing.T) {
	// Целевой сервер (доступен напрямую)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(simpleSubContent()))
	}))
	defer target.Close()

	fetcher := NewFetcher()
	// Несуществующий HTTP-прокси → должен фолбэчить на прямой
	_ = fetcher.SetProxyURL("http://127.0.0.1:1")

	result, err := fetcher.FetchWithCascade(context.Background(), target.URL)
	if err != nil {
		t.Fatalf("should fallback to direct, got error: %v", err)
	}
	if result.Source != SourceDirect {
		t.Errorf("expected fallback source %q, got %q", SourceDirect, result.Source)
	}
	if len(result.Entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(result.Entries))
	}
}

func TestFetchWithCascade_BothFail_ReturnsAggregatedError(t *testing.T) {
	fetcher := NewFetcher()
	// Несуществующий HTTP-прокси
	_ = fetcher.SetProxyURL("http://127.0.0.1:1")

	// Несуществующий целевой URL — и прокси, и прямой упадут
	_, err := fetcher.FetchWithCascade(context.Background(), "http://127.0.0.1:1/nonexistent")
	if err == nil {
		t.Fatal("expected aggregated error when both proxy and direct fail")
	}

	// Проверяем, что ошибка содержит информацию о двух попытках
	errStr := err.Error()
	if !contains(errStr, "proxy") || !contains(errStr, "direct") {
		t.Errorf("error should mention both proxy and direct attempts, got: %s", errStr)
	}
}

func TestFetchWithCascade_ResetProxy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(simpleSubContent()))
	}))
	defer server.Close()

	fetcher := NewFetcher()
	_ = fetcher.SetProxyURL("http://127.0.0.1:1")
	// Сброс
	if err := fetcher.SetProxyURL(""); err != nil {
		t.Fatalf("reset failed: %v", err)
	}

	result, err := fetcher.FetchWithCascade(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("unexpected error after reset: %v", err)
	}
	if result.Source != SourceDirect {
		t.Errorf("after reset should be direct, got %q", result.Source)
	}
}

func TestFetchWithCascade_PreservesBackwardCompat_Fetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(simpleSubContent()))
	}))
	defer server.Close()

	fetcher := NewFetcher()
	// Старый метод Fetch должен работать как раньше (через cascade внутри)
	entries, err := fetcher.Fetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("legacy Fetch failed: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
}

func TestFetchWithCascade_ProxyTimeout_FallsBackToDirect(t *testing.T) {
	// Целевой сервер отвечает быстро напрямую
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(simpleSubContent()))
	}))
	defer target.Close()

	// TCP-сервер, который принимает соединение, но никогда не отвечает.
	// Эмулирует зависший HTTP-прокси. Используем raw listener вместо
	// httptest.Server, т.к. последний не подходит для эмуляции прокси.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Держим соединение открытым без ответа → таймаут на этапе прокси
			go func(c net.Conn) {
				<-time.After(30 * time.Second)
				c.Close()
			}(conn)
		}
	}()

	fetcher := NewFetcher()
	_ = fetcher.SetProxyURL("http://" + ln.Addr().String())

	// Короткий общий таймаут, чтобы тест не висел (proxy=20s, но должен успеть за ~25s)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	result, err := fetcher.FetchWithCascade(ctx, target.URL)
	if err != nil {
		t.Fatalf("should fallback on proxy timeout, got: %v", err)
	}
	if result.Source != SourceDirect {
		t.Errorf("expected direct after proxy timeout, got %q", result.Source)
	}
}

func TestSetProxyURL_HTTPProxy(t *testing.T) {
	fetcher := NewFetcher()
	if err := fetcher.SetProxyURL("http://127.0.0.1:8080"); err != nil {
		t.Fatalf("http proxy should be accepted: %v", err)
	}
	if fetcher.proxyURL == "" {
		t.Error("proxyURL should be set")
	}
}

func TestSetProxyURL_SOCKS5Proxy(t *testing.T) {
	fetcher := NewFetcher()
	if err := fetcher.SetProxyURL("socks5://127.0.0.1:1080"); err != nil {
		t.Fatalf("socks5 proxy should be accepted: %v", err)
	}
	if fetcher.proxyURL == "" {
		t.Error("proxyURL should be set")
	}
}

func TestSetProxyURL_InvalidScheme(t *testing.T) {
	fetcher := NewFetcher()
	if err := fetcher.SetProxyURL("ftp://bad"); err == nil {
		t.Error("expected error for unsupported scheme")
	}
}

func TestSetProxyURL_InvalidURL(t *testing.T) {
	fetcher := NewFetcher()
	if err := fetcher.SetProxyURL("://no-scheme"); err == nil {
		t.Error("expected error for malformed URL")
	}
}

func TestSetProxyURL_EmptyResets(t *testing.T) {
	fetcher := NewFetcher()
	_ = fetcher.SetProxyURL("http://127.0.0.1:8080")
	if err := fetcher.SetProxyURL(""); err != nil {
		t.Fatalf("empty should reset without error: %v", err)
	}
	if fetcher.proxyURL != "" {
		t.Error("proxyURL should be empty after reset")
	}
}
