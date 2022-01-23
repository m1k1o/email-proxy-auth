package main

import (
	"email-proxy-auth/auth"
	"email-proxy-auth/mail"
	"email-proxy-auth/page"
	"flag"
	"log"
	"net/http"
	"time"
)

var (
	addr, sessionCookieName, appName, appUrl string
)

var (
	sessions *auth.Manager
	email    *mail.Manager
	wpage    *page.Manager
)

func init() {
	flag.StringVar(&addr, "addr", ":8080", "Address, where is HTTP server listening.")
	flag.StringVar(&sessionCookieName, "cookie", "MAILSESS", "Session cookie name.")
	flag.StringVar(&appName, "app", "E-mail proxy auth", "App name used on lign page and in emails.")
	flag.StringVar(&appUrl, "base", "http://127.0.0.1:8080/", "Base URL for all requests and redirects.")
	flag.Parse()

	sessions = auth.New(auth.Config{
		Expiration: 31 * 24 * time.Hour,
	})

	var err error
	email, err = mail.New(mail.Config{
		AppName:      appName,
		AppUrl:       appUrl,
		TemplatePath: "./tmpl/login-email.html",
		FromAddress:  "admin@localhost",

		// smtp
		Host:     "127.0.0.1",
		Port:     25,
		Username: "bb075a04a244",
		Password: "",
	})

	if err != nil {
		panic(err)
	}

	wpage, err = page.New(page.Config{
		AppName:      appName,
		AppUrl:       appUrl,
		TemplatePath: "./tmpl/login-page.html",
	})

	if err != nil {
		panic(err)
	}
}

//
// login
//

var secureCookie = false

func loginAction(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		secret := r.URL.Query().Get("login")
		if secret == "" || r.Method != "GET" {
			next.ServeHTTP(w, r)
			return
		}

		session, ok := sessions.GetBySecret(secret)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}

		if session.Expired() {
			wpage.Error(w, "Link already expired, please request new", http.StatusBadRequest)
			return
		}

		if session.LoggedIn() {
			wpage.Error(w, "Link has been already used, please request new", http.StatusConflict)
			return
		}

		sessions.Login(session)

		sameSite := http.SameSiteDefaultMode
		if secureCookie {
			sameSite = http.SameSiteNoneMode
		}

		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    session.Token(),
			Expires:  time.Now().Add(31 * 24 * time.Hour),
			Secure:   secureCookie,
			SameSite: sameSite,
			HttpOnly: true,
		})

		// TODO: Check redirect against whitelist.
		redirectTo := r.URL.Query().Get("to")
		if redirectTo == "" {
			redirectTo = appUrl
		}

		http.Redirect(w, r, redirectTo, http.StatusTemporaryRedirect)
	}
}

func loginPage(w http.ResponseWriter, r *http.Request) {
	redirectTo := r.URL.Query().Get("to")

	if r.Method == "GET" {
		wpage.Login(w, redirectTo)
		return
	}

	if r.Method == "POST" {
		usrEmail := r.FormValue("email")
		if usrEmail == "" {
			wpage.Error(w, "No email provided", http.StatusBadRequest)
			return
		}

		// TODO: Check email against whitelist.

		session := sessions.Create(auth.Profile{
			Email: usrEmail,
		})

		err := email.Send(session, redirectTo)
		if err != nil {
			wpage.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		wpage.Success(w, "Please check your E-Mail inbox for further instructions.")
		return
	}

	http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
}

func main() {
	handle := func(w http.ResponseWriter, r *http.Request) {
		sessionCookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			loginPage(w, r)
			return
		}

		token := sessionCookie.Value
		session, ok := sessions.GetByToken(token)
		if !ok {
			// remove cookie
			sessionCookie.Expires = time.Unix(0, 0)
			http.SetCookie(w, sessionCookie)

			wpage.Error(w, "Token not found", http.StatusUnauthorized)
			return
		}

		if session.Expired() {
			// remove cookie
			sessionCookie.Expires = time.Unix(0, 0)
			http.SetCookie(w, sessionCookie)

			wpage.Error(w, "Session expried", http.StatusForbidden)
			return
		}

		if r.Method == "POST" && r.FormValue("logout") != "" {
			sessions.Delete(session)

			// remove cookie
			sessionCookie.Expires = time.Unix(0, 0)
			http.SetCookie(w, sessionCookie)

			wpage.Success(w, "You have been successfully logged out")
			return
		}

		profile := session.Profile()
		w.Header().Set("X-Auth-Email", profile.Email)
		wpage.LoggedIn(w)
	}

	http.HandleFunc("/", loginAction(handle))

	log.Println("Starting http server on", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Println("ListenAndServe error:", err)
	}
}
