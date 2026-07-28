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
	t.Setenv(memcachedAddressKey, "memcached:65100")

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
	}
	if got != want {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
}

func TestLoadOverrides(t *testing.T) {
	clearEnvironment(t)
	t.Setenv(serverAddressKey, "127.0.0.1:65120")
	t.Setenv(healthTimeoutKey, "750ms")
	t.Setenv(memcachedAddressKey, "cache.internal:65121")
	t.Setenv(dnsNameKey, "boards.4chan.org")
	t.Setenv(otlpEndpointKey, "https://collector.example:4317")

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.ServerAddress != "127.0.0.1:65120" || got.HealthTimeout != 750*time.Millisecond ||
		got.MemcachedAddress != "cache.internal:65121" || got.DNSName != "boards.4chan.org" ||
		got.OTLPEndpoint != "https://collector.example:4317" {
		t.Fatalf("Load() returned unexpected overrides: %#v", got)
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
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearEnvironment(t)
			if test.key != memcachedAddressKey || test.value != "" {
				t.Setenv(memcachedAddressKey, "memcached:65100")
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
			t.Setenv(memcachedAddressKey, "memcached:65100")
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
	t.Setenv(memcachedAddressKey, "memcached:65100")
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
