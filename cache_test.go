package httpcache

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func makeRequest(url string, headers http.Header) *http.Request {
	r := httptest.NewRequest("GET", url, nil)
}

func TestHeaderRuleMatcher(t *testing.T) {

	r := HeaderRuleMatcher{
		Header: "Content-Type",
		Value:  []string{"image/jpg", "image/png"},
	}

}
