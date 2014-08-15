package cambridge_lookup_api

import (
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/julienschmidt/httprouter"

	"appengine"
)

func BasicAuth(pass httprouter.Handle) httprouter.Handle {

	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		ctx := appengine.NewContext(r)

		// check we have an authorization request
		authheaders, ok := r.Header["Authorization"]

		if !ok {
			// return an http basic auth request
			w.Header().Set("WWW-Authenticate", `Basic realm="Cambridge Lookup API"`)
			w.WriteHeader(401)
			w.Write([]byte("401 Unauthorized\n"))
			return
		}

		auth := strings.SplitN(authheaders[0], " ", 2)

		if len(auth) != 2 || auth[0] != "Basic" {
			http.Error(w, "bad syntax", http.StatusBadRequest)
			return
		}

		payload, _ := base64.StdEncoding.DecodeString(auth[1])
		pair := strings.SplitN(string(payload), ":", 2)

		if len(pair) != 2 || !Validate(ctx, pair[0], pair[1]) {
			http.Error(w, "authorization failed", http.StatusUnauthorized)
			return
		}

		pass(w, r, p)
	}
}

func Validate(ctx appengine.Context, username, password string) bool {
	apikey, _ := getConfig(ctx, "apikey")
	return username == apikey
}
