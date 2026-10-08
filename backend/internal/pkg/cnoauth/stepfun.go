package cnoauth

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/brandidentity"
)

// Step-Code accepts any loopback port. The panel uses manual callback import,
// so it does not bind a listener on the server or the administrator's computer.
const StepFunRedirect = "http://127.0.0.1:53683/callback"

func startStepFun(f *Flow) (*Flow, error) {
	var err error
	f.State, err = randomToken()
	if err != nil {
		return nil, err
	}
	origin := "https://platform.stepfun.com"
	if f.Region == "global" {
		origin = "https://platform.stepfun.ai"
	}
	f.AuthorizeURL = origin + "/cli-login?" + url.Values{"port": {"53683"}, "state": {f.State}}.Encode()
	return f, nil
}

func exchangeStepFun(f *Flow, callback string) (*Grant, error) {
	if !time.Now().Before(f.ExpiresAt) {
		return nil, ErrExpired
	}
	u, err := url.Parse(strings.TrimSpace(callback))
	expected, _ := url.Parse(StepFunRedirect)
	if err != nil || u.Scheme != expected.Scheme || u.Host != expected.Host || u.Path != expected.Path || u.User != nil || u.Fragment != "" {
		return nil, errors.New("paste the complete Step callback URL")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || f.State == "" || len(q["state"]) != 1 || q.Get("state") != f.State {
		return nil, errors.New("invalid callback state")
	}
	for _, values := range q {
		if len(values) != 1 {
			return nil, errors.New("ambiguous callback")
		}
	}
	if q.Get("error") != "" {
		return nil, ErrDenied
	}
	// A default production token exchange/refresh endpoint is not published.
	if q.Get("code") != "" {
		return nil, errors.New("authorization code exchange is not supported")
	}
	token := ""
	for _, key := range []string{"api_key", "apiKey", "access_token", "accessToken"} {
		if value, ok := q[key]; ok {
			if token != "" || !validToken(value[0]) || brandidentity.ContainsBrand(value[0]) {
				return nil, errors.New("invalid callback credential")
			}
			token = value[0]
		}
	}
	if token == "" {
		return nil, errors.New("callback credential is missing")
	}
	g := &Grant{AccessToken: token}
	// Retain an explicit lifetime without claiming that this grant can refresh.
	if q.Has("expires_in") && q.Has("expiresIn") {
		return nil, errors.New("ambiguous callback lifetime")
	}
	for _, key := range []string{"expires_in", "expiresIn"} {
		if q.Has(key) {
			seconds, err := strconv.ParseInt(q.Get(key), 10, 64)
			if err != nil || seconds <= 0 || seconds > 365*24*3600 {
				return nil, errors.New("invalid callback lifetime")
			}
			g.ExpiresAt = time.Now().Add(time.Duration(seconds) * time.Second)
		}
	}
	return g, nil
}
