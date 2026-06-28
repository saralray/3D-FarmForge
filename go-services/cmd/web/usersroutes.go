package main

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"printfarm/internal/pwcrypto"
)

// usersroutes.go ports the staff-user management handlers (server/app.js
// /api/users surface, minus /verify which lives in authroutes.go). The gate has
// already required admin for these paths; password hashes are never returned.

var userRoles = map[string]bool{"admin": true, "operator": true, "viewer": true}

// sanitizeStaffUser drops the password hash; ordered struct matches Node's
// {id, name, username, role} key order.
type sanitizedUser struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func sanitizeStaffUser(u staffUser) sanitizedUser {
	return sanitizedUser{ID: u.ID, Name: u.Name, Username: u.Username, Role: u.Role}
}

func handleUserRoutes(w http.ResponseWriter, req *http.Request) bool {
	ctx := req.Context()
	p := req.URL.Path
	m := req.Method

	switch {
	case p == "/api/users" && m == http.MethodGet:
		users, err := readStaffUsers(ctx)
		if err != nil {
			internalError(w, "readStaffUsers", err)
			return true
		}
		out := make([]sanitizedUser, 0, len(users))
		for _, u := range users {
			out = append(out, sanitizeStaffUser(u))
		}
		sendJSON(w, http.StatusOK, out, "")
		return true

	case p == "/api/users" && m == http.MethodPost:
		handleUserCreate(ctx, w, req)
		return true

	case strings.HasPrefix(p, "/api/users/"):
		handleUserByID(ctx, w, req)
		return true
	}
	return false
}

func handleUserCreate(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	var body struct {
		Name         string `json:"name"`
		Username     string `json:"username"`
		Role         string `json:"role"`
		PasswordHash string `json:"passwordHash"`
	}
	_ = readJSONBody(req, &body)
	name := strings.TrimSpace(body.Name)
	username := strings.ToLower(strings.TrimSpace(body.Username))
	if name == "" || username == "" {
		badRequest(w, "Name and username are required.")
		return
	}
	if !userRoles[body.Role] {
		badRequest(w, "role must be admin, operator, or viewer")
		return
	}
	if !pwcrypto.IsSha256Hex(body.PasswordHash) {
		badRequest(w, "passwordHash must be a sha256 hex string")
		return
	}
	if username == reservedUsername {
		sendJSON(w, http.StatusConflict, map[string]any{"error": "That username is reserved."}, "")
		return
	}
	users, err := readStaffUsers(ctx)
	if err != nil {
		internalError(w, "readStaffUsers", err)
		return
	}
	for _, u := range users {
		if u.Username == username {
			sendJSON(w, http.StatusConflict, map[string]any{"error": "That username is already in use."}, "")
			return
		}
	}
	derived, err := pwcrypto.Derive(body.PasswordHash)
	if err != nil {
		internalError(w, "derive", err)
		return
	}
	newUser := staffUser{ID: uuid.NewString(), Name: name, Username: username, Role: body.Role, PasswordHash: derived}
	if err := writeStaffUsers(ctx, append(users, newUser)); err != nil {
		internalError(w, "setAppSetting staff_users", err)
		return
	}
	sendJSON(w, http.StatusCreated, sanitizeStaffUser(newUser), "")
}

func handleUserByID(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	rest := req.URL.Path[len("/api/users/"):]
	parts := strings.SplitN(rest, "/", 2)
	rawID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	userID, _ := decodeURIComponent(rawID)
	if userID == "" {
		badRequest(w, "user id is required")
		return
	}
	m := req.Method

	switch {
	case action == "" && m == http.MethodDelete:
		users, err := readStaffUsers(ctx)
		if err != nil {
			internalError(w, "readStaffUsers", err)
			return
		}
		if indexOfUser(users, userID) == -1 {
			notFound(w, "user not found")
			return
		}
		next := make([]staffUser, 0, len(users))
		for _, u := range users {
			if u.ID != userID {
				next = append(next, u)
			}
		}
		if err := writeStaffUsers(ctx, next); err != nil {
			internalError(w, "setAppSetting staff_users", err)
			return
		}
		_ = deleteSessionsForUser(ctx, userID)
		sendEmpty(w, http.StatusNoContent)

	case action == "password" && m == http.MethodPut:
		var body struct {
			PasswordHash string `json:"passwordHash"`
		}
		_ = readJSONBody(req, &body)
		if !pwcrypto.IsSha256Hex(body.PasswordHash) {
			badRequest(w, "passwordHash must be a sha256 hex string")
			return
		}
		users, err := readStaffUsers(ctx)
		if err != nil {
			internalError(w, "readStaffUsers", err)
			return
		}
		idx := indexOfUser(users, userID)
		if idx == -1 {
			notFound(w, "user not found")
			return
		}
		derived, err := pwcrypto.Derive(body.PasswordHash)
		if err != nil {
			internalError(w, "derive", err)
			return
		}
		users[idx].PasswordHash = derived
		if err := writeStaffUsers(ctx, users); err != nil {
			internalError(w, "setAppSetting staff_users", err)
			return
		}
		_ = deleteSessionsForUser(ctx, userID)
		sendEmpty(w, http.StatusNoContent)

	case action == "role" && m == http.MethodPut:
		var body struct {
			Role string `json:"role"`
		}
		_ = readJSONBody(req, &body)
		if !userRoles[body.Role] {
			badRequest(w, "role must be admin, operator, or viewer")
			return
		}
		users, err := readStaffUsers(ctx)
		if err != nil {
			internalError(w, "readStaffUsers", err)
			return
		}
		idx := indexOfUser(users, userID)
		if idx == -1 {
			notFound(w, "user not found")
			return
		}
		users[idx].Role = body.Role
		if err := writeStaffUsers(ctx, users); err != nil {
			internalError(w, "setAppSetting staff_users", err)
			return
		}
		_ = deleteSessionsForUser(ctx, userID)
		sendJSON(w, http.StatusOK, sanitizeStaffUser(users[idx]), "")
	}
}

func indexOfUser(users []staffUser, id string) int {
	for i := range users {
		if users[i].ID == id {
			return i
		}
	}
	return -1
}

// writeStaffUsers persists the staff list, preserving each record's full shape
// (incl. passwordHash) so a round-trip never drops the credential.
func writeStaffUsers(ctx context.Context, users []staffUser) error {
	rows := make([]map[string]any, 0, len(users))
	for _, u := range users {
		rows = append(rows, map[string]any{
			"id": u.ID, "name": u.Name, "username": u.Username, "role": u.Role, "passwordHash": u.PasswordHash,
		})
	}
	return setAppSetting(ctx, "staff_users", rows)
}

func notFound(w http.ResponseWriter, msg string) {
	sendJSON(w, http.StatusNotFound, map[string]any{"error": msg}, "")
}
