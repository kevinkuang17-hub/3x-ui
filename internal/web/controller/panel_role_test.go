package controller

import (
	"net/http"
	"testing"
)

func TestOperatorAllowed(t *testing.T) {
	cases := []struct {
		method, path string
		want         bool
	}{
		{http.MethodGet, "/clients/list", true},
		{http.MethodPost, "/clients/add", true},
		{http.MethodPost, "/clients/groups/bulkAdd", true},
		{http.MethodPost, "/plans/apply", true},
		{http.MethodGet, "/server/status", true},
		{http.MethodPost, "/setting/defaultSettings", true},
		{http.MethodGet, "/server/getDb", false},
		{http.MethodGet, "/server/getConfigJson", false},
		{http.MethodGet, "/xray/history/get/:id", false},
		{http.MethodGet, "/users/list", false},
		{http.MethodPost, "/setting/update", false},
		{http.MethodPost, "/setting/all", false},
		{http.MethodPost, "/xray/update", false},
		{http.MethodPost, "/server/restartXrayService", false},
		{http.MethodPost, "/nodes/add", false},
		{http.MethodPost, "/users/add", false},
		{http.MethodPost, "/inbounds/del/:id", false},
		{http.MethodPost, "/crm/sso", false},
	}
	for _, c := range cases {
		if got := operatorAllowed(c.method, c.path); got != c.want {
			t.Errorf("%s %s: got %v want %v", c.method, c.path, got, c.want)
		}
	}
}
