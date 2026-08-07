package transport

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ZToolsCenter/ztools-sync-server/auth"
)

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func readJSON(r *http.Request, out interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(out)
}

func bearerUID(authService *auth.Service, r *http.Request) (string, bool) {
	uid, present, ok := optionalBearerUID(authService, r)
	return uid, present && ok
}

func optionalBearerUID(authService *auth.Service, r *http.Request) (string, bool, bool) {
	header := r.Header.Get("Authorization")
	token := strings.TrimPrefix(header, "Bearer ")
	if token == "" || token == header {
		return "", false, false
	}
	uid, err := authService.Verify(token)
	return uid, true, err == nil
}
