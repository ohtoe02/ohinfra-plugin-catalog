package catalogtool

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

func Materialize(
	ctx context.Context,
	entries []Entry,
	outputDirectory string,
	httpClient *http.Client,
	allowedHosts []string,
) error {
	for _, entry := range entries {
		if err := ValidateEntry(entry); err != nil {
			return err
		}
		for _, asset := range entry.Version.Assets {
			directory := filepath.Join(outputDirectory, entry.Name, entry.Version.Version)
			if err := os.MkdirAll(directory, 0o700); err != nil {
				return err
			}
			if err := materializeAsset(
				ctx,
				httpClient,
				allowedHosts,
				asset,
				filepath.Join(directory, "plugin"),
			); err != nil {
				return fmt.Errorf("%s %s: %w", entry.Name, entry.Version.Version, err)
			}
			manifest, err := json.MarshalIndent(entry.Version.Manifest, "", "  ")
			if err != nil {
				return err
			}
			if err := os.WriteFile(
				filepath.Join(directory, "manifest.json"),
				append(manifest, '\n'),
				0o600,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func CompareManifest(expected Manifest, actual []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(actual))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("plugin emitted multiple JSON documents")
		}
		return err
	}
	if !reflect.DeepEqual(expected, manifest) {
		return errors.New("plugin manifest does not match catalog metadata")
	}
	return nil
}

func materializeAsset(
	ctx context.Context,
	httpClient *http.Client,
	allowedHosts []string,
	asset Asset,
	destination string,
) error {
	if err := validateAssetURL(asset.URL, allowedHosts); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return err
	}
	client := secureHTTPClient(httpClient, allowedHosts)
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	if response.ContentLength > asset.SizeBytes {
		return errors.New("asset exceeds declared size")
	}
	temp, err := os.CreateTemp(filepath.Dir(destination), ".catalog-asset-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer func() { _ = os.Remove(name) }()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(response.Body, asset.SizeBytes+1))
	if err != nil {
		_ = temp.Close()
		return err
	}
	if written != asset.SizeBytes {
		_ = temp.Close()
		return fmt.Errorf("asset size mismatch: expected %d, got %d", asset.SizeBytes, written)
	}
	if got := hex.EncodeToString(hash.Sum(nil)); !strings.EqualFold(got, asset.SHA256) {
		_ = temp.Close()
		return errors.New("asset SHA-256 mismatch")
	}
	if err := temp.Chmod(0o755); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, destination)
}

func secureHTTPClient(provided *http.Client, allowedHosts []string) *http.Client {
	var client http.Client
	if provided != nil {
		client = *provided
	} else {
		client.Transport = &http.Transport{
			Proxy: nil,
			TLSClientConfig: &tls.Config{ // #nosec G402 -- TLS 1.2 is the compatibility floor.
				MinVersion: tls.VersionTLS12,
			},
		}
	}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("redirect limit exceeded")
		}
		return validateAssetURL(request.URL.String(), allowedHosts)
	}
	return &client
}

func validateAssetURL(value string, allowedHosts []string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return errors.New("asset URL must use credential-free HTTPS")
	}
	host := strings.ToLower(parsed.Hostname())
	for _, allowed := range allowedHosts {
		if host == strings.ToLower(allowed) {
			return nil
		}
	}
	return fmt.Errorf("asset host %q is not allowed", host)
}
