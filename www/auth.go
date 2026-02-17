package www

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"

	"github.com/gorilla/sessions"
	"golang.org/x/crypto/bcrypt"

	"waralert/config"
)

const (
	sessionName    = "waralert_session"
	sessionUserKey = "username"
	sessionRoleKey = "role"
)

type sessionStore struct {
	store *sessions.CookieStore
}

func newSessionStore(secret string) *sessionStore {
	var key []byte
	if secret != "" {
		key, _ = base64.StdEncoding.DecodeString(secret)
	}
	if len(key) < 32 {
		key = make([]byte, 32)
		rand.Read(key)
	}

	store := sessions.NewCookieStore(key)
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}

	return &sessionStore{store: store}
}

func (s *sessionStore) get(r *http.Request) *sessions.Session {
	session, _ := s.store.Get(r, sessionName)
	return session
}

func (s *sessionStore) getUser(r *http.Request) (username, role string, ok bool) {
	session := s.get(r)
	user, uok := session.Values[sessionUserKey].(string)
	role, rok := session.Values[sessionRoleKey].(string)
	if !uok || !rok || user == "" {
		return "", "", false
	}
	return user, role, true
}

func (s *sessionStore) setUser(w http.ResponseWriter, r *http.Request, username, role string) error {
	session := s.get(r)
	session.Values[sessionUserKey] = username
	session.Values[sessionRoleKey] = role
	return session.Save(r, w)
}

func (s *sessionStore) clear(w http.ResponseWriter, r *http.Request) error {
	session := s.get(r)
	delete(session.Values, sessionUserKey)
	delete(session.Values, sessionRoleKey)
	session.Options.MaxAge = -1
	return session.Save(r, w)
}

func checkPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// HashPassword generates a bcrypt hash for the given password.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func isAdmin(role string) bool {
	return role == config.RoleAdmin
}
