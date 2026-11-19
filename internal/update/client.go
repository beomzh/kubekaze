package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Where releases live by default.
const (
	Repo           = "beomzh/kubekaze"
	githubAPI      = "https://api.github.com"
	githubDownload = "https://github.com/" + Repo + "/releases/download"
)

// Client fetches release information and files. With an empty BaseURL it talks
// to GitHub; with a BaseURL it uses a mirror laid out like this:
//
//	<BaseURL>/latest                                  one line: the newest tag, e.g. v0.1.1
//	<BaseURL>/<tag>/checksums.txt
//	<BaseURL>/<tag>/kubekaze_<version>_<os>_<arch>.tar.gz   (.zip on Windows)
type Client struct {
	HTTP      *http.Client
	BaseURL   string
	APIURL    string // overridable for tests; default is the GitHub API
	UserAgent string
}

// NewClient builds a Client. Proxy settings from the environment
// (HTTPS_PROXY, NO_PROXY) are honoured by the default transport.
func NewClient(baseURL, userAgent string) *Client {
	c := &Client{BaseURL: strings.TrimRight(baseURL, "/"), UserAgent: userAgent, APIURL: githubAPI}
	c.HTTP = &http.Client{
		Timeout: 10 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			return checkURL(req.URL)
		},
	}
	return c
}

// checkURL allows https, and plain http only to the local machine (tests,
// a mirror on localhost), so a download can never be silently downgraded.
func checkURL(u *url.URL) error {
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
	}
	return fmt.Errorf("refusing to use %q: only https is allowed", u.String())
}

func (c *Client) get(ctx context.Context, rawURL string) (*http.Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if err := checkURL(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: %s", rawURL, resp.Status)
	}
	return resp, nil
}

// Latest returns the newest release tag.
func (c *Client) Latest(ctx context.Context) (string, error) {
	if c.BaseURL != "" {
		resp, err := c.get(ctx, c.BaseURL+"/latest")
		if err != nil {
			return "", fmt.Errorf("asking the mirror for the latest version: %w (the mirror needs a 'latest' file, or pass --version)", err)
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(io.LimitReader(resp.Body, 256))
		if err != nil {
			return "", err
		}
		tag := strings.TrimSpace(string(b))
		if !ValidTag(tag) {
			return "", fmt.Errorf("the mirror's latest file contains %q, not a version tag", tag)
		}
		return tag, nil
	}
	resp, err := c.get(ctx, c.APIURL+"/repos/"+Repo+"/releases/latest")
	if err != nil {
		return "", fmt.Errorf("asking GitHub for the latest release: %w", err)
	}
	defer resp.Body.Close()
	var rel struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return "", fmt.Errorf("reading the GitHub answer: %w", err)
	}
	if !ValidTag(rel.Tag) {
		return "", fmt.Errorf("GitHub returned an unexpected tag %q", rel.Tag)
	}
	return rel.Tag, nil
}

// Fetch downloads <tag>/<file> to dest, reading at most max bytes.
func (c *Client) Fetch(ctx context.Context, tag, file, dest string, max int64) error {
	if !ValidTag(tag) {
		return fmt.Errorf("invalid version tag %q", tag)
	}
	base := c.BaseURL
	if base == "" {
		base = githubDownload
	}
	resp, err := c.get(ctx, base+"/"+tag+"/"+file)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(resp.Body, max+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n > max {
		return fmt.Errorf("%s is larger than the allowed %d bytes", file, max)
	}
	return nil
}
