package userpass

import (
	"fmt"
	"net/http"
	"time"

	"github.com/distribution/distribution/v3/registry/auth"
	"github.com/jc-lab/docker-cache-server/internal/dcontext"
	"github.com/jc-lab/docker-cache-server/pkg/config"
)

type AuthenticateFunc func(username string, password string) (bool, error)

type accessController struct {
	realm        string
	modtime      time.Time
	authenticate AuthenticateFunc
}

var _ auth.AccessController = &accessController{}

func NewWithCallback(realm string, authenticate AuthenticateFunc) (auth.AccessController, error) {
	return &accessController{
		realm:        realm,
		authenticate: authenticate,
	}, nil
}

func NewWithCreds(realm string, creds []config.UserCreds) (auth.AccessController, error) {
	credsMap := make(map[string]config.UserCreds)
	for _, cred := range creds {
		credsMap[cred.Username] = cred
	}
	return &accessController{
		realm: realm,
		authenticate: func(username string, password string) (bool, error) {
			user, found := credsMap[username]
			if found && user.Password == password {
				return true, nil
			}
			return false, nil
		},
	}, nil
}

func (ac *accessController) Authorized(req *http.Request, accessRecords ...auth.Access) (*auth.Grant, error) {
	username, password, ok := req.BasicAuth()
	if !ok {
		return nil, &challenge{
			realm: ac.realm,
			err:   auth.ErrInvalidCredential,
		}
	}

	success, err := ac.authenticate(username, password)
	if err != nil {
		dcontext.GetLogger(req.Context()).Errorf("error authenticating user %q: %v", username, err)
		return nil, &challenge{
			realm: ac.realm,
			err:   err,
		}
	} else if !success {
		dcontext.GetLogger(req.Context()).Errorf("failure authenticating user %q", username)
		return nil, &challenge{
			realm: ac.realm,
			err:   auth.ErrAuthenticationFailure,
		}
	}

	return &auth.Grant{User: auth.UserInfo{Name: username}}, nil
}

// challenge implements the auth.Challenge interface.
type challenge struct {
	realm string
	err   error
}

var _ auth.Challenge = challenge{}

// SetHeaders sets the basic challenge header on the response.
func (ch challenge) SetHeaders(r *http.Request, w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", fmt.Sprintf("Basic realm=%q", ch.realm))
}

func (ch challenge) Error() string {
	return fmt.Sprintf("basic authentication challenge for realm %q: %s", ch.realm, ch.err)
}
