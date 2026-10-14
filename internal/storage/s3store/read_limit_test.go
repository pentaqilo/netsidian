package s3store

import (
	"errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/pentaqilo/netsidian/internal/domain"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemoteObjectsCannotBypassReadLimits(t *testing.T) {
	for _, chunked := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if chunked {
				w.(http.Flusher).Flush()
			}
			w.Write([]byte("01234567890"))
		}))
		client := s3.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: aws.AnonymousCredentials{}}, func(o *s3.Options) { o.BaseEndpoint = aws.String(server.URL); o.UsePathStyle = true })
		c := &Content{client: client, bucket: "bucket", readLimit: 8, localIDs: map[string]bool{}}
		_, e := c.Read("spc_limit", "large.md")
		server.Close()
		var de *domain.Error
		if !errors.As(e, &de) || de.Code != domain.ErrCodeTooLarge {
			t.Fatal("oversized remote object accepted", chunked, e)
		}
	}
}
