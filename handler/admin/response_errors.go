package admin

import (
	"net/http"

	"github.com/tokendancelab/metapi-go/handler/shared"
)

// writeError writes a unified admin API error: non-2xx status + camelCase JSON
// {"error":"..."}. Prefer this over writeJSON(..., {"success":false,...}) for
// mutation failure paths so clients never see HTTP 200 with an error body.
func writeError(w http.ResponseWriter, code int, message string) {
	shared.WriteError(w, code, message)
}

// writeErrorDetail writes {"error":"...","detail":"..."} with a non-2xx status.
func writeErrorDetail(w http.ResponseWriter, code int, message, detail string) {
	shared.WriteErrorDetail(w, code, message, detail)
}
