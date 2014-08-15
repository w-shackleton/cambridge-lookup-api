package cambridge_lookup_api

import (
	"fmt"
	"net/http"

	"github.com/codegangsta/negroni"
	"github.com/julienschmidt/httprouter"

	"appengine"
	"appengine/datastore"
)

func init() {
	router := httprouter.New()
	router.GET("/api/people/:crsid", BasicAuth(GetPerson))
	router.GET("/", Home)
	router.GET("/configure", UpdateConfigurationPage)
	router.POST("/configure", UpdateConfiguration)

	n := negroni.Classic()
	n.UseHandler(router)

	http.Handle("/", n)
}

func Home(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	fmt.Fprint(w, "This is a proxy for the cambridge lookup API\n")
}

func UpdateConfiguration(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	key := r.FormValue("key")
	val := r.FormValue("value")

	ctx := appengine.NewContext(r)

	err := setConfig(ctx, key, val)
	if err != nil {
		panic(err.Error())
	}

	UpdateConfigurationPage(w, r, p)
}

func UpdateConfigurationPage(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	fmt.Fprint(w, updateConfigTemplate)
}

func GetPerson(w http.ResponseWriter, r *http.Request, params httprouter.Params) {
	crsid := params.ByName("crsid")

	val, ok := getFromCache(crsid)

	if ok {
		fmt.Fprint(w, val)
		return
	}

	ctx := appengine.NewContext(r)

	// check if this person is in the datastore
	k := datastore.NewKey(ctx, "Person", crsid, 0, nil)
	person := new(Person)

	if err := datastore.Get(ctx, k, person); err != nil {
		// person doesn't exist in database

		for triesRemaining := 2; triesRemaining > 0; triesRemaining-- {
			sess := getSession()
			ctx.Infof("Trying with session " + sess)
			person, err = getPerson(ctx, crsid, sess)

			if err != nil {
				switch err := err.(type) {
				case authRequiredError:
					refreshSession(sess, ctx)
					continue
					// auth was required - lets refresh the session then continue
				default:
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
		}

		// we don't care if this fails - it is only a cache
		datastore.Put(ctx, k, person)

	}

	putInCache(crsid, person.String())

	fmt.Fprint(w, person)
}

// Gets a person from lookup

const updateConfigTemplate = `
<html>
  <body>
    <form method="post">
		<input name="key" value="" placeholder="Key" /><br />
		<input name="value" type="password" value="" placeholder="Value" /><br />
		<input type="submit" />
	</form>
  </body>
</html>
`
