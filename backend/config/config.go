// Package config loads and validates the backend's FOURVISOR_ environment boundary.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	serverAddressKey     = "FOURVISOR_SERVER_ADDRESS"
	healthTimeoutKey     = "FOURVISOR_HEALTH_TIMEOUT"
	memcachedAddressKey  = "FOURVISOR_MEMCACHED_ADDRESS"
	dnsNameKey           = "FOURVISOR_DNS_NAME"
	otlpEndpointKey      = "FOURVISOR_OTLP_ENDPOINT"
	defaultServerAddress = ":65102"
	defaultHealthTimeout = 2 * time.Second
	defaultDNSName       = "a.4cdn.org"
	defaultOTLPEndpoint  = "http://otelcol:65103"
	firstProjectPort     = 65100
	lastProjectPort      = 65199
)

var (
	errMissing         = errors.New("required setting is missing")
	errInvalidAddress  = errors.New("invalid network address")
	errInvalidDuration = errors.New("invalid duration")
	errInvalidDNSName  = errors.New("invalid DNS name")
)

// Config contains only the settings required by the backend health boundary.
type Config struct {
	ServerAddress    string
	HealthTimeout    time.Duration
	MemcachedAddress string
	DNSName          string
	OTLPEndpoint     string
}

// Error preserves a configuration failure while keeping its rendered diagnostic value-free.
type Error struct {
	key   string
	kind  string
	cause error
}

// Error returns a secret-free diagnostic naming only the setting and failure class.
func (err *Error) Error() string {
	return fmt.Sprintf("%s: %s", err.key, err.kind)
}

// Unwrap preserves the original parsing or validation cause.
func (err *Error) Unwrap() error {
	return err.cause
}

// Load reads the backend configuration exclusively from FOURVISOR_ variables.
func Load() (Config, error) {
	serverAddress := valueOrDefault(serverAddressKey, defaultServerAddress)

	err := validateAddress(serverAddressKey, serverAddress, true, true)
	if err != nil {
		return Config{}, err
	}

	healthTimeout, err := duration(healthTimeoutKey, defaultHealthTimeout)
	if err != nil {
		return Config{}, err
	}

	memcachedAddress := os.Getenv(memcachedAddressKey)
	if memcachedAddress == "" {
		return Config{}, configError(memcachedAddressKey, "is required", errMissing)
	}

	err = validateAddress(memcachedAddressKey, memcachedAddress, true, false)
	if err != nil {
		return Config{}, err
	}

	dnsName := valueOrDefault(dnsNameKey, defaultDNSName)
	if !validDNSName(dnsName) {
		return Config{}, configError(dnsNameKey, "must be a valid DNS name", errInvalidDNSName)
	}

	otlpEndpoint := valueOrDefault(otlpEndpointKey, defaultOTLPEndpoint)

	err = validateOTLPEndpoint(otlpEndpoint)
	if err != nil {
		return Config{}, err
	}

	return Config{
		ServerAddress:    serverAddress,
		HealthTimeout:    healthTimeout,
		MemcachedAddress: memcachedAddress,
		DNSName:          dnsName,
		OTLPEndpoint:     otlpEndpoint,
	}, nil
}

func duration(key string, fallback time.Duration) (time.Duration, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, configError(key, "must be a positive duration", err)
	}

	if parsed <= 0 {
		return 0, configError(key, "must be a positive duration", errInvalidDuration)
	}

	return parsed, nil
}

func valueOrDefault(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}

	return fallback
}

func validateAddress(key, address string, projectPort, allowEmptyHost bool) error {
	host, portValue, err := net.SplitHostPort(address)
	if err != nil {
		return configError(key, "must be a host and port", err)
	}

	if host == "" && !allowEmptyHost {
		return configError(key, "must include a host", errInvalidAddress)
	}

	port, err := validPort(portValue)
	if err != nil {
		return configError(key, "must use a valid port", err)
	}

	if projectPort && (port < firstProjectPort || port > lastProjectPort) {
		return configError(key, "must use a project port", errInvalidAddress)
	}

	return nil
}

func validPort(value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parsing port: %w", err)
	}

	if port < 1 || port > 65535 {
		return 0, errInvalidAddress
	}

	return port, nil
}

func validateOTLPEndpoint(endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return configError(otlpEndpointKey, "must be an HTTP or HTTPS URL", err)
	}

	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil {
		return configError(otlpEndpointKey, "must be an HTTP or HTTPS URL without credentials", errInvalidAddress)
	}

	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return configError(otlpEndpointKey, "must not include a query or fragment", errInvalidAddress)
	}

	return validateOTLPPort(parsed)
}

func validateOTLPPort(endpoint *url.URL) error {
	if strings.HasSuffix(endpoint.Host, ":") {
		return configError(otlpEndpointKey, "must use a valid port", errInvalidAddress)
	}

	port := endpoint.Port()
	if port == "" {
		return nil
	}

	_, err := validPort(port)
	if err != nil {
		return configError(otlpEndpointKey, "must use a valid port", err)
	}

	return nil
}

func validDNSName(name string) bool {
	if len(name) == 0 || len(name) > 253 || strings.HasSuffix(name, ".") {
		return false
	}

	for label := range strings.SplitSeq(name, ".") {
		if !validDNSLabel(label) {
			return false
		}
	}

	return true
}

func validDNSLabel(label string) bool {
	if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}

	for _, character := range label {
		if !validDNSCharacter(character) {
			return false
		}
	}

	return true
}

func validDNSCharacter(character rune) bool {
	return character == '-' || character >= '0' && character <= '9' ||
		character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z'
}

func configError(key, kind string, cause error) error {
	return &Error{key: key, kind: kind, cause: cause}
}
