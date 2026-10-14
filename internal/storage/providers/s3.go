package providers

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/pentaqilo/netsidian/internal/ports"
	"github.com/pentaqilo/netsidian/internal/storage/local"
	"github.com/pentaqilo/netsidian/internal/storage/s3store"
)

type IDTokenSource func(context.Context, string) (string, error)
type tokenRetriever struct {
	source IDTokenSource
	owner  string
}

func (t tokenRetriever) GetIdentityToken() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	v, e := t.source(ctx, t.owner)
	return []byte(v), e
}

// S3Factory accepts only SigV4 credentials or a trusted web-identity STS
// exchange. It never borrows the service's AWS credentials for a user's bucket.
func S3Factory(localContent *local.Content, endpoints, trustedSTS []string, source IDTokenSource, readLimit int64) Factory {
	return func(ctx context.Context, v ports.StorageConfig) (ports.ContentStore, error) {
		if v.Region == "" {
			return nil, fmt.Errorf("S3 region required")
		}
		client, err := endpointClient(v.Endpoint, endpoints)
		if err != nil {
			return nil, err
		}
		var cp aws.CredentialsProvider
		switch v.Auth {
		case "keys", "session":
			if v.AccessKey == "" || v.SecretKey == "" {
				return nil, fmt.Errorf("access key and secret key required")
			}
			if v.Auth == "session" && v.SessionToken == "" {
				return nil, fmt.Errorf("session token required")
			}
			if v.Auth == "keys" && v.SessionToken != "" {
				return nil, fmt.Errorf("choose temporary session credentials when providing a session token")
			}
			cp = credentials.NewStaticCredentialsProvider(v.AccessKey, v.SecretKey, v.SessionToken)
		case "oidc":
			if source == nil || v.OwnerID == "" {
				return nil, fmt.Errorf("sign in with the configured OIDC provider first")
			}
			if v.STSEndpoint == "" {
				return nil, fmt.Errorf("trusted STS endpoint required")
			}
			sc, err := endpointClient(v.STSEndpoint, trustedSTS)
			if err != nil {
				return nil, fmt.Errorf("STS endpoint is not trusted")
			}
			cfg := aws.Config{Region: v.Region, HTTPClient: sc, Credentials: aws.AnonymousCredentials{}}
			s := sts.NewFromConfig(cfg, func(o *sts.Options) { o.BaseEndpoint = aws.String(v.STSEndpoint) })
			cp = aws.NewCredentialsCache(stscreds.NewWebIdentityRoleProvider(s, v.RoleARN, tokenRetriever{source, v.OwnerID}, func(o *stscreds.WebIdentityRoleOptions) { o.RoleSessionName = "netsidian" }))
		default:
			return nil, fmt.Errorf("choose access keys, temporary session credentials, or OIDC federation")
		}
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		store, err := s3store.NewWithCredentials(ctx, v.Bucket, v.Region, v.Endpoint, v.Prefix, v.PathStyle, localContent, cp, client)
		if err == nil {
			store.SetReadLimit(readLimit)
		}
		return store, err
	}
}

func endpointClient(endpoint string, allowed []string) (*http.Client, error) {
	privateHosts := map[string]bool{}
	if endpoint != "" {
		u, e := url.Parse(endpoint)
		if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return nil, fmt.Errorf("endpoint must be an origin")
		}
		match := false
		for _, a := range allowed {
			if strings.TrimRight(a, "/") == strings.TrimRight(endpoint, "/") {
				match = true
			}
		}
		if !match {
			return nil, fmt.Errorf("endpoint is not in the operator's allowlist")
		}
		if u.Scheme != "https" && u.Scheme != "http" {
			return nil, fmt.Errorf("endpoint must use HTTP or HTTPS")
		}
		privateHosts[u.Hostname()] = true
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, e
		}
		ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
		if e != nil {
			return nil, e
		}
		for _, a := range ips {
			// Cloud metadata/link-local is forbidden even on explicitly trusted hosts.
			if a.IP.IsLinkLocalUnicast() || a.IP.IsLinkLocalMulticast() || a.IP.IsUnspecified() {
				continue
			}
			if !privateHosts[host] && (a.IP.IsPrivate() || a.IP.IsLoopback() || !a.IP.IsGlobalUnicast()) {
				continue
			}
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(a.IP.String(), port))
		}
		return nil, fmt.Errorf("storage endpoint resolves to a prohibited address")
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("storage redirects are prohibited") }}, nil
}
