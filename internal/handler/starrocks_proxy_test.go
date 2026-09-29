package handler

import (
	"strings"
	"testing"
)

func TestRewriteStaticToLocal(t *testing.T) {
	in := `<link href="/static/bootstrap.css"><script src='/static/jquery.js'></script>`
	out, ok := rewriteStaticToLocal(in)
	if !ok {
		t.Fatal("expected rewrite")
	}
	if !strings.Contains(out, `href="/starrocks-webroot/static/bootstrap.css"`) {
		t.Fatalf("href not rewritten: %s", out)
	}
	if !strings.Contains(out, `src='/starrocks-webroot/static/jquery.js'`) {
		t.Fatalf("src not rewritten: %s", out)
	}
}

func TestRewriteRootAbsoluteSkipsLocalStatic(t *testing.T) {
	proxy := "/api/v1/starrocks-be-proxy/10.0.0.1%3A8040"
	in := `<a href="/starrocks-webroot/static/bootstrap.css"><a href="/mem_tracker">`
	out := rewriteRootAbsoluteAttr(in, `href="`, proxy)
	if !strings.Contains(out, `href="/starrocks-webroot/static/bootstrap.css"`) {
		t.Fatalf("local static should stay: %s", out)
	}
	if !strings.Contains(out, `href="`+proxy+`/mem_tracker"`) {
		t.Fatalf("business path should get proxy prefix: %s", out)
	}
}

func TestRewriteSRBodyHTML(t *testing.T) {
	raw := []byte(`<!DOCTYPE html><html><head>
<link href="/bootstrap/css/bootstrap.min.css" rel="stylesheet">
<script src="/jquery-3.5.0.min.js"></script>
<link href="/starrocks.css" rel="stylesheet">
</head><body>
<a href="/static/bootstrap.css">css</a>
<a href="/mem_tracker">mem</a>
</body></html>`)
	out := string(rewriteSRBody(raw, "text/html", "http://10.0.0.1:8040", "/api/v1/starrocks-be-proxy/h"))
	if strings.Contains(out, `href="/static/`) {
		t.Fatalf("still has /static: %s", out)
	}
	if !strings.Contains(out, `/starrocks-webroot/static/bootstrap.css`) {
		t.Fatalf("missing local static: %s", out)
	}
	if !strings.Contains(out, `/starrocks-webroot/be/bootstrap/css/bootstrap.min.css`) {
		t.Fatalf("missing be bootstrap: %s", out)
	}
	if !strings.Contains(out, `/starrocks-webroot/be/jquery-3.5.0.min.js`) {
		t.Fatalf("missing be jquery: %s", out)
	}
	if !strings.Contains(out, `/api/v1/starrocks-be-proxy/h/mem_tracker`) {
		t.Fatalf("mem_tracker not proxied: %s", out)
	}
	if strings.Contains(out, `/api/v1/starrocks-be-proxy/h/bootstrap/`) {
		t.Fatalf("bootstrap should not go through proxy: %s", out)
	}
	if !strings.Contains(out, "auto-dba-sr-embed") {
		t.Fatal("missing embed chrome")
	}
}

func TestCopySRResponseHeadersStripsWWWAuthenticate(t *testing.T) {
	dst := make(map[string][]string)
	src := map[string][]string{
		"WWW-Authenticate":  {"Basic realm=\"StarRocks\""},
		"Proxy-Authenticate": {"Basic"},
		"Content-Type":      {"text/html"},
		"X-Custom":          {"keep"},
	}
	copySRResponseHeaders(dst, src)
	if _, ok := dst["WWW-Authenticate"]; ok {
		t.Fatal("WWW-Authenticate should be stripped")
	}
	if _, ok := dst["Proxy-Authenticate"]; ok {
		t.Fatal("Proxy-Authenticate should be stripped")
	}
	if dst["Content-Type"][0] != "text/html" || dst["X-Custom"][0] != "keep" {
		t.Fatalf("unexpected headers: %#v", dst)
	}
}
