package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/globalsign/mgo/bson"
	"github.com/gorilla/mux"
)

func Index(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	b, err := ioutil.ReadFile(PATH_INDEX_PAGE)
	if err != nil {
		fmt.Fprintln(w, "Could not read HTML file from disk. Error: ", err)
		d, _ := os.Getwd()
		fmt.Fprintf(w, "Currently in %s, searching for %s", d, PATH_INDEX_PAGE)
	} else {
		fmt.Fprintf(w, "%s", b)
	}
}

func BeginAuth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	r.ParseForm()

	for _, f := range fields {
		if !f.validator(r.PostForm.Get(f.key)) {
			fmt.Fprintf(w, "%s field isn't valid!", f.key)
			return
		}
	}

	roll := r.PostForm.Get("roll")
	email := r.PostForm.Get("email")

	c := GlobalDBSession.DB(os.Getenv("DB_NAME")).C("people")

	rollExists, _ := c.Find(bson.M{"roll": roll, "step1complete": true, "step2complete": true}).Count()
	emailExists, _ := c.Find(bson.M{"email": email, "step1complete": true, "step2complete": true}).Count()

	p := Person{}
	if rollExists > 0 || emailExists > 0 {
		fmt.Fprintf(w, "%s", buildAuthUnsuccessful(rollExists, emailExists))
	} else {
		p = GetPerson(roll, email)

		err := c.Insert(&p)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Fprintf(w, "%s", buildAuthPage(p.Verifier, p.LinkSuffix))
	}
}

func getSingleSecQues(roll string) string {
	v := url.Values{}
	v.Set("user_id", roll)
	resp, _ := http.PostForm(ERP_SECRET_QUES_URL, v)
	body, _ := ioutil.ReadAll(resp.Body)
	return string(body)
}

func getSecurityQuestions(roll string) []string {
	allSecQues := make(map[string]bool)
	data := make(chan string)

	for i := 1; i <= 30; i++ { // Perform upto 30 tries to get the 3 unique secret questions from ERP
		go func() { data <- getSingleSecQues(roll) }()
	}

	for i := 1; i <= 30; i++ {
		secQues := <-data
		log.Printf("Run %d, Got %s\n", i, secQues)

		allSecQues[secQues] = true

		if len(allSecQues) >= 3 {
			break
		}
	}

	retVal := []string{}
	for key := range allSecQues {
		retVal = append(retVal, key)
	}

	return retVal
}

func VerifyStep1(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	vars := mux.Vars(r)
	linkSuf := vars["token"]

	c := GlobalDBSession.DB(os.Getenv("DB_NAME")).C("people")

	var result Person
	err := c.Find(bson.M{"linksuffix": linkSuf}).One(&result)

	if err != nil {
		fmt.Fprint(w, "That verifier token isn't there in our DB!")
		return
	}

	verified := false

	if !result.Step1Complete {

		secQues := getSecurityQuestions(result.Roll)

		for _, ques := range secQues {
			if strings.Contains(ques, result.Verifier) {
				verified = true
				break
			}
		}
	}

	if verified || result.Step1Complete {
		fmt.Fprint(w, buildStep1CompletePage(result.Email, result.Step1CompletedAt, result.Step1Complete))
		SendVerificationEmail(result.Email,
			EMAIL_SUBJECT_STEP2,
			"verify2/"+result.EmailToken)
		if !result.Step1Complete {
			c.Update(bson.M{"linksuffix": linkSuf}, bson.M{"$set": bson.M{"step1complete": true, "step1completedat": time.Now()}})
		}
	} else {
		fmt.Fprint(w, "Not verified! Go into your ERP and ensure that you have put your verifier token in one of the secret questions!")
	}
}

func VerifyStep2(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	vars := mux.Vars(r)
	emailTok := vars["token"]
	c := GlobalDBSession.DB(os.Getenv("DB_NAME")).C("people")

	var result Person
	err := c.Find(bson.M{"emailtoken": emailTok}).One(&result)

	if err != nil {
		fmt.Fprint(w, "That email token isn't there in the DB!")
		return
	}

	fmt.Fprint(w, buildStep2CompletePage(result.Roll, result.Email))
	c.Update(bson.M{"emailtoken": emailTok}, bson.M{"$set": bson.M{"step2complete": true, "step2completedat": time.Now()}})
}

func ResetIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	b, err := ioutil.ReadFile(PATH_RESET_INDEX_PAGE)
	if err != nil {
		fmt.Fprintln(w, "Could not read HTML file from disk. Error: ", err)
		d, _ := os.Getwd()
		fmt.Fprintf(w, "Currently in %s, searching for %s", d, PATH_INDEX_PAGE)
	} else {
		fmt.Fprintf(w, "%s", b)
	}
}

func BeginReset(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	vars := mux.Vars(r)
	key := vars["key"]
	r.ParseForm()
	value := r.PostForm.Get("key")

	c := GlobalDBSession.DB(os.Getenv("DB_NAME")).C("people")

	var result Person
	err := c.Find(bson.M{key: value, "step1complete": true, "step2complete": true}).One(&result)
	if err != nil {
		fmt.Fprintf(w, "%s is not associated with any authenticated person in our DB!", value)
		return
	}

	emailTok := getSha256SumRandom(fmt.Sprintf("%s %s %v %v", result.Roll, result.Email, result.Step1CompletedAt, result.Step2CompletedAt))[:HASH_LEN]
	resetReq := GetResetReq(result.Roll, result.Email, emailTok)

	c = GlobalDBSession.DB(os.Getenv("DB_NAME")).C("resetrequests")
	err = c.Insert(&resetReq)

	if err != nil {
		fmt.Fprintf(w, "We were unable to write to our Database. Please try again later! Error: %v", err)
		return
	}

	SendVerificationEmail(result.Email, EMAIL_SUBJECT_RESET, "verify-reset/"+emailTok)

	fmt.Fprintf(w, "%s", buildResetPage(redactEmail(result.Email)))
}

func VerifyReset(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	vars := mux.Vars(r)
	token := vars["token"]

	resets := GlobalDBSession.DB(os.Getenv("DB_NAME")).C("resetrequests")

	var result ResetRequest
	err := resets.Find(bson.M{"token": token}).One(&result)
	if err != nil {
		fmt.Fprintf(w, "That token doesn't exist in our DB! Check your email once again and ensure you copied the right link")
		return
	}

	// Reset is successful!
	// Delete all resets related to this roll number, email ID.
	// Delete all people related to this roll number and email ID (both
	// completely authenticated and otherwise)

	filter := bson.M{"$or": []bson.M{{"email": result.Email}, {"roll": result.Roll}}}

	people := GlobalDBSession.DB(os.Getenv("DB_NAME")).C("people")

	peopleInfo, err1 := people.RemoveAll(filter)
	resetInfo, err2 := resets.RemoveAll(filter)

	if err1 != nil || err2 != nil {
		fmt.Fprintf(w, "OOPS! There was an error while writing to the DB. People Error: %v; Resets Error: %v", err1, err2)
		return
	}

	log.Printf("DELETE People deleted: %v", peopleInfo)
	log.Printf("DELETE Reset requests deleted: %v", resetInfo)

	fmt.Fprintf(w, "%s", buildResetCompletePage(result.Roll, result.Email))
}

func GetDetails(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	err := authenticateRequest(r)

	if err != nil {
		http.Error(w, ERROR_UNAUTH, 401)
		return
	}

	vars := mux.Vars(r)
	input_type := vars["input"]
	val := vars["input_val"]

	if !allowedInputType(input_type) {
		http.Error(w, ERROR_UNKNOWN_TYPE, 401)
		return
	}

	c := GlobalDBSession.DB(os.Getenv("DB_NAME")).C("people")
	var result Person
	query := bson.M{"step1complete": true, "step2complete": true}
	query[input_type] = val

	err = c.Find(query).One(&result)
	if err != nil {
		http.Error(w, ERROR_NOT_FOUND, 404)
		return
	}

	to_return := map[string]string{}

	to_return["email"] = result.Email
	to_return["roll"] = result.Roll
	to_return["authenticated"] = result.Step2CompletedAt.Format(time.RFC3339)

	proper_json, _ := json.Marshal(to_return)

	fmt.Fprint(w, string(proper_json))
}
