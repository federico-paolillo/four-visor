package config

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	clearEnvironment(t)
	setRequiredEnvironment(t)

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := Config{
		ServerAddress:    defaultServerAddress,
		HealthTimeout:    defaultHealthTimeout,
		MemcachedAddress: "memcached:65100",
		DNSName:          defaultDNSName,
		OTLPEndpoint:     defaultOTLPEndpoint,
		Acquisition: Acquisition{
			RateInterval:   defaultRateInterval,
			MaxConcurrency: defaultConcurrency,
			RequestTimeout: defaultRequestTimeout,
			MaxRetries:     defaultMaxRetries,
			RetryBackoff:   defaultRetryBackoff,
			Deadline:       defaultDeadline,
			UserAgent:      "4Visor/0123456789abcdef0123456789abcdef01234567",
		},
		Synchronization: Synchronization{
			Interval:                defaultSyncInterval,
			FailedResourceTolerance: defaultTolerance,
		},
	}
	if got != want {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
}

func TestLoadOverrides(t *testing.T) {
	clearEnvironment(t)
	setRequiredEnvironment(t)
	t.Setenv(serverAddressKey, "127.0.0.1:65120")
	t.Setenv(healthTimeoutKey, "750ms")
	t.Setenv(memcachedAddressKey, "cache.internal:65121")
	t.Setenv(dnsNameKey, "boards.4chan.org")
	t.Setenv(otlpEndpointKey, "https://collector.example:4317")
	t.Setenv(rateIntervalKey, "2s")
	t.Setenv(maxConcurrencyKey, "4")
	t.Setenv(requestTimeoutKey, "3s")
	t.Setenv(maxRetriesKey, "1")
	t.Setenv(retryBackoffKey, "500ms")
	t.Setenv(deadlineKey, "30m")
	t.Setenv(syncIntervalKey, "1s")
	t.Setenv(failedToleranceKey, "4")

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.ServerAddress != "127.0.0.1:65120" || got.HealthTimeout != 750*time.Millisecond ||
		got.MemcachedAddress != "cache.internal:65121" || got.DNSName != "boards.4chan.org" ||
		got.OTLPEndpoint != "https://collector.example:4317" {
		t.Fatalf("Load() returned unexpected overrides: %#v", got)
	}
	if got.Acquisition.RateInterval != 2*time.Second || got.Acquisition.MaxConcurrency != 4 ||
		got.Acquisition.RequestTimeout != 3*time.Second || got.Acquisition.MaxRetries != 1 ||
		got.Acquisition.RetryBackoff != 500*time.Millisecond ||
		got.Acquisition.Deadline != 30*time.Minute ||
		got.Acquisition.UserAgent != "4Visor/0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("Load() returned unexpected acquisition overrides: %#v", got.Acquisition)
	}
	if got.Synchronization.Interval != time.Second || got.Synchronization.FailedResourceTolerance != 4 {
		t.Fatalf("Load() returned unexpected synchronization overrides: %#v", got.Synchronization)
	}
}

func TestLoadValidation(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "missing Memcached", key: memcachedAddressKey},
		{name: "invalid duration", key: healthTimeoutKey, value: "later"},
		{name: "zero duration", key: healthTimeoutKey, value: "0s"},
		{name: "server outside project range", key: serverAddressKey, value: ":65099"},
		{name: "Memcached outside project range", key: memcachedAddressKey, value: "cache:65099"},
		{name: "invalid DNS name", key: dnsNameKey, value: "-4chan.example"},
		{name: "invalid OTLP endpoint", key: otlpEndpointKey, value: "collector:4317"},
		{name: "OTLP query", key: otlpEndpointKey, value: "https://collector.example:4317?token=secret"},
		{name: "OTLP fragment", key: otlpEndpointKey, value: "https://collector.example:4317#secret"},
		{name: "OTLP zero port", key: otlpEndpointKey, value: "https://collector.example:0"},
		{name: "OTLP port above range", key: otlpEndpointKey, value: "https://collector.example:65536"},
		{name: "rate below official limit", key: rateIntervalKey, value: "999ms"},
		{name: "zero concurrency", key: maxConcurrencyKey, value: "0"},
		{name: "concurrency above maximum", key: maxConcurrencyKey, value: "11"},
		{name: "invalid request timeout", key: requestTimeoutKey, value: "later"},
		{name: "negative retries", key: maxRetriesKey, value: "-1"},
		{name: "retries above maximum", key: maxRetriesKey, value: "3"},
		{name: "zero retry backoff", key: retryBackoffKey, value: "0s"},
		{name: "invalid acquisition deadline", key: deadlineKey, value: "later"},
		{name: "zero acquisition deadline", key: deadlineKey, value: "0s"},
		{name: "invalid synchronization interval", key: syncIntervalKey, value: "later"},
		{name: "zero synchronization interval", key: syncIntervalKey, value: "0s"},
		{name: "subsecond synchronization interval", key: syncIntervalKey, value: "999ms"},
		{name: "fractional-second synchronization interval", key: syncIntervalKey, value: "1500ms"},
		{name: "unrepresentable synchronization interval", key: syncIntervalKey, value: "200000h"},
		{name: "invalid failed resource tolerance", key: failedToleranceKey, value: "many"},
		{name: "negative failed resource tolerance", key: failedToleranceKey, value: "-1"},
		{name: "missing commit hash", key: commitHashKey},
		{name: "short commit hash", key: commitHashKey, value: "0123456"},
		{name: "uppercase commit hash", key: commitHashKey, value: "0123456789ABCDEF0123456789ABCDEF01234567"},
		{name: "non-hex commit hash", key: commitHashKey, value: "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearEnvironment(t)
			setRequiredEnvironment(t)
			if test.key == memcachedAddressKey && test.value == "" {
				if err := os.Unsetenv(memcachedAddressKey); err != nil {
					t.Fatalf("os.Unsetenv(%q): %v", memcachedAddressKey, err)
				}
			}
			if test.key == commitHashKey && test.value == "" {
				if err := os.Unsetenv(commitHashKey); err != nil {
					t.Fatalf("os.Unsetenv(%q): %v", commitHashKey, err)
				}
			}
			if test.value != "" {
				t.Setenv(test.key, test.value)
			}

			_, err := Load()
			if err == nil {
				t.Fatal("Load() error = nil, want validation error")
			}
			var configErr *Error
			if !errors.As(err, &configErr) {
				t.Fatalf("Load() error type = %T, want *Error", err)
			}
		})
	}
}

func TestOTLPEndpointDiagnosticRedactsValueAndPreservesCause(t *testing.T) {
	tests := []string{
		"https://collector.example:4317?token=credential-do-not-log",
		"https://collector.example:4317#credential-do-not-log",
		"https://collector.example:65536",
	}

	for _, endpoint := range tests {
		t.Run(endpoint, func(t *testing.T) {
			clearEnvironment(t)
			setRequiredEnvironment(t)
			t.Setenv(otlpEndpointKey, endpoint)

			_, err := Load()
			if !errors.Is(err, errInvalidAddress) {
				t.Fatalf("Load() error = %v, want preserved invalid-address cause", err)
			}
			var output bytes.Buffer
			slog.New(slog.NewJSONHandler(&output, nil)).Error("startup failed", slog.Any("error", err))
			if strings.Contains(err.Error(), endpoint) || strings.Contains(output.String(), endpoint) ||
				strings.Contains(output.String(), "credential-do-not-log") {
				t.Fatalf("diagnostic disclosed OTLP endpoint: %s", output.String())
			}
		})
	}
}

func TestLoadDiagnosticRedactsValueAndPreservesCause(t *testing.T) {
	clearEnvironment(t)
	setRequiredEnvironment(t)
	const secret = "credential-do-not-log"
	t.Setenv(healthTimeoutKey, secret)

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want parse error")
	}
	var configErr *Error
	if !errors.As(err, &configErr) || errors.Unwrap(configErr) == nil {
		t.Fatalf("Load() error = %v, want preserved cause", err)
	}

	var output bytes.Buffer
	slog.New(slog.NewJSONHandler(&output, nil)).Error("startup failed", slog.Any("error", err))
	if strings.Contains(err.Error(), secret) || strings.Contains(output.String(), secret) {
		t.Fatalf("diagnostic disclosed configured value: %s", output.String())
	}
	if !strings.Contains(output.String(), healthTimeoutKey) {
		t.Fatalf("diagnostic omitted setting name: %s", output.String())
	}
}

func clearEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		serverAddressKey,
		healthTimeoutKey,
		memcachedAddressKey,
		dnsNameKey,
		otlpEndpointKey,
		rateIntervalKey,
		maxConcurrencyKey,
		requestTimeoutKey,
		maxRetriesKey,
		retryBackoffKey,
		deadlineKey,
		syncIntervalKey,
		failedToleranceKey,
		commitHashKey,
	} {
		value, exists := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("os.Unsetenv(%q): %v", key, err)
		}
		t.Cleanup(func() {
			if exists {
				_ = os.Setenv(key, value)
			} else {
				_ = os.Unsetenv(key)
			}
		})
	}
}

func setRequiredEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv(memcachedAddressKey, "memcached:65100")
	t.Setenv(commitHashKey, "0123456789abcdef0123456789abcdef01234567")
}
