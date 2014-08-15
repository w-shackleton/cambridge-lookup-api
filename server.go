package cambridge_lookup_api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/codegangsta/negroni"
	"github.com/julienschmidt/httprouter"

	"appengine"
	"appengine/datastore"
	"appengine/urlfetch"
)

var config map[string]string
var configSync sync.Mutex

var session string
var sessionSync sync.Mutex

/*
var ramCache string
var ramCacheSync sync.Mutex
*/

type Config struct {
	Value       string
	LastUpdated time.Time
}

func getConfig(ctx appengine.Context, key string) (string, error) {
	configSync.Lock()
	defer configSync.Unlock()

	// grab the config
	val, ok := config[key]

	if ok {
		return val, nil
	}

	// not in memory - query database

	conf := new(Config)
	k := datastore.NewKey(ctx, "Config", key, 0, nil)

	if err := datastore.Get(ctx, k, conf); err != nil {
		return "", errors.New("Config does not exist")
	}

	return conf.Value, nil
}

func setConfig(ctx appengine.Context, key string, val string) error {
	k := datastore.NewKey(ctx, "Config", key, 0, nil)
	conf := Config{
		Value:       val,
		LastUpdated: time.Now(),
	}
	_, err := datastore.Put(ctx, k, &conf)
	return err
}

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

/*
	Person struct
*/

type Person struct {
	CRSID          string
	DisplayName    string
	RegisteredName string
	Surname        string
	Institution    string
	College        string
	Status         string
	LastUpdated    time.Time
}

func (p Person) String() string {
	b, err := json.Marshal(p)
	if err != nil {
		fmt.Println(err)
		return "{}"
	}
	s := string(b)
	return s
}

type authRequiredError struct{}

func (e authRequiredError) Error() string {
	return "Lookup redirected to raven"
}

type PersonNotFoundError struct {
	CRSID string
}

func (e PersonNotFoundError) Error() string {
	return "Person was not found: " + e.CRSID
}

type LookupServerError struct {
	Status int
}

func (e LookupServerError) Error() string {
	return "Lookup server returned status code: " + string(e.Status)
}

/*
	Server handlers
*/

func init() {
	router := httprouter.New()
	router.GET("/api/people/:crsid", GetPerson)
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
	ctx := appengine.NewContext(r)
	crsid := params.ByName("crsid")

	// TODO: use RAM cache (could fit every single entry into RAM easily)

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

	fmt.Fprint(w, person)
}

// Gets a person from lookup
func getPerson(ctx appengine.Context, crsid string, sess string) (*Person, error) {

	client := urlfetch.Client(ctx)

	// make a new request
	lookupURL := "http://www.lookup.cam.ac.uk/person/crsid/" + crsid + "/details"
	req, err := http.NewRequest("GET", lookupURL, nil)

	if err != nil {
		return &Person{}, err
	}

	// create cookie
	cookie := &http.Cookie{
		Name:    "JSESSIONID",
		Value:   sess,
		Expires: time.Now().Add(356 * 24 * time.Hour),
	}

	req.AddCookie(cookie)

	res, err := client.Do(req)

	if err != nil {
		return &Person{}, err
	}

	// success?

	defer res.Body.Close()

	// check if we have been redirected to raven
	if strings.Contains(res.Request.URL.String(), "raven.cam") {
		return &Person{}, authRequiredError{}
	}

	// got a response

	// check if 404:
	if res.StatusCode == 404 {
		return &Person{}, PersonNotFoundError{crsid}
	}

	// check if not 200
	if res.StatusCode != 200 {
		return &Person{}, LookupServerError{res.StatusCode}
	}

	doc, err := goquery.NewDocumentFromResponse(res)

	if err != nil {
		return &Person{}, err
	}

	person := Person{
		CRSID:       crsid,
		LastUpdated: time.Now(),
	}

	// parse response
	doc.Find(".listing.identifiers tr").Each(func(i int, s *goquery.Selection) {
		name := s.Find("td strong").Text()
		value := s.Find("td").First().Next().Text()

		// format the values properly
		reg, _ := regexp.Compile(`\s*Value:\s+(.*)\s+Visibility.*`)

		match := reg.FindStringSubmatch(value)

		if match != nil {
			value = match[1]
		}

		if name == "Display name:" {
			person.DisplayName = value
		}

		if name == "Registered name:" {
			person.RegisteredName = value
		}

		if name == "Surname:" {
			person.Surname = value
		}

		if name == "MIS status:" {
			person.Status = value
		}

		if name == "UCS registered institution:" {
			person.Institution = value
		}

		if name == "College:" {
			person.College = value
		}
	})

	return &person, nil
}

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
