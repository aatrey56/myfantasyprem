package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
)

// The generated request bodies decode both an omitted field and an
// explicit JSON null to a nil pointer, but a PATCH must tell them apart:
// omitted keeps the value, null clears it (`settings.draft.startsAt`,
// `trades.deadlineGameweek`, `avatarColor`). rawJSON keeps a PATCH body in
// the context so handlers can ask with explicitNull.

type rawBodyKey struct{}

// rawJSON stores the body of PATCH requests in the request context. The
// body is already capped by maxBytes; a read error is handed on to the
// generated handler, which reports it (413 or 400).
func rawJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.Body == nil {
			next.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), errReader{err}))
			next.ServeHTTP(w, r)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), rawBodyKey{}, body)))
	})
}

// explicitNull reports whether the request body has the member at path
// (object keys from the top) set to JSON null.
func explicitNull(ctx context.Context, path ...string) bool {
	raw, _ := ctx.Value(rawBodyKey{}).([]byte)
	cur := json.RawMessage(raw)
	for _, key := range path {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(cur, &obj); err != nil {
			return false
		}
		next, ok := obj[key]
		if !ok {
			return false
		}
		cur = next
	}
	return len(path) > 0 && bytes.Equal(bytes.TrimSpace(cur), []byte("null"))
}
