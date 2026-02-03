package main

import (
	"embed"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

type config struct {
	Users        map[string]User
	Servers      map[string]Server
	UserGroups   map[string]UserGroup
	ServerGroups map[string]ServerGroup
}

type User struct {
	Keys         []string
	expandedKeys []string
}

type Server struct {
	Users    map[string][]string
	Mapusers bool
}

type UserGroup struct {
	Members []string
}

type ServerGroup struct {
	Members []string
	Server
}

type server struct {
	router *mux.Router
	logger *zap.Logger
	conf   config
	url    string
	tpl    *template.Template

	m sync.RWMutex
}

var GitCommit string

//go:embed templates
var templates embed.FS

var (
	flagAddr        = flag.String("addr", ":8080", "Server address")
	flagUrl         = flag.String("url", "http://localhost:8080", "Server url")
	flagKeyFetch    = flag.Duration("keyfetch", 5*time.Minute, "Online public key update interval")
	flagCertFile    = flag.String("certfile", "", "Path to a TLS cert file")
	flagKeyFile     = flag.String("keyfile", "", "Path to a TLS key file")
	flagConfigFile  = flag.String("config", "config.toml", "Path to toml config file")
	flagVersion     = flag.Bool("version", false, "Print version and exit")
	flagAuthScript  = flag.Bool("authScript", false, "generate auth.sh and exit")
	flagSetupScript = flag.Bool("setupScript", false, "generate setup.sh and exit")
)

func main() {
	flag.Parse()

	tpl, err := template.ParseFS(templates, "templates/*")
	if err != nil {
		log.Fatalf("Could not parse templates: %s", err)
	}

	if *flagVersion {
		fmt.Printf("Git-commit: '%s'\n", GitCommit)
		os.Exit(0)
	}
	if *flagAuthScript {
		if err := authScript(tpl, os.Stdout, *flagUrl); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}
	if *flagSetupScript {
		if err := setupScript(tpl, os.Stdout, *flagUrl); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}

	s := &server{
		router: mux.NewRouter(),
		url:    *flagUrl,
		tpl:    tpl,
	}
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatal("Could not setup zap logger")
	}
	s.logger = logger
	c, err := os.ReadFile(*flagConfigFile)
	if err != nil {
		log.Fatal(err)
	}
	_, err = toml.Decode(string(c), &s.conf)
	if err != nil {
		log.Fatal(err)
	}
	// the keys (and their length do not change, already initialize them
	for username, user := range s.conf.Users {
		user.expandedKeys = make([]string, len(user.Keys))
		s.conf.Users[username] = user
	}
	// expand server groups
	for groupname, group := range s.conf.ServerGroups {
		for _, member := range group.Members {
			if _, ok := s.conf.Servers[member]; ok {
				log.Fatalf("server '%s' already exists, but is also member of servergroup '%s'", member, groupname)
			}
			s.conf.Servers[member] = Server{Users: group.Users, Mapusers: group.Mapusers}
		}
	}

	s.routes()
	s.updateKeys()
	go s.keysWatcher(*flagKeyFetch)

	server := http.Server{
		Addr:    *flagAddr,
		Handler: s,
	}

	if *flagCertFile != "" && *flagKeyFile != "" {
		log.Fatal(server.ListenAndServeTLS(*flagCertFile, *flagKeyFile))
	} else {
		log.Fatal(server.ListenAndServe())
	}
}

var (
	keysRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "lbkeyper_keys_requests_total",
		Help: "Total requests for keys",
	}, []string{"code", "host", "user"})
)

// handler interface
func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *server) updateKeys() {
	s.m.Lock()
	defer s.m.Unlock()
	for username, user := range s.conf.Users {
		for i, key := range user.Keys {
			if strings.HasPrefix(key, "http") {
				resp, err := http.Get(key)
				if err != nil {
					s.logger.Error(fmt.Sprintf("Could not get '%s' for user '%s'", key, username))
					continue // ignore, but keep the old one "cached"
				}
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					s.logger.Error(fmt.Sprintf("Could not get '%s' for user '%s'", key, username))
					continue // ignore, but keep the old one "cached"
				} else if resp.StatusCode < 200 || resp.StatusCode >= 300 { // check late, we want Body to be closed
					s.logger.Error(fmt.Sprintf("Could not get '%s' for user '%s', status not successful: %s", key, username, resp.Status))
					continue // ignore, but keep the old one "cached"
				}
				s.conf.Users[username].expandedKeys[i] = strings.TrimSpace(string(body))
			} else {
				s.conf.Users[username].expandedKeys[i] = s.conf.Users[username].Keys[i]
			}
		}
	}
}

func (s *server) keysWatcher(interval time.Duration) {
	for {
		s.updateKeys()
		time.Sleep(interval)
	}
}

func (s *server) getKeys() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hostname, ok := mux.Vars(r)["host"]
		if !ok {
			keysRequestsTotal.WithLabelValues(strconv.FormatInt(http.StatusBadRequest, 10), "", "").Inc()
			s.errorf(http.StatusBadRequest, r.RemoteAddr, w, "# Could not get 'host' parameter")
			return
		}
		username, ok := mux.Vars(r)["user"]
		if !ok {
			keysRequestsTotal.WithLabelValues(strconv.FormatInt(http.StatusBadRequest, 10), hostname, "").Inc()
			s.errorf(http.StatusBadRequest, r.RemoteAddr, w, "# Could not get 'user' parameter")
			return
		}

		server, ok := s.conf.Servers[hostname]
		if !ok {
			keysRequestsTotal.WithLabelValues(strconv.FormatInt(http.StatusBadRequest, 10), hostname, username).Inc()
			s.errorf(http.StatusBadRequest, r.RemoteAddr, w, "# No entry for hostname '%s'", hostname)
			return
		}

		defer keysRequestsTotal.WithLabelValues(strconv.FormatInt(http.StatusOK, 10), hostname, username)

		// from here on we don't want to error out:
		// if we return successful (but empty), this will clean the cache for users that got rotated out (see authsh)
		users, ok := server.Users[username]
		if !ok {
			if !server.Mapusers { // we are done here
				s.logger.Error(fmt.Sprintf("No entry for user '%s' on server '%s'", username, hostname))
				return
			}
			// check for mapped user
			_, ok := s.conf.Users[username]
			if !ok {
				s.logger.Error(fmt.Sprintf("No entry for mapped user '%s' on server '%s'", username, hostname))
				return
			}
			users = []string{username}
		}

		users, err := expandUsers(users, s.conf.UserGroups)
		if err != nil {
			s.logger.Error(fmt.Sprintf("Could not expand users: %v", err))
			return
		}

		// lock could be moved to inner loop, but I don't think getting the lock and giving it up for every user helps
		s.m.RLock()
		defer s.m.RUnlock()
		for _, username := range users {
			if _, err := fmt.Fprintf(w, "# user: %s\n", username); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
			}
			user, ok := s.conf.Users[username]
			if !ok {
				s.logger.Error(fmt.Sprintf("Could not get user for username: %s", username))
				return
			}
			for _, key := range user.expandedKeys {
				if len(key) == 0 {
					continue
				}
				if _, err := fmt.Fprintln(w, key); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
				}
			}
		}
	}
}

func (s *server) authsh() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := authScript(s.tpl, w, s.url); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
}

func (s *server) setupsh() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := setupScript(s.tpl, w, s.url); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}
}

func (s *server) hello() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/text")

		if _, err := fmt.Fprintf(w, "Successfully connected to lbkeyper ('%s')\n", GitCommit); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
}

func (s *server) errorf(code int, remoteAddr string, w http.ResponseWriter, format string, a ...interface{}) {
	w.WriteHeader(code)
	_, _ = fmt.Fprintf(w, format, a...)
	s.logger.Error(fmt.Sprintf(format, a...),
		zap.String("type", "error"),
		zap.String("remoteAddr", remoteAddr),
		zap.Int("code", code))
}

func expandUsers(usersWithGroups []string, groups map[string]UserGroup) ([]string, error) {
	users := make([]string, 0, len(usersWithGroups))

	userSet := make(map[string]struct{})
	for _, user := range usersWithGroups {
		if strings.HasPrefix(user, "@") {
			groupUsers, ok := groups[user[1:]]
			if !ok {
				return users, fmt.Errorf("Could not find users for group %s", user)
			}
			for _, groupUser := range groupUsers.Members {
				userSet[groupUser] = struct{}{}
			}
		} else {
			userSet[user] = struct{}{}
		}
	}

	for user := range userSet {
		users = append(users, user)
	}
	sort.Strings(users)

	return users, nil
}

func authScript(t *template.Template, w io.Writer, url string) error {
	return t.ExecuteTemplate(w, "auth.sh", map[string]string{"KeyperServer": url})
}

func setupScript(t *template.Template, w io.Writer, url string) error {
	return t.ExecuteTemplate(w, "setup.sh", map[string]string{"KeyperServer": url})
}
