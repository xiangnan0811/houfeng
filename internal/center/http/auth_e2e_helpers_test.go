package http_test

import (
	"net/http"
	"net/http/cookiejar"
)

func cookieJarHelper() (http.CookieJar, error) {
	return cookiejar.New(nil)
}
