package cambridge_lookup_api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"appengine"
	"appengine/urlfetch"

	"github.com/PuerkitoBio/goquery"
)

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
		reg, _ := regexp.Compile(`\s*Value:\s+(.*)\s+(Visibility)?.*`)

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
