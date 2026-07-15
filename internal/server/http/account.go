package httpapi

import (
	"net/http"

	"github.com/hoijun/fleet/internal/server/pgstore"
)

// Account serves account-scoped operations that require an authenticated user.
type Account struct {
	Store pgstore.Store
}

// Delete handles DELETE /account: it irreversibly removes the authenticated
// user and all of their data, then returns 204. The route lives behind
// AuthMiddleware, so a missing user id means the token was not verified.
func (a Account) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserID(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := a.Store.DeleteAccount(r.Context(), userID); err != nil {
		http.Error(w, "delete failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
