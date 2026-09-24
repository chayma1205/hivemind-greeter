package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	fmt.Println("Hivemind's Go Greeter")
	fmt.Println("You are running the service with this tag: ", os.Getenv("HELLO_TAG"))
	http.HandleFunc("/", HelloServer)
	http.ListenAndServe(":8080", nil)
}

func HelloServer(w http.ResponseWriter, r *http.Request) {
	fmtStr := fmt.Sprintf("Hello, %s! I'm %s, running tag %s", GetIPFromRequest(r), os.Getenv("HOSTNAME"), GetTag(r))
	fmt.Println(fmtStr)
	fmt.Fprintln(w, fmtStr)
}

// GetTag resolves the running tag: a `?tag=` query parameter, if present,
// overrides the HELLO_TAG environment variable set at deploy time — the
// URL-parameter requirement from the original challenge brief, closing a
// gap that was env-var-only until now (see README.md).
func GetTag(r *http.Request) string {
	if tag := r.URL.Query().Get("tag"); tag != "" {
		return tag
	}

	return os.Getenv("HELLO_TAG")
}

func GetIPFromRequest(r *http.Request) string {
	if fwd := r.Header.Get("x-forwarded-for"); fwd != "" {
		return fwd
	}

	return r.RemoteAddr
}
