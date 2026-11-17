package connector

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// jiraGateway is Atlassian's gateway to a Jira Cloud site, by cloud id: where a sign-in's and a
// scoped API token's calls go.
const jiraGateway = "https://api.atlassian.com/ex/jira/"

// jiraSite is the Jira site a person typed, as the root of its REST API: https when no scheme
// was typed, and without the page they copied it from (…/jira/your-work, …/browse/KIT-1). A
// Jira Cloud site is its host alone, always over https (http answers a redirect that turns a
// POST into a GET); a Data Center site keeps its context path (https://host/jira).
func jiraSite(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return ""
	}
	if !strings.Contains(u, "://") {
		u = "https://" + u
	}
	p, err := url.Parse(u)
	if err != nil || p.Host == "" {
		return strings.TrimRight(u, "/")
	}
	if jiraCloudHost(p.Host) {
		return "https://" + strings.ToLower(p.Host)
	}
	path := p.Path
	for _, page := range []string{"/rest/", "/browse/", "/secure/", "/projects/", "/plugins/", "/issues/", "/servicedesk/", "/login.jsp"} {
		if i := strings.Index(path+"/", page); i >= 0 {
			path = path[:i]
		}
	}
	return p.Scheme + "://" + p.Host + strings.TrimRight(path, "/")
}

// jiraCloudHost says whether a host is a Jira Cloud site.
func jiraCloudHost(host string) bool {
	h := strings.ToLower(host)
	return strings.HasSuffix(h, ".atlassian.net") || strings.HasSuffix(h, ".jira.com")
}

// sendJira sends a Jira request with the account typed in:
//   - a token without an email is a Data Center personal access token, sent as a bearer;
//   - a Jira Cloud site that refuses an email and token with 401 is asked for its cloud id, and
//     the call goes again through Atlassian's gateway, where scoped API tokens work (the site
//     itself only takes classic ones);
//
// and says what an error means for Jira.
func (a *api) sendJira(ctx context.Context, req *request) ([]byte, error) {
	if req.basic[0] == "" && req.basic[1] != "" {
		if req.headers == nil {
			req.headers = map[string]string{}
		}
		req.headers["Authorization"] = "Bearer " + req.basic[1]
		req.basic = [2]string{}
	}
	site := jiraSite(a.s["url"])
	gateway := ""
	if req.basic[0] != "" && site != "" && strings.HasPrefix(req.url, site) {
		a.mu.Lock()
		if a.jiraCloud != "" {
			gateway = jiraGateway + url.PathEscape(a.jiraCloud)
			req.url = gateway + strings.TrimPrefix(req.url, site)
		}
		a.mu.Unlock()
	}
	raw, err := do(ctx, *req)
	var st *StatusError
	if err != nil && gateway == "" && errors.As(err, &st) && st.Code == http.StatusUnauthorized && req.basic[0] != "" && strings.HasPrefix(req.url, site) {
		if host, _ := url.Parse(site); host != nil && jiraCloudHost(host.Host) {
			if id := jiraCloudID(ctx, site); id != "" {
				gateway = jiraGateway + url.PathEscape(id)
				retry := *req
				retry.url = gateway + strings.TrimPrefix(req.url, site)
				if raw, err = do(ctx, retry); err == nil {
					a.mu.Lock()
					a.jiraCloud = id
					a.mu.Unlock()
				}
			}
		}
	}
	if err != nil {
		return nil, jiraError(err, site, req.basic[0] != "")
	}
	return raw, nil
}

// jiraCloudID asks a Jira Cloud site its cloud id (an answer without an account); empty when it
// does not say.
func jiraCloudID(ctx context.Context, site string) string {
	raw, err := do(ctx, request{method: "GET", url: site + "/_edge/tenant_info"})
	if err != nil {
		return ""
	}
	var info struct {
		CloudID string `json:"cloudId"`
	}
	if json.Unmarshal(raw, &info) != nil {
		return ""
	}
	return strings.TrimSpace(info.CloudID)
}

// jiraError says what a failed Jira call means: the site, the account or its permissions.
func jiraError(err error, site string, withEmail bool) error {
	if site == "" && withEmail {
		return fmt.Errorf("Jira: the Site is missing (like https://acme.atlassian.net): %w", err)
	}
	var st *StatusError
	if !errors.As(err, &st) {
		return fmt.Errorf("Jira at %s: %w", site, err)
	}
	switch st.Code {
	case http.StatusUnauthorized:
		if withEmail {
			return fmt.Errorf("Jira at %s refused the email and API token (HTTP 401): the email must be the one of the Atlassian account that made the token, and the token a current one from id.atlassian.com/manage-profile/security/api-tokens: %w", site, err)
		}
		return fmt.Errorf("Jira at %s refused the token (HTTP 401): on Jira Cloud fill in the account's email too; on Data Center use a current personal access token: %w", site, err)
	case http.StatusForbidden:
		return fmt.Errorf("Jira at %s: the account may not do that (HTTP 403): check its permissions in the project, or the token's scopes: %w", site, err)
	case http.StatusNotFound:
		return fmt.Errorf("Jira at %s answered 404: check the Site (just the address, like https://acme.atlassian.net) and the project key: %w", site, err)
	}
	return fmt.Errorf("Jira at %s: %w", site, err)
}

// StatusError is an answer outside 2xx.
type StatusError struct {
	Code int
	Body string
}

func (e *StatusError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Code, e.Body) }

// netError says in words why a request reached no answer (a bad address, a name that does not
// resolve, a refused or timed-out connection, an untrusted certificate), keeping the cause.
func netError(err error) error {
	var ue *url.Error
	host := ""
	if errors.As(err, &ue) {
		if p, perr := url.Parse(ue.URL); perr == nil {
			host = p.Host
		}
	}
	var dns *net.DNSError
	var unknownCA x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var verify *tls.CertificateVerificationError
	var ne net.Error
	switch {
	case ue != nil && strings.Contains(ue.Err.Error(), "unsupported protocol scheme"):
		if ue.URL == "" || strings.HasPrefix(ue.URL, "/") {
			return fmt.Errorf("the address is missing: %w", err)
		}
		return fmt.Errorf("%q is not a web address (it starts with https://): %w", ue.URL, err)
	case errors.As(err, &dns):
		return fmt.Errorf("cannot find %s (DNS): check the address, or the network or VPN: %w", dns.Name, err)
	case errors.As(err, &unknownCA), errors.As(err, &hostname), errors.As(err, &invalid), errors.As(err, &verify):
		return fmt.Errorf("the certificate of %s is not trusted (a proxy or a company certificate in between?): %w", host, err)
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return fmt.Errorf("%s did not answer in time (network, proxy or firewall): %w", host, err)
	case strings.Contains(err.Error(), "connection refused"):
		return fmt.Errorf("%s refused the connection: %w", host, err)
	}
	return err
}
