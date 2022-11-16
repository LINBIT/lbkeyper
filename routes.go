package main

func (s *server) routes() {
	s.router.HandleFunc("/api/v1/keys/{host}/{user}", s.getKeys()).Methods("GET")
	s.router.HandleFunc("/api/v1/hello", s.hello()).Methods("GET")
	s.router.HandleFunc("/auth.sh", s.authsh()).Methods("GET")
}
