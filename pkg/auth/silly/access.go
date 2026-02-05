// Package silly provides a simple authentication scheme that checks for the
// existence of an Authorization header and issues access if is present and
// non-empty.
//
// This package is present as an example implementation of a minimal
// auth.AccessController and for testing. This is not suitable for any kind of
// production security.
package silly

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/distribution/distribution/v3/registry/auth"
)

// AccessController provides a simple implementation of auth.AccessController
// that simply checks for a non-empty Authorization header. It is useful for
// demonstration and testing.
type AccessController struct {
	realm   string
	service string
}

var _ auth.AccessController = &AccessController{}

func New(realm string, service string) (auth.AccessController, error) {
	return &AccessController{realm: realm, service: service}, nil
}

func MustNew(realm string, service string) auth.AccessController {
	return &AccessController{realm: realm, service: service}
}

// Authorized simply checks for the existence of the authorization header,
// responding with a bearer challenge if it doesn't exist.
func (ac *AccessController) Authorized(req *http.Request, accessRecords ...auth.Access) (*auth.Grant, error) {
	if req.Header.Get("Authorization") == "" {
		challenge := challenge{
			realm:   ac.realm,
			service: ac.service,
		}

		if len(accessRecords) > 0 {
			var scopes []string
			for _, access := range accessRecords {
				scopes = append(scopes, fmt.Sprintf("%s:%s:%s", access.Type, access.Resource.Name, access.Action))
			}
			challenge.scope = strings.Join(scopes, " ")
		}

		return nil, &challenge
	}

	return &auth.Grant{User: auth.UserInfo{Name: "silly"}}, nil
}

type challenge struct {
	realm   string
	service string
	scope   string
}

var _ auth.Challenge = challenge{}

// SetHeaders sets a simple bearer challenge on the response.
func (ch challenge) SetHeaders(r *http.Request, w http.ResponseWriter) {
	header := fmt.Sprintf("Bearer realm=%q,service=%q", ch.realm, ch.service)

	if ch.scope != "" {
		header = fmt.Sprintf("%s,scope=%q", header, ch.scope)
	}

	w.Header().Set("WWW-Authenticate", header)
}

func (ch challenge) Error() string {
	return fmt.Sprintf("silly authentication challenge: %#v", ch)
}
