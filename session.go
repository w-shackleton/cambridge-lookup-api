package cambridge_lookup_api

import (
	"errors"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"

	"appengine"
	"appengine/urlfetch"
)

var session string
var sessionSync sync.Mutex

func getSession() string {
	// we use locks to make sure we don't return a session that is currently
	// being refreshed
	sessionSync.Lock()
	defer sessionSync.Unlock()

	return session
}

func refreshSession(oldSession string, ctx appengine.Context) error {
	sessionSync.Lock()
	defer sessionSync.Unlock()

	// check that the session hasn't already been refreshed
	if oldSession != session {
		return nil
	}

	// create a fresh cookie supported client
	client := urlfetch.Client(ctx)
	// this can't fail with no options passes
	client.Jar, _ = cookiejar.New(nil)

	// prepare the auth request
	lookupURL := "http://www.lookup.cam.ac.uk/"
	values := make(url.Values)

	userid, err := getConfig(ctx, "userid")
	if err != nil {
		panic("Server not configured")
	}

	pwd, err := getConfig(ctx, "pwd")
	if err != nil {
		panic("Server not configured")
	}

	values.Set("ver", "3") // version of raven to use
	values.Set("url", lookupURL)
	values.Set("userid", userid)
	values.Set("pwd", pwd)

	res, err := client.PostForm(
		"https://raven.cam.ac.uk/auth/authenticate2.html",
		values,
	)

	// check for error
	if err != nil {
		return err
	}

	// check that we logged in successfully
	if strings.Contains(res.Request.URL.String(), "raven.cam") {
		return errors.New("Raven login unsuccessful")
	}

	// login was successful - grab the session cookie
	u, _ := url.Parse(lookupURL)
	cookies := client.Jar.Cookies(u)

	for _, cookie := range cookies {
		ctx.Infof(cookie.Name)
		if cookie.Name == "JSESSIONID" {
			ctx.Infof(cookie.Value)
			session = cookie.Value
		}
	}

	// check that session has changed
	if session == oldSession {
		ctx.Errorf("Session hasn't changed")
		return errors.New("Session didn't change")
	}

	return nil
}
